package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	dataUsageOutChain = "STRATUX_USAGE_OUT"
	dataUsageInChain  = "STRATUX_USAGE_IN"
	dataBlockChain    = "STRATUX_BLOCK"
)

type dataUsageSettings struct {
	WarningMB        uint64 `json:"warningMB"`
	AutoBlockMB      uint64 `json:"autoBlockMB"`
	SessionLimitMB   uint64 `json:"sessionLimitMB"`
	AutoBlockEnabled bool   `json:"autoBlockEnabled"`
}

type dataUsageClient struct {
	IP             string  `json:"ip"`
	MAC            string  `json:"mac"`
	Hostname       string  `json:"hostname"`
	Connected      bool    `json:"connected"`
	Blocked        bool    `json:"blocked"`
	UploadBytes    uint64  `json:"uploadBytes"`
	DownloadBytes  uint64  `json:"downloadBytes"`
	TotalBytes     uint64  `json:"totalBytes"`
	UploadBps      float64 `json:"uploadBps"`
	DownloadBps    float64 `json:"downloadBps"`
	LastSeen       string  `json:"lastSeen"`
	WarningReached bool    `json:"warningReached"`
}

type dataUsageResponse struct {
	SessionStart       string              `json:"sessionStart"`
	SessionSeconds     int64               `json:"sessionSeconds"`
	TotalBytes         uint64              `json:"totalBytes"`
	UploadBytes        uint64              `json:"uploadBytes"`
	DownloadBytes      uint64              `json:"downloadBytes"`
	UploadBps          float64             `json:"uploadBps"`
	DownloadBps        float64             `json:"downloadBps"`
	WANOnline          bool                `json:"wanOnline"`
	APInterface        string              `json:"apInterface"`
	WANInterface       string              `json:"wanInterface"`
	Settings           dataUsageSettings   `json:"settings"`
	Clients            []dataUsageClient   `json:"clients"`
	RecentEvents       []dataUsageLogRecord `json:"recentEvents"`
	PersistentLogPath  string              `json:"persistentLogPath"`
	MonitoringActive   bool                `json:"monitoringActive"`
	SessionLimitReached bool               `json:"sessionLimitReached"`
}

type dataUsageLogRecord struct {
	Time     string `json:"time"`
	Type     string `json:"type"`
	IP       string `json:"ip,omitempty"`
	MAC      string `json:"mac,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	Message  string `json:"message"`
	Bytes    uint64 `json:"bytes,omitempty"`
}

type dataUsageCounter struct {
	Upload   uint64
	Download uint64
}

var (
	dataUsageMu       sync.Mutex
	dataUsageClients  = make(map[string]*dataUsageClient)
	dataUsagePrevious = make(map[string]dataUsageCounter)
	dataUsageWarned   = make(map[string]bool)
	dataUsageSessionStart = time.Now()
	dataUsageLastSample   = time.Now()
	dataUsageConfig = dataUsageSettings{
		WarningMB:        250,
		AutoBlockMB:      500,
		SessionLimitMB:   1000,
		AutoBlockEnabled: true,
	}
	dataUsageStorageDir string
	dataUsageLogPath    string
	dataUsageConfigPath string
	dataUsageInitialized bool
	dataUsageSessionLimitReached bool
)

func init() {
	http.HandleFunc("/dataUsage", handleDataUsageGet)
	http.HandleFunc("/dataUsage/block", handleDataUsageBlock)
	http.HandleFunc("/dataUsage/settings", handleDataUsageSettings)
	http.HandleFunc("/dataUsage/reset", handleDataUsageReset)
	http.HandleFunc("/dataUsage/rename", handleDeviceRename)
	go dataUsageMonitorLoop()
}

func dataUsageMonitorLoop() {
	time.Sleep(3 * time.Second)
	initializeDataUsage()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastSnapshot := time.Now()
	for range ticker.C {
		refreshDataUsage()
		if time.Since(lastSnapshot) >= time.Minute {
			dataUsageMu.Lock()
			total := uint64(0)
			for _, c := range dataUsageClients {
				total += c.TotalBytes
			}
			dataUsageMu.Unlock()
			appendDataUsageLog(dataUsageLogRecord{
				Time: time.Now().UTC().Format(time.RFC3339), Type: "snapshot",
				Message: "Periodic session usage snapshot", Bytes: total,
			})
			lastSnapshot = time.Now()
		}
	}
}

func initializeDataUsage() {
	dataUsageMu.Lock()
	if dataUsageInitialized {
		dataUsageMu.Unlock()
		return
	}
	dataUsageStorageDir = chooseDataUsageStorageDir()
	dataUsageLogPath = filepath.Join(dataUsageStorageDir, "data-usage.jsonl")
	dataUsageConfigPath = filepath.Join(dataUsageStorageDir, "data-usage-settings.json")
	loadDataUsageSettingsLocked()
	dataUsageInitialized = true
	dataUsageMu.Unlock()

	ensureDataUsageChains()
	loadDeviceManualNames()
	appendDataUsageLog(dataUsageLogRecord{
		Time: time.Now().UTC().Format(time.RFC3339), Type: "session_start",
		Message: "Stratux internet data monitoring session started",
	})
}

func chooseDataUsageStorageDir() string {
	candidates := []string{
		"/boot/firmware/stratux-data-monitor",
		"/boot/stratux-data-monitor",
		"/var/log/stratux-data-monitor",
		"/tmp/stratux-data-monitor",
	}
	for _, dir := range candidates {
		if err := os.MkdirAll(dir, 0755); err != nil {
			continue
		}
		test := filepath.Join(dir, ".write-test")
		if err := os.WriteFile(test, []byte("ok"), 0644); err == nil {
			_ = os.Remove(test)
			return dir
		}
	}
	return "/tmp"
}

func loadDataUsageSettingsLocked() {
	b, err := os.ReadFile(dataUsageConfigPath)
	if err != nil {
		return
	}
	var cfg dataUsageSettings
	if json.Unmarshal(b, &cfg) == nil {
		if cfg.WarningMB > 0 {
			dataUsageConfig.WarningMB = cfg.WarningMB
		}
		if cfg.AutoBlockMB > 0 {
			dataUsageConfig.AutoBlockMB = cfg.AutoBlockMB
		}
		if cfg.SessionLimitMB > 0 {
			dataUsageConfig.SessionLimitMB = cfg.SessionLimitMB
		}
		dataUsageConfig.AutoBlockEnabled = cfg.AutoBlockEnabled
	}
}

func saveDataUsageSettingsLocked() {
	b, _ := json.MarshalIndent(dataUsageConfig, "", "  ")
	_ = os.WriteFile(dataUsageConfigPath, b, 0644)
}

func appendDataUsageLog(rec dataUsageLogRecord) {
	if rec.Time == "" {
		rec.Time = time.Now().UTC().Format(time.RFC3339)
	}
	dataUsageMu.Lock()
	path := dataUsageLogPath
	dataUsageMu.Unlock()
	if path == "" {
		return
	}
	b, _ := json.Marshal(rec)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func recentDataUsageEvents(limit int) []dataUsageLogRecord {
	dataUsageMu.Lock()
	path := dataUsageLogPath
	dataUsageMu.Unlock()
	if path == "" || limit <= 0 {
		return []dataUsageLogRecord{}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return []dataUsageLogRecord{}
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	start := 0
	if len(lines) > limit {
		start = len(lines) - limit
	}
	result := make([]dataUsageLogRecord, 0, len(lines)-start)
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		var rec dataUsageLogRecord
		if json.Unmarshal([]byte(lines[i]), &rec) == nil {
			result = append(result, rec)
		}
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

func runIptables(args ...string) error {
	cmd := exec.Command("iptables", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("iptables %v: %v (%s)", args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func iptablesRuleExists(args ...string) bool {
	return runIptables(args...) == nil
}

func ensureDataUsageChains() {
	_ = runIptables("-N", dataUsageOutChain)
	_ = runIptables("-N", dataUsageInChain)
	_ = runIptables("-N", dataBlockChain)

	// CRITICAL: allow all intra-AP traffic first. Devices on the AP network
	// must always reach Stratux services (GDL90 port 4000, AHRS, web) and
	// each other, regardless of internet blocking state. Without this rule,
	// AP+Client mode can break GDL90/AV-Link/UAVionix communication.
	if !iptablesRuleExists("-C", "FORWARD", "-i", "ap0", "-o", "ap0", "-j", "ACCEPT") {
		_ = runIptables("-I", "FORWARD", "1", "-i", "ap0", "-o", "ap0", "-j", "ACCEPT")
	}

	// Stratux service ports must never be blocked from AP clients. GDL90
	// uses UDP 4000 (ForeFlight, AV-Link, SkyRadar). Web UI is on TCP 80.
	for _, port := range []string{"4000", "80"} {
		if !iptablesRuleExists("-C", "INPUT", "-i", "ap0", "-p", "udp", "--dport", port, "-j", "ACCEPT") {
			_ = runIptables("-I", "INPUT", "1", "-i", "ap0", "-p", "udp", "--dport", port, "-j", "ACCEPT")
		}
		if !iptablesRuleExists("-C", "INPUT", "-i", "ap0", "-p", "tcp", "--dport", port, "-j", "ACCEPT") {
			_ = runIptables("-I", "INPUT", "1", "-i", "ap0", "-p", "tcp", "--dport", port, "-j", "ACCEPT")
		}
	}

	if !iptablesRuleExists("-C", "FORWARD", "-j", dataBlockChain) {
		_ = runIptables("-I", "FORWARD", "2", "-j", dataBlockChain)
	}
	if !iptablesRuleExists("-C", "FORWARD", "-i", "ap0", "-o", "wlan0", "-j", dataUsageOutChain) {
		_ = runIptables("-I", "FORWARD", "3", "-i", "ap0", "-o", "wlan0", "-j", dataUsageOutChain)
	}
	if !iptablesRuleExists("-C", "FORWARD", "-i", "wlan0", "-o", "ap0", "-j", dataUsageInChain) {
		_ = runIptables("-I", "FORWARD", "3", "-i", "wlan0", "-o", "ap0", "-j", dataUsageInChain)
	}
}

func ensureClientCounterRules(ip string) {
	if net.ParseIP(ip) == nil {
		return
	}
	if !iptablesRuleExists("-C", dataUsageOutChain, "-s", ip, "-j", "RETURN") {
		_ = runIptables("-A", dataUsageOutChain, "-s", ip, "-j", "RETURN")
	}
	if !iptablesRuleExists("-C", dataUsageInChain, "-d", ip, "-j", "RETURN") {
		_ = runIptables("-A", dataUsageInChain, "-d", ip, "-j", "RETURN")
	}
}

func setClientBlocked(ip string, blocked bool) error {
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("invalid IP address")
	}
	if blocked {
		// Only block internet-bound traffic (via wlan0). Local AP traffic
		// (GDL90, AV-Link, Stratux web) must never be affected.
		if !iptablesRuleExists("-C", dataBlockChain, "-s", ip, "-o", "wlan0", "-j", "DROP") {
			if err := runIptables("-A", dataBlockChain, "-s", ip, "-o", "wlan0", "-j", "DROP"); err != nil {
				return err
			}
		}
		if !iptablesRuleExists("-C", dataBlockChain, "-i", "wlan0", "-d", ip, "-j", "DROP") {
			_ = runIptables("-A", dataBlockChain, "-i", "wlan0", "-d", ip, "-j", "DROP")
		}
		// Clean up any legacy unqualified rules.
		for iptablesRuleExists("-C", dataBlockChain, "-s", ip, "-j", "DROP") {
			_ = runIptables("-D", dataBlockChain, "-s", ip, "-j", "DROP")
		}
		for iptablesRuleExists("-C", dataBlockChain, "-d", ip, "-j", "DROP") {
			_ = runIptables("-D", dataBlockChain, "-d", ip, "-j", "DROP")
		}
	} else {
		// Remove all block rules for this IP (both new qualified and legacy).
		for iptablesRuleExists("-C", dataBlockChain, "-s", ip, "-o", "wlan0", "-j", "DROP") {
			_ = runIptables("-D", dataBlockChain, "-s", ip, "-o", "wlan0", "-j", "DROP")
		}
		for iptablesRuleExists("-C", dataBlockChain, "-i", "wlan0", "-d", ip, "-j", "DROP") {
			_ = runIptables("-D", dataBlockChain, "-i", "wlan0", "-d", ip, "-j", "DROP")
		}
		for iptablesRuleExists("-C", dataBlockChain, "-s", ip, "-j", "DROP") {
			_ = runIptables("-D", dataBlockChain, "-s", ip, "-j", "DROP")
		}
		for iptablesRuleExists("-C", dataBlockChain, "-d", ip, "-j", "DROP") {
			_ = runIptables("-D", dataBlockChain, "-d", ip, "-j", "DROP")
		}
	}
	dataUsageMu.Lock()
	if c, ok := dataUsageClients[ip]; ok {
		c.Blocked = blocked
	}
	dataUsageMu.Unlock()
	return nil
}

func currentBlockedIPs() map[string]bool {
	result := make(map[string]bool)
	out, err := exec.Command("iptables-save", "-c").Output()
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "-A "+dataBlockChain+" ") || !strings.Contains(line, "-j DROP") {
			continue
		}
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "-s" && i+1 < len(fields) {
				result[strings.Split(fields[i+1], "/")[0]] = true
			}
		}
	}
	return result
}

func readChainCounters(chain string, source bool) map[string]uint64 {
	result := make(map[string]uint64)
	out, err := exec.Command("iptables-save", "-c").Output()
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "-A "+chain+" ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		counter := strings.Trim(fields[0], "[]")
		parts := strings.Split(counter, ":")
		if len(parts) != 2 {
			continue
		}
		bytes, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			continue
		}
		needle := "-d"
		if source {
			needle = "-s"
		}
		for i, f := range fields {
			if f == needle && i+1 < len(fields) {
				ip := strings.Split(fields[i+1], "/")[0]
				result[ip] = bytes
				break
			}
		}
	}
	return result
}

func discoverAPClients() map[string][2]string {
	clients := make(map[string][2]string)
	out, err := exec.Command("ip", "neigh", "show", "dev", "ap0").Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 1 || net.ParseIP(fields[0]) == nil {
				continue
			}
			ip := fields[0]
			mac := ""
			for i, f := range fields {
				if f == "lladdr" && i+1 < len(fields) {
					mac = fields[i+1]
				}
			}
			clients[ip] = [2]string{mac, ""}
		}
	}

	leaseFiles := []string{
		"/var/lib/misc/dnsmasq.leases",
		"/var/lib/dnsmasq/dnsmasq.leases",
		"/tmp/dnsmasq.leases",
	}
	for _, leaseFile := range leaseFiles {
		b, err := os.ReadFile(leaseFile)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) < 4 || net.ParseIP(f[2]) == nil {
				continue
			}
			ip, mac, host := f[2], f[1], f[3]
			if host == "*" {
				host = ""
			}
			old := clients[ip]
			if old[0] != "" {
				mac = old[0]
			}
			clients[ip] = [2]string{mac, host}
		}
		break
	}

	// Enrich device names from multiple sources for clients without hostname.
	deviceMDNSCacheMu.Lock()
	manualCopy := make(map[string]string, len(deviceManualNames))
	for k, v := range deviceManualNames {
		manualCopy[k] = v
	}
	mdnsCopy := make(map[string]string, len(deviceMDNSCache))
	for k, v := range deviceMDNSCache {
		mdnsCopy[k] = v
	}
	deviceMDNSCacheMu.Unlock()

	for ip, identity := range clients {
		mac, host := identity[0], identity[1]

		// 1. Manual name by MAC or IP (highest priority).
		if host == "" {
			if name, ok := manualCopy[strings.ToLower(mac)]; ok {
				host = name
			} else if name, ok := manualCopy[ip]; ok {
				host = name
			}
		}

		// 2. mDNS / Bonjour name (cached from async resolution).
		if host == "" {
			if name, ok := mdnsCopy[ip]; ok {
				host = name
			}
		}

		// 3. MAC vendor + private address fallback.
		if host == "" {
			host = deviceDisplayName(mac, "")
		}

		// Request mDNS resolution for next refresh if still unidentified.
		if identity[1] == "" && mac != "" {
			resolveViaMDNS(ip)
		}

		clients[ip] = [2]string{mac, host}
	}

	return clients
}

// ---------------------------------------------------------------------------
// Device identification: mDNS, MAC vendor lookup, manual names
// ---------------------------------------------------------------------------

var (
	deviceMDNSCache   = make(map[string]string)
	deviceMDNSCacheMu sync.Mutex
	deviceMDNSPending = make(map[string]bool)
	deviceManualNames = make(map[string]string)
	deviceManualPath  string
)

func isPrivateMAC(mac string) bool {
	mac = strings.TrimSpace(mac)
	if len(mac) < 2 {
		return false
	}
	firstByte, err := strconv.ParseUint(strings.ReplaceAll(mac, ":", "")[:2], 16, 8)
	if err != nil {
		return false
	}
	return firstByte&0x02 != 0
}

func macVendorName(mac string) string {
	mac = strings.ToLower(strings.TrimSpace(mac))
	if len(mac) < 8 {
		return ""
	}
	prefix := mac[:8]
	vendors := map[string]string{
		"00:cd:fe": "Apple", "28:6a:ba": "Apple", "3c:06:30": "Apple",
		"40:4d:7f": "Apple", "68:db:f5": "Apple", "78:7b:8a": "Apple",
		"9c:20:7b": "Apple", "a4:83:e7": "Apple", "ac:bc:32": "Apple",
		"b8:e8:56": "Apple", "c8:69:cd": "Apple", "d0:25:98": "Apple",
		"e0:5f:45": "Apple", "f0:18:98": "Apple", "14:7d:da": "Apple",
		"a8:5c:2c": "Apple", "f0:d4:f6": "Apple", "64:b0:a6": "Apple",
		"dc:a4:ca": "Apple", "88:66:a5": "Apple", "7c:d1:c3": "Apple",
		"f4:f9:51": "Apple", "8c:85:90": "Apple", "28:ff:3c": "Apple",
		"38:f9:d3": "Apple", "48:a9:1c": "Apple", "54:4e:90": "Apple",
		"60:c5:47": "Apple", "74:1b:b2": "Apple", "84:fc:fe": "Apple",
		"98:01:a7": "Apple", "a0:78:17": "Apple", "bc:52:b7": "Apple",
		"cc:08:8d": "Apple", "e4:25:e7": "Apple", "f8:ff:c2": "Apple",
		"00:17:f2": "Apple", "d8:1c:79": "Apple", "34:08:bc": "Apple",
		"18:65:90": "Apple", "00:1b:63": "Samsung", "00:21:19": "Samsung",
		"00:26:37": "Samsung", "08:37:3d": "Samsung", "10:1d:c0": "Samsung",
		"14:49:bc": "Samsung", "18:3a:2d": "Samsung", "1c:66:aa": "Samsung",
		"24:18:1d": "Samsung", "28:cc:01": "Samsung", "2c:ae:2b": "Samsung",
		"34:23:ba": "Samsung", "38:01:97": "Samsung", "40:4e:36": "Samsung",
		"44:78:3e": "Samsung", "4c:bc:48": "Samsung", "50:01:bb": "Samsung",
		"54:40:ad": "Samsung", "58:c3:8b": "Samsung", "6c:f3:73": "Samsung",
		"78:52:1a": "Samsung", "84:11:9e": "Samsung", "94:35:0a": "Samsung",
		"a0:82:1f": "Samsung", "b4:3a:28": "Samsung", "c0:97:27": "Samsung",
		"d0:87:e2": "Samsung", "e4:b0:21": "Samsung", "f8:04:2e": "Samsung",
		"b4:a9:fc": "Raspberry Pi", "d8:3a:dd": "Raspberry Pi",
		"dc:a6:32": "Raspberry Pi", "e4:5f:01": "Raspberry Pi",
		"2c:cf:67": "Raspberry Pi",
		"00:50:b6": "Intel", "3c:a9:f4": "Intel", "48:51:b7": "Intel",
		"68:17:29": "Intel", "80:86:f2": "Intel", "a0:36:9f": "Intel",
		"00:15:5d": "Microsoft", "28:18:78": "Microsoft",
		"7c:1e:52": "Google", "a4:77:33": "Google", "f4:f5:d8": "Google",
		"30:52:cb": "Google", "f8:0f:f9": "Google",
		"60:ab:d2": "Google", "94:eb:2c": "Google",
		"04:d3:b0": "Huawei", "24:09:95": "Huawei", "48:46:fb": "Huawei",
		"70:8c:b6": "Huawei", "80:b6:55": "Huawei", "c8:d7:19": "Huawei",
		"94:77:2b": "uAvionix",
	}
	if vendor, ok := vendors[prefix]; ok {
		return vendor
	}
	return ""
}

func deviceDisplayName(mac, hostname string) string {
	if hostname != "" {
		return hostname
	}
	if mac == "" {
		return ""
	}
	vendor := macVendorName(mac)
	if vendor != "" {
		return vendor + " device"
	}
	if isPrivateMAC(mac) {
		return "Private address device"
	}
	return ""
}

func resolveViaMDNS(ip string) {
	deviceMDNSCacheMu.Lock()
	if deviceMDNSPending[ip] {
		deviceMDNSCacheMu.Unlock()
		return
	}
	if _, ok := deviceMDNSCache[ip]; ok {
		deviceMDNSCacheMu.Unlock()
		return
	}
	deviceMDNSPending[ip] = true
	deviceMDNSCacheMu.Unlock()

	go func() {
		defer func() {
			deviceMDNSCacheMu.Lock()
			delete(deviceMDNSPending, ip)
			deviceMDNSCacheMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "avahi-resolve-address", "-4", ip).Output()
		if err != nil || len(out) == 0 {
			return
		}
		fields := strings.Fields(string(out))
		if len(fields) >= 2 {
			name := strings.TrimSuffix(strings.TrimSuffix(fields[1], "."), ".local")
			if name != "" && name != ip {
				deviceMDNSCacheMu.Lock()
				deviceMDNSCache[ip] = name
				deviceMDNSCacheMu.Unlock()
			}
		}
	}()
}

func loadDeviceManualNames() {
	dataUsageMu.Lock()
	path := dataUsageStorageDir
	dataUsageMu.Unlock()
	if path == "" {
		return
	}
	deviceManualPath = filepath.Join(path, "device-names.json")
	b, err := os.ReadFile(deviceManualPath)
	if err != nil {
		return
	}
	var names map[string]string
	if json.Unmarshal(b, &names) == nil {
		deviceMDNSCacheMu.Lock()
		deviceManualNames = names
		deviceMDNSCacheMu.Unlock()
	}
}

func saveDeviceManualNames() {
	if deviceManualPath == "" {
		return
	}
	deviceMDNSCacheMu.Lock()
	b, _ := json.MarshalIndent(deviceManualNames, "", "  ")
	deviceMDNSCacheMu.Unlock()
	_ = os.WriteFile(deviceManualPath, b, 0644)
}

func handleDeviceRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Key == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	deviceMDNSCacheMu.Lock()
	if req.Name == "" {
		delete(deviceManualNames, req.Key)
	} else {
		deviceManualNames[req.Key] = req.Name
	}
	deviceMDNSCacheMu.Unlock()
	saveDeviceManualNames()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func isWANOnline() bool {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "wlan0")
}

func refreshDataUsage() {
	ensureDataUsageChains()
	discovered := discoverAPClients()
	for ip := range discovered {
		ensureClientCounterRules(ip)
	}

	upload := readChainCounters(dataUsageOutChain, true)
	download := readChainCounters(dataUsageInChain, false)
	blocked := currentBlockedIPs()
	now := time.Now()

	dataUsageMu.Lock()
	deltaSeconds := now.Sub(dataUsageLastSample).Seconds()
	if deltaSeconds <= 0 {
		deltaSeconds = 2
	}
	for _, c := range dataUsageClients {
		c.Connected = false
		c.UploadBps = 0
		c.DownloadBps = 0
	}

	newConnections := make([]dataUsageLogRecord, 0)
	for ip, identity := range discovered {
		c, exists := dataUsageClients[ip]
		if !exists {
			c = &dataUsageClient{IP: ip}
			dataUsageClients[ip] = c
			newConnections = append(newConnections, dataUsageLogRecord{
				Time: now.UTC().Format(time.RFC3339), Type: "client_connected",
				IP: ip, MAC: identity[0], Hostname: identity[1], Message: "Client connected to Stratux",
			})
		}
		c.MAC = identity[0]
		if identity[1] != "" {
			c.Hostname = identity[1]
		}
		c.Connected = true
		c.LastSeen = now.UTC().Format(time.RFC3339)
		c.Blocked = blocked[ip]
		c.UploadBytes = upload[ip]
		c.DownloadBytes = download[ip]
		c.TotalBytes = c.UploadBytes + c.DownloadBytes
		prev := dataUsagePrevious[ip]
		if c.UploadBytes >= prev.Upload {
			c.UploadBps = float64(c.UploadBytes-prev.Upload) / deltaSeconds
		}
		if c.DownloadBytes >= prev.Download {
			c.DownloadBps = float64(c.DownloadBytes-prev.Download) / deltaSeconds
		}
		dataUsagePrevious[ip] = dataUsageCounter{Upload: c.UploadBytes, Download: c.DownloadBytes}
	}

	total := uint64(0)
	for _, c := range dataUsageClients {
		total += c.TotalBytes
	}
	cfg := dataUsageConfig
	dataUsageLastSample = now
	dataUsageMu.Unlock()

	for _, rec := range newConnections {
		appendDataUsageLog(rec)
	}

	warningBytes := cfg.WarningMB * 1024 * 1024
	autoBlockBytes := cfg.AutoBlockMB * 1024 * 1024
	for ip := range discovered {
		dataUsageMu.Lock()
		c := dataUsageClients[ip]
		shouldWarn := c != nil && warningBytes > 0 && c.TotalBytes >= warningBytes && !dataUsageWarned[ip]
		if shouldWarn {
			dataUsageWarned[ip] = true
			c.WarningReached = true
		}
		shouldBlock := c != nil && cfg.AutoBlockEnabled && autoBlockBytes > 0 && c.TotalBytes >= autoBlockBytes && !c.Blocked
		var snapshot dataUsageClient
		if c != nil {
			snapshot = *c
		}
		dataUsageMu.Unlock()
		if shouldWarn {
			appendDataUsageLog(dataUsageLogRecord{Time: now.UTC().Format(time.RFC3339), Type: "warning", IP: ip, MAC: snapshot.MAC, Hostname: snapshot.Hostname, Message: fmt.Sprintf("Client exceeded warning threshold of %d MB", cfg.WarningMB), Bytes: snapshot.TotalBytes})
		}
		if shouldBlock {
			if setClientBlocked(ip, true) == nil {
				appendDataUsageLog(dataUsageLogRecord{Time: now.UTC().Format(time.RFC3339), Type: "auto_block", IP: ip, MAC: snapshot.MAC, Hostname: snapshot.Hostname, Message: fmt.Sprintf("Internet automatically blocked after exceeding %d MB", cfg.AutoBlockMB), Bytes: snapshot.TotalBytes})
			}
		}
	}

	sessionLimitBytes := cfg.SessionLimitMB * 1024 * 1024
	if cfg.AutoBlockEnabled && sessionLimitBytes > 0 && total >= sessionLimitBytes {
		dataUsageMu.Lock()
		firstHit := !dataUsageSessionLimitReached
		dataUsageSessionLimitReached = true
		dataUsageMu.Unlock()
		for ip := range discovered {
			_ = setClientBlocked(ip, true)
		}
		if firstHit {
			appendDataUsageLog(dataUsageLogRecord{Time: now.UTC().Format(time.RFC3339), Type: "session_limit", Message: fmt.Sprintf("Session hard limit of %d MB reached; all client Internet access blocked", cfg.SessionLimitMB), Bytes: total})
		}
	}
}

func handleDataUsageGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	initializeDataUsage()
	dataUsageMu.Lock()
	resp := dataUsageResponse{
		SessionStart: dataUsageSessionStart.UTC().Format(time.RFC3339),
		SessionSeconds: int64(time.Since(dataUsageSessionStart).Seconds()),
		APInterface: "ap0", WANInterface: "wlan0",
		Settings: dataUsageConfig, PersistentLogPath: dataUsageLogPath,
		MonitoringActive: true, SessionLimitReached: dataUsageSessionLimitReached,
	}
	for _, c := range dataUsageClients {
		copyClient := *c
		resp.Clients = append(resp.Clients, copyClient)
		resp.UploadBytes += c.UploadBytes
		resp.DownloadBytes += c.DownloadBytes
		resp.UploadBps += c.UploadBps
		resp.DownloadBps += c.DownloadBps
	}
	resp.TotalBytes = resp.UploadBytes + resp.DownloadBytes
	dataUsageMu.Unlock()
	resp.WANOnline = isWANOnline()
	resp.RecentEvents = recentDataUsageEvents(40)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

func handleDataUsageBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		IP      string `json:"ip"`
		Blocked bool   `json:"blocked"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || net.ParseIP(req.IP) == nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := setClientBlocked(req.IP, req.Blocked); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dataUsageMu.Lock()
	c := dataUsageClients[req.IP]
	var mac, host string
	var bytes uint64
	if c != nil {
		mac, host, bytes = c.MAC, c.Hostname, c.TotalBytes
	}
	dataUsageMu.Unlock()
	action := "manual_unblock"
	message := "Internet access manually restored"
	if req.Blocked {
		action = "manual_block"
		message = "Internet access manually blocked"
	}
	appendDataUsageLog(dataUsageLogRecord{Time: time.Now().UTC().Format(time.RFC3339), Type: action, IP: req.IP, MAC: mac, Hostname: host, Message: message, Bytes: bytes})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func handleDataUsageSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var cfg dataUsageSettings
	if json.NewDecoder(r.Body).Decode(&cfg) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if cfg.WarningMB == 0 || cfg.AutoBlockMB == 0 || cfg.SessionLimitMB == 0 || cfg.WarningMB > cfg.AutoBlockMB || cfg.AutoBlockMB > cfg.SessionLimitMB {
		http.Error(w, "limits must satisfy 0 < warning <= auto block <= session limit", http.StatusBadRequest)
		return
	}
	dataUsageMu.Lock()
	dataUsageConfig = cfg
	saveDataUsageSettingsLocked()
	dataUsageMu.Unlock()
	appendDataUsageLog(dataUsageLogRecord{Time: time.Now().UTC().Format(time.RFC3339), Type: "settings", Message: fmt.Sprintf("Safety limits updated: warning %d MB, auto-block %d MB/device, session %d MB, automatic protection %t", cfg.WarningMB, cfg.AutoBlockMB, cfg.SessionLimitMB, cfg.AutoBlockEnabled)})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func handleDataUsageReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	_ = runIptables("-Z", dataUsageOutChain)
	_ = runIptables("-Z", dataUsageInChain)
	dataUsageMu.Lock()
	dataUsagePrevious = make(map[string]dataUsageCounter)
	dataUsageWarned = make(map[string]bool)
	for _, c := range dataUsageClients {
		c.UploadBytes, c.DownloadBytes, c.TotalBytes = 0, 0, 0
		c.UploadBps, c.DownloadBps = 0, 0
		c.WarningReached = false
	}
	dataUsageSessionStart = time.Now()
	dataUsageLastSample = time.Now()
	dataUsageSessionLimitReached = false
	dataUsageMu.Unlock()
	appendDataUsageLog(dataUsageLogRecord{Time: time.Now().UTC().Format(time.RFC3339), Type: "session_reset", Message: "Data usage session counters manually reset"})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
