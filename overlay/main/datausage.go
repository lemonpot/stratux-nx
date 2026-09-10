package main

import (
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

	if !iptablesRuleExists("-C", "FORWARD", "-j", dataBlockChain) {
		_ = runIptables("-I", "FORWARD", "1", "-j", dataBlockChain)
	}
	if !iptablesRuleExists("-C", "FORWARD", "-i", "ap0", "-o", "wlan0", "-j", dataUsageOutChain) {
		_ = runIptables("-I", "FORWARD", "2", "-i", "ap0", "-o", "wlan0", "-j", dataUsageOutChain)
	}
	if !iptablesRuleExists("-C", "FORWARD", "-i", "wlan0", "-o", "ap0", "-j", dataUsageInChain) {
		_ = runIptables("-I", "FORWARD", "2", "-i", "wlan0", "-o", "ap0", "-j", dataUsageInChain)
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
		if !iptablesRuleExists("-C", dataBlockChain, "-s", ip, "-j", "DROP") {
			if err := runIptables("-A", dataBlockChain, "-s", ip, "-j", "DROP"); err != nil {
				return err
			}
		}
		if !iptablesRuleExists("-C", dataBlockChain, "-d", ip, "-j", "DROP") {
			_ = runIptables("-A", dataBlockChain, "-d", ip, "-j", "DROP")
		}
	} else {
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
			if (f == "-s" || f == "-d") && i+1 < len(fields) {
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
	return clients
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
