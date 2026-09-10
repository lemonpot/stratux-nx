package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	dataFlowKillChain = "STRATUX_FLOW_KILL"
	flowGuardDuration = 20 * time.Second
)

type internetFlow struct {
	ID             string  `json:"ID"`
	ClientIP       string  `json:"ClientIP"`
	ClientPort     int     `json:"ClientPort"`
	RemoteIP       string  `json:"RemoteIP"`
	RemotePort     int     `json:"RemotePort"`
	Protocol       string  `json:"Protocol"`
	ConnectionType string  `json:"ConnectionType"`
	State          string  `json:"State"`
	Hostname       string  `json:"Hostname"`
	HostnameSource string  `json:"HostnameSource"`
	HTTPPath       string  `json:"HTTPPath,omitempty"`
	Service        string  `json:"Service"`
	Category       string  `json:"Category"`
	UploadBytes    uint64  `json:"UploadBytes"`
	DownloadBytes  uint64  `json:"DownloadBytes"`
	TotalBytes     uint64  `json:"TotalBytes"`
	UploadBps      float64 `json:"UploadBps"`
	DownloadBps    float64 `json:"DownloadBps"`
	CurrentBps     float64 `json:"CurrentBps"`
	FirstSeen      string  `json:"FirstSeen"`
	LastSeen       string  `json:"LastSeen"`
	Killable       bool    `json:"Killable"`
	RecentlyKilled bool    `json:"RecentlyKilled"`
}

type internetFlowResponse struct {
	Flows                 []internetFlow `json:"Flows"`
	ActiveConnections     int            `json:"ActiveConnections"`
	IdentifiedConnections int            `json:"IdentifiedConnections"`
	PacketInspection      bool           `json:"PacketInspection"`
	ConntrackAccounting   bool           `json:"ConntrackAccounting"`
	GeneratedAt           string         `json:"GeneratedAt"`
	VisibilityNote        string         `json:"VisibilityNote"`
}

type conntrackTuple struct {
	src     string
	dst     string
	sport   int
	dport   int
	packets uint64
	bytes   uint64
}

type conntrackFlow struct {
	proto string
	state string
	orig  conntrackTuple
	reply conntrackTuple
}

type flowCounter struct {
	upload   uint64
	download uint64
}

type hostObservation struct {
	host   string
	source string
	path   string
	at     time.Time
}

var (
	internetFlowMu              sync.Mutex
	internetFlows               = make(map[string]internetFlow)
	internetFlowPrevious        = make(map[string]flowCounter)
	internetFlowFirstSeen       = make(map[string]time.Time)
	internetObservedHosts       = make(map[string]hostObservation)
	internetDNSNames            = make(map[string]hostObservation)
	internetPTRNames            = make(map[string]hostObservation)
	internetPTRPending          = make(map[string]bool)
	internetTLSBuffers          = make(map[string][]byte)
	internetTLSBufferTimes      = make(map[string]time.Time)
	internetRecentlyKilled      = make(map[string]time.Time)
	internetFlowLastSample      = time.Now()
	internetPacketInspection    bool
	internetConntrackAccounting bool
)

func init() {
	http.HandleFunc("/dataUsage/flows", handleInternetFlows)
	http.HandleFunc("/dataUsage/flow/kill", handleInternetFlowKill)
	go internetFlowMonitorLoop()
	go internetPacketMonitorLoop()
}

func internetFlowMonitorLoop() {
	time.Sleep(4 * time.Second)
	ensureFlowKillChain()
	if out, err := exec.Command("sysctl", "-w", "net.netfilter.nf_conntrack_acct=1").CombinedOutput(); err == nil {
		internetFlowMu.Lock()
		internetConntrackAccounting = true
		internetFlowMu.Unlock()
	} else {
		_ = out
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		refreshInternetFlows()
	}
}

func ensureFlowKillChain() {
	_ = runIptables("-N", dataFlowKillChain)
	if !iptablesRuleExists("-C", "FORWARD", "-j", dataFlowKillChain) {
		_ = runIptables("-I", "FORWARD", "1", "-j", dataFlowKillChain)
	}
}

func readConntrackTable() []byte {
	for _, path := range []string{"/proc/net/nf_conntrack", "/proc/net/ip_conntrack"} {
		if b, err := os.ReadFile(path); err == nil {
			return b
		}
	}
	return nil
}

func parseConntrackLine(line string) (conntrackFlow, bool) {
	var result conntrackFlow
	fields := strings.Fields(line)
	if len(fields) < 8 {
		return result, false
	}
	for _, f := range fields {
		if f == "tcp" || f == "udp" {
			result.proto = f
			break
		}
	}
	if result.proto == "" {
		return result, false
	}

	tuples := []*conntrackTuple{&result.orig, &result.reply}
	tupleIndex := 0
	for _, f := range fields {
		if !strings.Contains(f, "=") {
			if f == "ESTABLISHED" || f == "SYN_SENT" || f == "SYN_RECV" || f == "FIN_WAIT" || f == "TIME_WAIT" || f == "CLOSE" || f == "CLOSE_WAIT" || f == "LAST_ACK" || f == "LISTEN" {
				result.state = f
			}
			continue
		}
		parts := strings.SplitN(f, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := parts[0], parts[1]
		if key == "src" && result.orig.src != "" && result.reply.src == "" {
			tupleIndex = 1
		}
		t := tuples[tupleIndex]
		switch key {
		case "src":
			t.src = value
		case "dst":
			t.dst = value
		case "sport":
			t.sport, _ = strconv.Atoi(value)
		case "dport":
			t.dport, _ = strconv.Atoi(value)
		case "packets":
			t.packets, _ = strconv.ParseUint(value, 10, 64)
		case "bytes":
			t.bytes, _ = strconv.ParseUint(value, 10, 64)
		}
	}
	if result.orig.src == "" || result.orig.dst == "" || result.orig.sport == 0 || result.orig.dport == 0 {
		return result, false
	}
	if result.state == "" {
		result.state = "ACTIVE"
	}
	return result, true
}

func apClientNetworks() []*net.IPNet {
	var result []*net.IPNet
	if iface, err := net.InterfaceByName("ap0"); err == nil {
		if addrs, err := iface.Addrs(); err == nil {
			for _, addr := range addrs {
				_, network, err := net.ParseCIDR(addr.String())
				if err == nil && network != nil {
					result = append(result, network)
				}
			}
		}
	}
	if len(result) == 0 {
		_, fallback, _ := net.ParseCIDR("192.168.10.0/24")
		result = append(result, fallback)
	}
	return result
}

func ipInNetworks(ipText string, networks []*net.IPNet) bool {
	ip := net.ParseIP(ipText)
	if ip == nil {
		return false
	}
	for _, network := range networks {
		if network != nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func internetFlowID(proto, clientIP string, clientPort int, remoteIP string, remotePort int) string {
	return fmt.Sprintf("%s|%s|%d|%s|%d", proto, clientIP, clientPort, remoteIP, remotePort)
}

func refreshInternetFlows() {
	b := readConntrackTable()
	if len(b) == 0 {
		return
	}
	now := time.Now()
	networks := apClientNetworks()
	seen := make(map[string]bool)
	current := make(map[string]internetFlow)
	unknownIPs := make(map[string]bool)

	internetFlowMu.Lock()
	deltaSeconds := now.Sub(internetFlowLastSample).Seconds()
	if deltaSeconds <= 0 {
		deltaSeconds = 2
	}

	for _, line := range strings.Split(string(b), "\n") {
		ct, ok := parseConntrackLine(line)
		if !ok || !ipInNetworks(ct.orig.src, networks) || ct.orig.dst == "" {
			continue
		}
		id := internetFlowID(ct.proto, ct.orig.src, ct.orig.sport, ct.orig.dst, ct.orig.dport)
		seen[id] = true
		firstSeen := internetFlowFirstSeen[id]
		if firstSeen.IsZero() {
			firstSeen = now
			internetFlowFirstSeen[id] = firstSeen
		}

		prev := internetFlowPrevious[id]
		uploadBps, downloadBps := float64(0), float64(0)
		if ct.orig.bytes >= prev.upload {
			uploadBps = float64(ct.orig.bytes-prev.upload) / deltaSeconds
		}
		if ct.reply.bytes >= prev.download {
			downloadBps = float64(ct.reply.bytes-prev.download) / deltaSeconds
		}
		internetFlowPrevious[id] = flowCounter{upload: ct.orig.bytes, download: ct.reply.bytes}

		host, source, path := "", "", ""
		if observed, ok := internetObservedHosts[id]; ok && now.Sub(observed.at) < 2*time.Hour {
			host, source, path = observed.host, observed.source, observed.path
		} else if observed, ok := internetDNSNames[ct.orig.dst]; ok && now.Sub(observed.at) < 2*time.Hour {
			host, source = observed.host, observed.source
		} else if observed, ok := internetPTRNames[ct.orig.dst]; ok && now.Sub(observed.at) < 24*time.Hour {
			host, source = observed.host, observed.source
		} else {
			unknownIPs[ct.orig.dst] = true
		}

		service, category := classifyInternetService(host, ct.orig.dport, ct.proto)
		recentlyKilled := false
		if killedAt, ok := internetRecentlyKilled[id]; ok {
			recentlyKilled = now.Sub(killedAt) < flowGuardDuration+5*time.Second
		}
		flow := internetFlow{
			ID:             id,
			ClientIP:       ct.orig.src,
			ClientPort:     ct.orig.sport,
			RemoteIP:       ct.orig.dst,
			RemotePort:     ct.orig.dport,
			Protocol:       strings.ToUpper(ct.proto),
			ConnectionType: internetConnectionType(ct.proto, ct.orig.dport),
			State:          ct.state,
			Hostname:       host,
			HostnameSource: source,
			HTTPPath:       path,
			Service:        service,
			Category:       category,
			UploadBytes:    ct.orig.bytes,
			DownloadBytes:  ct.reply.bytes,
			TotalBytes:     ct.orig.bytes + ct.reply.bytes,
			UploadBps:      uploadBps,
			DownloadBps:    downloadBps,
			CurrentBps:     uploadBps + downloadBps,
			FirstSeen:      firstSeen.UTC().Format(time.RFC3339),
			LastSeen:       now.UTC().Format(time.RFC3339),
			Killable:       true,
			RecentlyKilled: recentlyKilled,
		}
		current[id] = flow
	}

	for id := range internetFlowPrevious {
		if !seen[id] {
			delete(internetFlowPrevious, id)
		}
	}
	for id, at := range internetFlowFirstSeen {
		if !seen[id] && now.Sub(at) > 10*time.Minute {
			delete(internetFlowFirstSeen, id)
		}
	}
	for id, at := range internetTLSBufferTimes {
		if now.Sub(at) > 20*time.Second {
			delete(internetTLSBufferTimes, id)
			delete(internetTLSBuffers, id)
		}
	}
	for id, at := range internetRecentlyKilled {
		if now.Sub(at) > time.Minute {
			delete(internetRecentlyKilled, id)
		}
	}
	internetFlows = current
	internetFlowLastSample = now
	internetFlowMu.Unlock()

	for ip := range unknownIPs {
		requestPTRLookup(ip)
	}
}

func requestPTRLookup(ip string) {
	if net.ParseIP(ip) == nil {
		return
	}
	internetFlowMu.Lock()
	if internetPTRPending[ip] {
		internetFlowMu.Unlock()
		return
	}
	if existing, ok := internetPTRNames[ip]; ok && time.Since(existing.at) < 24*time.Hour {
		internetFlowMu.Unlock()
		return
	}
	internetPTRPending[ip] = true
	internetFlowMu.Unlock()

	go func() {
		names, _ := net.LookupAddr(ip)
		host := ""
		if len(names) > 0 {
			host = normalizeHostname(names[0])
		}
		internetFlowMu.Lock()
		delete(internetPTRPending, ip)
		if host != "" {
			internetPTRNames[ip] = hostObservation{host: host, source: "PTR", at: time.Now()}
		}
		internetFlowMu.Unlock()
	}()
}

func normalizeHostname(host string) string {
	host = strings.TrimSpace(strings.TrimSuffix(strings.ToLower(host), "."))
	if len(host) > 253 {
		return ""
	}
	return host
}

func classifyInternetService(host string, port int, proto string) (string, string) {
	h := strings.ToLower(host)
	switch {
	case strings.Contains(h, "googlevideo.com") || strings.Contains(h, "youtube.com") || strings.Contains(h, "youtubei.googleapis.com") || strings.Contains(h, "ytimg.com"):
		return "YouTube", "Streaming video"
	case strings.Contains(h, "nflxvideo.net") || strings.Contains(h, "netflix.com"):
		return "Netflix", "Streaming video"
	case strings.Contains(h, "spotify.com") || strings.Contains(h, "scdn.co"):
		return "Spotify", "Streaming audio"
	case strings.Contains(h, "swcdn.apple.com") || strings.Contains(h, "swdist.apple.com") || strings.Contains(h, "mesu.apple.com") || strings.Contains(h, "gdmf.apple.com") || strings.Contains(h, "appldnld.apple.com") || strings.Contains(h, "updates.cdn-apple.com"):
		return "Apple Software Update", "Software update"
	case strings.Contains(h, "icloud.com") || strings.Contains(h, "icloud-content.com"):
		return "iCloud", "Cloud / sync"
	case strings.Contains(h, "apple.com") || strings.Contains(h, "mzstatic.com") || strings.Contains(h, "apple-dns.net"):
		return "Apple", "Apple service"
	case strings.Contains(h, "windowsupdate.com") || strings.Contains(h, "update.microsoft.com") || strings.Contains(h, "delivery.mp.microsoft.com"):
		return "Windows Update", "Software update"
	case strings.Contains(h, "microsoft.com") || strings.Contains(h, "office.com") || strings.Contains(h, "office365.com") || strings.Contains(h, "teams.microsoft.com"):
		return "Microsoft", "Microsoft service"
	case strings.Contains(h, "instagram.com") || strings.Contains(h, "cdninstagram.com"):
		return "Instagram", "Social media"
	case strings.Contains(h, "facebook.com") || strings.Contains(h, "fbcdn.net") || strings.Contains(h, "fbsbx.com"):
		return "Facebook / Meta", "Social media"
	case strings.Contains(h, "tiktok.com") || strings.Contains(h, "tiktokcdn.com") || strings.Contains(h, "byteoversea.com"):
		return "TikTok", "Streaming video"
	case strings.Contains(h, "google.com") || strings.Contains(h, "gstatic.com") || strings.Contains(h, "googleapis.com") || strings.Contains(h, "1e100.net"):
		return "Google", "Google service"
	case strings.Contains(h, "openai.com") || strings.Contains(h, "chatgpt.com"):
		return "OpenAI / ChatGPT", "Web / AI"
	case strings.Contains(h, "amazonaws.com") || strings.Contains(h, "cloudfront.net"):
		return "Amazon AWS / CloudFront", "Cloud / CDN"
	case strings.Contains(h, "cloudflare.com") || strings.Contains(h, "cloudflare.net"):
		return "Cloudflare", "Cloud / CDN"
	case strings.Contains(h, "akamai") || strings.Contains(h, "akamaized.net") || strings.Contains(h, "akamaihd.net"):
		return "Akamai CDN", "Cloud / CDN"
	case host != "":
		return host, "Internet service"
	case port == 443 && proto == "udp":
		return "Encrypted QUIC traffic", "Unknown"
	case port == 443:
		return "Encrypted HTTPS traffic", "Unknown"
	case port == 80:
		return "HTTP traffic", "Web"
	case port == 53:
		return "DNS", "Infrastructure"
	default:
		return fmt.Sprintf("Port %d", port), "Unknown"
	}
}

func internetConnectionType(proto string, port int) string {
	if proto == "udp" && port == 443 {
		return "QUIC / HTTP3"
	}
	if proto == "tcp" && port == 443 {
		return "HTTPS / TLS"
	}
	if proto == "tcp" && port == 80 {
		return "HTTP"
	}
	if port == 53 {
		return "DNS"
	}
	if port == 123 {
		return "NTP"
	}
	return strings.ToUpper(proto)
}

func handleInternetFlows(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	internetFlowMu.Lock()
	flows := make([]internetFlow, 0, len(internetFlows))
	identified := 0
	for _, flow := range internetFlows {
		flows = append(flows, flow)
		if flow.Hostname != "" {
			identified++
		}
	}
	packetInspection := internetPacketInspection
	conntrackAccounting := internetConntrackAccounting
	internetFlowMu.Unlock()

	sort.Slice(flows, func(i, j int) bool {
		if flows[i].CurrentBps == flows[j].CurrentBps {
			return flows[i].TotalBytes > flows[j].TotalBytes
		}
		return flows[i].CurrentBps > flows[j].CurrentBps
	})

	resp := internetFlowResponse{
		Flows:                 flows,
		ActiveConnections:     len(flows),
		IdentifiedConnections: identified,
		PacketInspection:      packetInspection,
		ConntrackAccounting:   conntrackAccounting,
		GeneratedAt:           time.Now().UTC().Format(time.RFC3339),
		VisibilityNote:        "HTTPS payloads remain encrypted. Hostnames are learned from DNS, TLS SNI or reverse DNS; exact URL paths are only visible for unencrypted HTTP.",
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

func handleInternetFlowKill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.ID == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	internetFlowMu.Lock()
	flow, ok := internetFlows[req.ID]
	internetFlowMu.Unlock()
	if !ok {
		http.Error(w, "connection is no longer active", http.StatusNotFound)
		return
	}

	if err := guardAndKillInternetFlow(flow); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	internetFlowMu.Lock()
	internetRecentlyKilled[flow.ID] = time.Now()
	internetFlowMu.Unlock()
	appendDataUsageLog(dataUsageLogRecord{
		Time:     time.Now().UTC().Format(time.RFC3339),
		Type:     "flow_kill",
		IP:       flow.ClientIP,
		Hostname: flow.Hostname,
		Message:  fmt.Sprintf("Killed %s connection to %s (%s:%d)", flow.ConnectionType, flow.Service, flow.RemoteIP, flow.RemotePort),
		Bytes:    flow.TotalBytes,
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "guardSeconds": int(flowGuardDuration.Seconds())})
}

func guardAndKillInternetFlow(flow internetFlow) error {
	ensureFlowKillChain()
	proto := strings.ToLower(flow.Protocol)
	if proto != "tcp" && proto != "udp" {
		return fmt.Errorf("unsupported protocol")
	}

	outRule := []string{"-p", proto, "-s", flow.ClientIP, "--sport", strconv.Itoa(flow.ClientPort), "-d", flow.RemoteIP, "--dport", strconv.Itoa(flow.RemotePort)}
	inRule := []string{"-p", proto, "-s", flow.RemoteIP, "--sport", strconv.Itoa(flow.RemotePort), "-d", flow.ClientIP, "--dport", strconv.Itoa(flow.ClientPort)}
	if proto == "tcp" {
		outRule = append(outRule, "-j", "REJECT", "--reject-with", "tcp-reset")
		inRule = append(inRule, "-j", "REJECT", "--reject-with", "tcp-reset")
	} else {
		outRule = append(outRule, "-j", "DROP")
		inRule = append(inRule, "-j", "DROP")
	}

	if err := runIptables(append([]string{"-I", dataFlowKillChain, "1"}, outRule...)...); err != nil {
		return err
	}
	if err := runIptables(append([]string{"-I", dataFlowKillChain, "1"}, inRule...)...); err != nil {
		_ = runIptables(append([]string{"-D", dataFlowKillChain}, outRule...)...)
		return err
	}

	conntrackArgs := []string{"-D", "-p", proto, "--orig-src", flow.ClientIP, "--orig-dst", flow.RemoteIP, "--sport", strconv.Itoa(flow.ClientPort), "--dport", strconv.Itoa(flow.RemotePort)}
	_, _ = exec.Command("conntrack", conntrackArgs...).CombinedOutput()

	go func() {
		time.Sleep(flowGuardDuration)
		_ = runIptables(append([]string{"-D", dataFlowKillChain}, outRule...)...)
		_ = runIptables(append([]string{"-D", dataFlowKillChain}, inRule...)...)
	}()
	return nil
}

func internetPacketMonitorLoop() {
	time.Sleep(5 * time.Second)
	for {
		if err := captureInternetMetadataOnAP(); err != nil {
			internetFlowMu.Lock()
			internetPacketInspection = false
			internetFlowMu.Unlock()
			time.Sleep(5 * time.Second)
			continue
		}
	}
}

func htons(value uint16) uint16 {
	return value<<8 | value>>8
}

func captureInternetMetadataOnAP() error {
	iface, err := net.InterfaceByName("ap0")
	if err != nil {
		return err
	}
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(htons(0x0003)))
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	if err := syscall.Bind(fd, &syscall.SockaddrLinklayer{Protocol: htons(0x0003), Ifindex: iface.Index}); err != nil {
		return err
	}
	internetFlowMu.Lock()
	internetPacketInspection = true
	internetFlowMu.Unlock()

	buf := make([]byte, 65535)
	for {
		n, _, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			return err
		}
		if n > 0 {
			inspectEthernetPacket(buf[:n])
		}
	}
}

func inspectEthernetPacket(packet []byte) {
	if len(packet) < 14 {
		return
	}
	offset := 14
	etherType := binary.BigEndian.Uint16(packet[12:14])
	if etherType == 0x8100 && len(packet) >= 18 {
		etherType = binary.BigEndian.Uint16(packet[16:18])
		offset = 18
	}
	if etherType != 0x0800 || len(packet) < offset+20 {
		return
	}
	ip := packet[offset:]
	if ip[0]>>4 != 4 {
		return
	}
	ihl := int(ip[0]&0x0f) * 4
	if ihl < 20 || len(ip) < ihl {
		return
	}
	srcIP := net.IP(ip[12:16]).String()
	dstIP := net.IP(ip[16:20]).String()
	proto := ip[9]
	payload := ip[ihl:]

	switch proto {
	case 17:
		inspectUDPPacket(srcIP, dstIP, payload)
	case 6:
		inspectTCPPacket(srcIP, dstIP, payload)
	}
}

func inspectUDPPacket(srcIP, dstIP string, segment []byte) {
	if len(segment) < 8 {
		return
	}
	sport := int(binary.BigEndian.Uint16(segment[0:2]))
	dport := int(binary.BigEndian.Uint16(segment[2:4]))
	payload := segment[8:]
	if sport == 53 {
		observeDNSResponse(payload)
	} else if dport == 53 {
		return
	}
}

func inspectTCPPacket(srcIP, dstIP string, segment []byte) {
	if len(segment) < 20 {
		return
	}
	sport := int(binary.BigEndian.Uint16(segment[0:2]))
	dport := int(binary.BigEndian.Uint16(segment[2:4]))
	headerLen := int(segment[12]>>4) * 4
	if headerLen < 20 || len(segment) <= headerLen {
		return
	}
	payload := segment[headerLen:]
	id := internetFlowID("tcp", srcIP, sport, dstIP, dport)

	if dport == 443 {
		observeTLSClientHello(id, payload)
		return
	}
	if dport == 80 {
		if host, path := parseHTTPRequest(payload); host != "" {
			internetFlowMu.Lock()
			internetObservedHosts[id] = hostObservation{host: host, source: "HTTP Host", path: path, at: time.Now()}
			internetFlowMu.Unlock()
		}
	}
}

func observeTLSClientHello(id string, payload []byte) {
	if len(payload) == 0 || len(payload) > 16384 {
		return
	}
	internetFlowMu.Lock()
	buf := internetTLSBuffers[id]
	if len(buf) == 0 && payload[0] != 0x16 {
		internetFlowMu.Unlock()
		return
	}
	if len(buf)+len(payload) > 20000 {
		delete(internetTLSBuffers, id)
		delete(internetTLSBufferTimes, id)
		internetFlowMu.Unlock()
		return
	}
	buf = append(buf, payload...)
	internetTLSBuffers[id] = buf
	internetTLSBufferTimes[id] = time.Now()
	host := parseTLSSNI(buf)
	if host != "" {
		internetObservedHosts[id] = hostObservation{host: host, source: "TLS SNI", at: time.Now()}
		delete(internetTLSBuffers, id)
		delete(internetTLSBufferTimes, id)
	}
	internetFlowMu.Unlock()
}

func parseTLSSNI(data []byte) string {
	if len(data) < 9 || data[0] != 0x16 {
		return ""
	}
	recordLen := int(binary.BigEndian.Uint16(data[3:5]))
	if recordLen <= 0 || len(data) < 5+recordLen {
		return ""
	}
	h := data[5 : 5+recordLen]
	if len(h) < 4 || h[0] != 0x01 {
		return ""
	}
	pos := 4
	if len(h) < pos+2+32+1 {
		return ""
	}
	pos += 2 + 32
	sessionLen := int(h[pos])
	pos++
	if len(h) < pos+sessionLen+2 {
		return ""
	}
	pos += sessionLen
	cipherLen := int(binary.BigEndian.Uint16(h[pos : pos+2]))
	pos += 2
	if len(h) < pos+cipherLen+1 {
		return ""
	}
	pos += cipherLen
	compressionLen := int(h[pos])
	pos++
	if len(h) < pos+compressionLen+2 {
		return ""
	}
	pos += compressionLen
	extensionsLen := int(binary.BigEndian.Uint16(h[pos : pos+2]))
	pos += 2
	end := pos + extensionsLen
	if end > len(h) {
		end = len(h)
	}
	for pos+4 <= end {
		extType := binary.BigEndian.Uint16(h[pos : pos+2])
		extLen := int(binary.BigEndian.Uint16(h[pos+2 : pos+4]))
		pos += 4
		if pos+extLen > end {
			break
		}
		if extType == 0 && extLen >= 5 {
			ext := h[pos : pos+extLen]
			listLen := int(binary.BigEndian.Uint16(ext[0:2]))
			p := 2
			limit := 2 + listLen
			if limit > len(ext) {
				limit = len(ext)
			}
			for p+3 <= limit {
				nameType := ext[p]
				nameLen := int(binary.BigEndian.Uint16(ext[p+1 : p+3]))
				p += 3
				if p+nameLen > limit {
					break
				}
				if nameType == 0 {
					return normalizeHostname(string(ext[p : p+nameLen]))
				}
				p += nameLen
			}
		}
		pos += extLen
	}
	return ""
}

func parseHTTPRequest(payload []byte) (string, string) {
	text := string(payload)
	methods := []string{"GET ", "POST ", "HEAD ", "PUT ", "DELETE ", "OPTIONS ", "PATCH "}
	valid := false
	for _, method := range methods {
		if strings.HasPrefix(text, method) {
			valid = true
			break
		}
	}
	if !valid {
		return "", ""
	}
	lines := strings.Split(text, "\r\n")
	if len(lines) == 0 {
		return "", ""
	}
	parts := strings.Fields(lines[0])
	path := ""
	if len(parts) >= 2 {
		path = parts[1]
		if len(path) > 300 {
			path = path[:300]
		}
	}
	for _, line := range lines[1:] {
		if strings.HasPrefix(strings.ToLower(line), "host:") {
			host := strings.TrimSpace(line[5:])
			if i := strings.LastIndex(host, ":"); i > 0 && !strings.Contains(host[i+1:], "]") {
				if _, err := strconv.Atoi(host[i+1:]); err == nil {
					host = host[:i]
				}
			}
			return normalizeHostname(host), path
		}
	}
	return "", path
}

func observeDNSResponse(msg []byte) {
	if len(msg) < 12 || msg[2]&0x80 == 0 {
		return
	}
	qdCount := int(binary.BigEndian.Uint16(msg[4:6]))
	anCount := int(binary.BigEndian.Uint16(msg[6:8]))
	if qdCount < 1 || anCount < 1 {
		return
	}
	offset := 12
	queryName := ""
	for i := 0; i < qdCount; i++ {
		name, next, ok := readDNSName(msg, offset)
		if !ok || next+4 > len(msg) {
			return
		}
		if queryName == "" {
			queryName = normalizeHostname(name)
		}
		offset = next + 4
	}
	if queryName == "" {
		return
	}

	for i := 0; i < anCount && offset < len(msg); i++ {
		_, next, ok := readDNSName(msg, offset)
		if !ok || next+10 > len(msg) {
			return
		}
		rrType := binary.BigEndian.Uint16(msg[next : next+2])
		rdLen := int(binary.BigEndian.Uint16(msg[next+8 : next+10]))
		rdata := next + 10
		if rdata+rdLen > len(msg) {
			return
		}
		var ip string
		if rrType == 1 && rdLen == 4 {
			ip = net.IP(msg[rdata : rdata+4]).String()
		} else if rrType == 28 && rdLen == 16 {
			ip = net.IP(msg[rdata : rdata+16]).String()
		}
		if ip != "" {
			internetFlowMu.Lock()
			internetDNSNames[ip] = hostObservation{host: queryName, source: "DNS", at: time.Now()}
			internetFlowMu.Unlock()
		}
		offset = rdata + rdLen
	}
}

func readDNSName(msg []byte, offset int) (string, int, bool) {
	if offset < 0 || offset >= len(msg) {
		return "", offset, false
	}
	labels := make([]string, 0, 8)
	next := offset
	jumped := false
	seen := make(map[int]bool)
	for steps := 0; steps < 128; steps++ {
		if offset >= len(msg) || seen[offset] {
			return "", next, false
		}
		seen[offset] = true
		length := int(msg[offset])
		if length == 0 {
			if !jumped {
				next = offset + 1
			}
			return strings.Join(labels, "."), next, true
		}
		if length&0xC0 == 0xC0 {
			if offset+1 >= len(msg) {
				return "", next, false
			}
			ptr := ((length & 0x3F) << 8) | int(msg[offset+1])
			if !jumped {
				next = offset + 2
			}
			offset = ptr
			jumped = true
			continue
		}
		if length > 63 || offset+1+length > len(msg) {
			return "", next, false
		}
		labels = append(labels, string(msg[offset+1:offset+1+length]))
		offset += 1 + length
		if !jumped {
			next = offset
		}
	}
	return "", next, false
}
