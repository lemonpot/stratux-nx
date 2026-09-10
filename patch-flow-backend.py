#!/usr/bin/env python3
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
p = root / "main/datausage_flows.go"
s = p.read_text()

old = '''func readConntrackTable() []byte {
\tfor _, path := range []string{"/proc/net/nf_conntrack", "/proc/net/ip_conntrack"} {
\t\tif b, err := os.ReadFile(path); err == nil {
\t\t\treturn b
\t\t}
\t}
\treturn nil
}'''
new = '''func readConntrackTable() []byte {
\tfor _, path := range []string{"/proc/net/nf_conntrack", "/proc/net/ip_conntrack"} {
\t\tif b, err := os.ReadFile(path); err == nil && len(b) > 0 {
\t\t\treturn b
\t\t}
\t}
\t// Modern Raspberry Pi kernels commonly do not expose /proc/net/nf_conntrack.
\t// Use the conntrack netlink utility instead. Use an absolute path because
\t// the systemd service PATH is not guaranteed to include /usr/sbin.
\tif b, err := exec.Command("/usr/sbin/conntrack", "-L", "-f", "ipv4", "-o", "extended").Output(); err == nil {
\t\treturn b
\t}
\treturn nil
}'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    s = s.replace('exec.Command("conntrack", "-L", "-o", "extended")', 'exec.Command("/usr/sbin/conntrack", "-L", "-f", "ipv4", "-o", "extended")')

old = '''\t\tprev := internetFlowPrevious[id]
\t\tuploadBps, downloadBps := float64(0), float64(0)
\t\tif ct.orig.bytes >= prev.upload {
\t\t\tuploadBps = float64(ct.orig.bytes-prev.upload) / deltaSeconds
\t\t}
\t\tif ct.reply.bytes >= prev.download {
\t\t\tdownloadBps = float64(ct.reply.bytes-prev.download) / deltaSeconds
\t\t}
\t\tinternetFlowPrevious[id] = flowCounter{upload: ct.orig.bytes, download: ct.reply.bytes}'''
new = '''\t\tprev, hadPrevious := internetFlowPrevious[id]
\t\tuploadBps, downloadBps := float64(0), float64(0)
\t\tif hadPrevious && ct.orig.bytes >= prev.upload {
\t\t\tuploadBps = float64(ct.orig.bytes-prev.upload) / deltaSeconds
\t\t}
\t\tif hadPrevious && ct.reply.bytes >= prev.download {
\t\t\tdownloadBps = float64(ct.reply.bytes-prev.download) / deltaSeconds
\t\t}
\t\tinternetFlowPrevious[id] = flowCounter{upload: ct.orig.bytes, download: ct.reply.bytes}'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit("Could not patch first-sample flow rate")

old = '''\tnow := time.Now()
\tnetworks := apClientNetworks()'''
new = '''\tnow := time.Now()
\tif strings.Contains(string(b), "bytes=") {
\t\tinternetFlowMu.Lock()
\t\tinternetConntrackAccounting = true
\t\tinternetFlowMu.Unlock()
\t}
\tnetworks := apClientNetworks()'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit("Could not patch conntrack accounting detection")

# Show only forwarded Internet flows. Local connections to Stratux itself are not
# Internet usage and were cluttering the list. Also suppress already-dead TCP states.
old = '''\t\tct, ok := parseConntrackLine(line)
\t\tif !ok || !ipInNetworks(ct.orig.src, networks) || ct.orig.dst == "" {
\t\t\tcontinue
\t\t}
\t\tid := internetFlowID(ct.proto, ct.orig.src, ct.orig.sport, ct.orig.dst, ct.orig.dport)'''
new = '''\t\tct, ok := parseConntrackLine(line)
\t\tif !ok || !ipInNetworks(ct.orig.src, networks) || ct.orig.dst == "" || ipInNetworks(ct.orig.dst, networks) {
\t\t\tcontinue
\t\t}
\t\tif ct.proto == "tcp" && (ct.state == "TIME_WAIT" || ct.state == "CLOSE" || ct.state == "LAST_ACK") {
\t\t\tcontinue
\t\t}
\t\tid := internetFlowID(ct.proto, ct.orig.src, ct.orig.sport, ct.orig.dst, ct.orig.dport)'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit("Could not patch Internet-only active flow filter")

# nf_conntrack_acct can appear after Stratux starts. A one-shot sysctl at startup
# races with kernel/module initialization on some Raspberry Pi images. Keep retrying
# until the proc setting really reads back as 1.
old = '''func internetFlowMonitorLoop() {
\ttime.Sleep(4 * time.Second)
\tensureFlowKillChain()
\tif out, err := exec.Command("sysctl", "-w", "net.netfilter.nf_conntrack_acct=1").CombinedOutput(); err == nil {
\t\tinternetFlowMu.Lock()
\t\tinternetConntrackAccounting = true
\t\tinternetFlowMu.Unlock()
\t} else {
\t\t_ = out
\t}

\tticker := time.NewTicker(2 * time.Second)
\tdefer ticker.Stop()
\tfor range ticker.C {
\t\trefreshInternetFlows()
\t}
}'''
new = '''func setInternetConntrackAccounting() bool {
\tpath := "/proc/sys/net/netfilter/nf_conntrack_acct"
\tif b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) == "1" {
\t\treturn true
\t}
\tif err := os.WriteFile(path, []byte("1\\n"), 0644); err == nil {
\t\tif b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) == "1" {
\t\t\treturn true
\t\t}
\t}
\t_, _ = exec.Command("/usr/sbin/sysctl", "-w", "net.netfilter.nf_conntrack_acct=1").CombinedOutput()
\tb, err := os.ReadFile(path)
\treturn err == nil && strings.TrimSpace(string(b)) == "1"
}

func internetFlowMonitorLoop() {
\ttime.Sleep(4 * time.Second)
\tensureFlowKillChain()

\tticker := time.NewTicker(2 * time.Second)
\tdefer ticker.Stop()
\tfor {
\t\taccounting := setInternetConntrackAccounting()
\t\tinternetFlowMu.Lock()
\t\tinternetConntrackAccounting = accounting
\t\tinternetFlowMu.Unlock()
\t\trefreshInternetFlows()
\t\t<-ticker.C
\t}
}'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit("Could not patch conntrack accounting retry loop")

# systemd's PATH may not include /usr/sbin. Use absolute paths for networking tools.
s = s.replace('exec.Command("sysctl", "-w", "net.netfilter.nf_conntrack_acct=1")',
              'exec.Command("/usr/sbin/sysctl", "-w", "net.netfilter.nf_conntrack_acct=1")')
s = s.replace('exec.Command("conntrack", conntrackArgs...)',
              'exec.Command("/usr/sbin/conntrack", conntrackArgs...)')

# Keep the Apple software-update classifier broad enough for current Apple CDN/update hosts.
old = '''\tcase strings.Contains(h, "swcdn.apple.com") || strings.Contains(h, "swdist.apple.com") || strings.Contains(h, "mesu.apple.com") || strings.Contains(h, "gdmf.apple.com") || strings.Contains(h, "appldnld.apple.com") || strings.Contains(h, "updates.cdn-apple.com"):
\t\treturn "Apple Software Update", "Software update"'''
new = '''\tcase strings.Contains(h, "swcdn.apple.com") || strings.Contains(h, "swdist.apple.com") || strings.Contains(h, "swdownload.apple.com") || strings.Contains(h, "swscan.apple.com") || strings.Contains(h, "mesu.apple.com") || strings.Contains(h, "gdmf.apple.com") || strings.Contains(h, "gdmf-ados.apple.com") || strings.Contains(h, "appldnld.apple.com") || strings.Contains(h, "updates.cdn-apple.com") || strings.Contains(h, "updates-http.cdn-apple.com") || strings.Contains(h, "oscdn.apple.com") || strings.Contains(h, "osrecovery.apple.com") || strings.Contains(h, "configuration.apple.com") || strings.Contains(h, "gg.apple.com") || strings.Contains(h, "gs.apple.com") || strings.Contains(h, "ig.apple.com") || strings.Contains(h, "skl.apple.com") || strings.Contains(h, "xp.apple.com") || strings.Contains(h, "gsra.apple.com") || strings.Contains(h, "wkms-public.apple.com") || strings.Contains(h, "fcs-keys-pub-prod.cdn-apple.com"):
\t\treturn "Apple Software Update", "Software update"'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit("Could not patch Apple update hostname classifier")

# Keep cumulative per-service usage for the whole Internet Data session. Active
# conntrack entries come and go, so summing only the current flow list cannot tell
# which service consumed a large amount a few minutes ago.
old = '''type internetFlowResponse struct {
\tFlows                 []internetFlow `json:"Flows"`
\tActiveConnections     int            `json:"ActiveConnections"`
\tIdentifiedConnections int            `json:"IdentifiedConnections"`
\tPacketInspection      bool           `json:"PacketInspection"`
\tConntrackAccounting   bool           `json:"ConntrackAccounting"`
\tGeneratedAt           string         `json:"GeneratedAt"`
\tVisibilityNote        string         `json:"VisibilityNote"`
}'''
new = '''type internetFlowResponse struct {
\tFlows                 []internetFlow         `json:"Flows"`
\tServices              []internetServiceUsage `json:"Services"`
\tTrackedBytes          uint64                 `json:"TrackedBytes"`
\tActiveConnections     int                    `json:"ActiveConnections"`
\tIdentifiedConnections int                    `json:"IdentifiedConnections"`
\tPacketInspection      bool                   `json:"PacketInspection"`
\tConntrackAccounting   bool                   `json:"ConntrackAccounting"`
\tGeneratedAt           string                 `json:"GeneratedAt"`
\tVisibilityNote        string                 `json:"VisibilityNote"`
}

type internetServiceUsage struct {
\tKey               string   `json:"Key"`
\tService           string   `json:"Service"`
\tCategory          string   `json:"Category"`
\tUploadBytes       uint64   `json:"UploadBytes"`
\tDownloadBytes     uint64   `json:"DownloadBytes"`
\tTotalBytes        uint64   `json:"TotalBytes"`
\tCurrentBps        float64  `json:"CurrentBps"`
\tActiveConnections int      `json:"ActiveConnections"`
\tHosts             []string `json:"Hosts"`
}'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit("Could not add cumulative service response")

old = '''\tinternetFlowPrevious        = make(map[string]flowCounter)
\tinternetFlowFirstSeen       = make(map[string]time.Time)'''
new = '''\tinternetFlowPrevious        = make(map[string]flowCounter)
\tinternetServiceTotals       = make(map[string]*internetServiceUsage)
\tinternetFlowFirstSeen       = make(map[string]time.Time)'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit("Could not add cumulative service state")

marker = '''func refreshInternetFlows() {'''
helper = '''func internetServiceUsageKey(service, category, host, remoteIP string, remotePort int) string {
\tif host != "" && (category == "Internet service" || service == host) {
\t\treturn "host|" + strings.ToLower(host)
\t}
\tif category == "Unknown" {
\t\treturn fmt.Sprintf("unknown|%s|%s|%d", strings.ToLower(service), remoteIP, remotePort)
\t}
\tif service == "" {
\t\treturn fmt.Sprintf("remote|%s|%d", remoteIP, remotePort)
\t}
\treturn "service|" + strings.ToLower(service)
}

func refreshInternetFlows() {'''
if helper not in s:
    if marker not in s:
        raise SystemExit("Could not add service key helper")
    s = s.replace(marker, helper, 1)

old = '''\t\tservice, category := classifyInternetService(host, ct.orig.dport, ct.proto)
\t\trecentlyKilled := false'''
new = '''\t\tservice, category := classifyInternetService(host, ct.orig.dport, ct.proto)
\t\tserviceKey := internetServiceUsageKey(service, category, host, ct.orig.dst, ct.orig.dport)
\t\tdeltaUpload, deltaDownload := ct.orig.bytes, ct.reply.bytes
\t\tif hadPrevious {
\t\t\tif ct.orig.bytes >= prev.upload {
\t\t\t\tdeltaUpload = ct.orig.bytes - prev.upload
\t\t\t}
\t\t\tif ct.reply.bytes >= prev.download {
\t\t\t\tdeltaDownload = ct.reply.bytes - prev.download
\t\t\t}
\t\t}
\t\tusage := internetServiceTotals[serviceKey]
\t\tif usage == nil {
\t\t\tusage = &internetServiceUsage{Key: serviceKey, Service: service, Category: category}
\t\t\tinternetServiceTotals[serviceKey] = usage
\t\t}
\t\tif service != "" {
\t\t\tusage.Service = service
\t\t}
\t\tif category != "" {
\t\t\tusage.Category = category
\t\t}
\t\tusage.UploadBytes += deltaUpload
\t\tusage.DownloadBytes += deltaDownload
\t\tusage.TotalBytes = usage.UploadBytes + usage.DownloadBytes

\t\trecentlyKilled := false'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit("Could not accumulate per-service usage")

old = '''\tinternetFlowMu.Lock()
\tflows := make([]internetFlow, 0, len(internetFlows))
\tidentified := 0
\tfor _, flow := range internetFlows {
\t\tflows = append(flows, flow)
\t\tif flow.Hostname != "" {
\t\t\tidentified++
\t\t}
\t}
\tpacketInspection := internetPacketInspection
\tconntrackAccounting := internetConntrackAccounting
\tinternetFlowMu.Unlock()

\tsort.Slice(flows, func(i, j int) bool {
\t\tif flows[i].CurrentBps == flows[j].CurrentBps {
\t\t\treturn flows[i].TotalBytes > flows[j].TotalBytes
\t\t}
\t\treturn flows[i].CurrentBps > flows[j].CurrentBps
\t})

\tresp := internetFlowResponse{
\t\tFlows:                 flows,
\t\tActiveConnections:     len(flows),
\t\tIdentifiedConnections: identified,
\t\tPacketInspection:      packetInspection,
\t\tConntrackAccounting:   conntrackAccounting,
\t\tGeneratedAt:           time.Now().UTC().Format(time.RFC3339),
\t\tVisibilityNote:        "HTTPS payloads remain encrypted. Hostnames are learned from DNS, TLS SNI or reverse DNS; exact URL paths are only visible for unencrypted HTTP.",
\t}'''
new = '''\tinternetFlowMu.Lock()
\tflows := make([]internetFlow, 0, len(internetFlows))
\tidentified := 0
\tfor _, flow := range internetFlows {
\t\tflows = append(flows, flow)
\t\tif flow.Hostname != "" {
\t\t\tidentified++
\t\t}
\t}

\tservices := make([]internetServiceUsage, 0, len(internetServiceTotals))
\tserviceIndex := make(map[string]int)
\ttrackedBytes := uint64(0)
\tfor key, total := range internetServiceTotals {
\t\tif total == nil {
\t\t\tcontinue
\t\t}
\t\tcopyUsage := *total
\t\tcopyUsage.CurrentBps = 0
\t\tcopyUsage.ActiveConnections = 0
\t\tcopyUsage.Hosts = nil
\t\tserviceIndex[key] = len(services)
\t\tservices = append(services, copyUsage)
\t\ttrackedBytes += copyUsage.TotalBytes
\t}
\tfor _, flow := range internetFlows {
\t\tkey := internetServiceUsageKey(flow.Service, flow.Category, flow.Hostname, flow.RemoteIP, flow.RemotePort)
\t\tidx, ok := serviceIndex[key]
\t\tif !ok {
\t\t\tcontinue
\t\t}
\t\tservices[idx].CurrentBps += flow.CurrentBps
\t\tservices[idx].ActiveConnections++
\t\thost := flow.Hostname
\t\tif host == "" {
\t\t\thost = flow.RemoteIP
\t\t}
\t\tif host != "" && len(services[idx].Hosts) < 4 {
\t\t\tfound := false
\t\t\tfor _, existing := range services[idx].Hosts {
\t\t\t\tif existing == host {
\t\t\t\t\tfound = true
\t\t\t\t\tbreak
\t\t\t\t}
\t\t\t}
\t\t\tif !found {
\t\t\t\tservices[idx].Hosts = append(services[idx].Hosts, host)
\t\t\t}
\t\t}
\t}

\tpacketInspection := internetPacketInspection
\tconntrackAccounting := internetConntrackAccounting
\tinternetFlowMu.Unlock()

\tsort.Slice(flows, func(i, j int) bool {
\t\tif flows[i].CurrentBps == flows[j].CurrentBps {
\t\t\treturn flows[i].TotalBytes > flows[j].TotalBytes
\t\t}
\t\treturn flows[i].CurrentBps > flows[j].CurrentBps
\t})
\tsort.Slice(services, func(i, j int) bool {
\t\tif services[i].TotalBytes == services[j].TotalBytes {
\t\t\treturn services[i].CurrentBps > services[j].CurrentBps
\t\t}
\t\treturn services[i].TotalBytes > services[j].TotalBytes
\t})

\tresp := internetFlowResponse{
\t\tFlows:                 flows,
\t\tServices:              services,
\t\tTrackedBytes:          trackedBytes,
\t\tActiveConnections:     len(flows),
\t\tIdentifiedConnections: identified,
\t\tPacketInspection:      packetInspection,
\t\tConntrackAccounting:   conntrackAccounting,
\t\tGeneratedAt:           time.Now().UTC().Format(time.RFC3339),
\t\tVisibilityNote:        "HTTPS payloads remain encrypted. Hostnames are learned from DNS, TLS SNI or reverse DNS; exact URL paths are only visible for unencrypted HTTP.",
\t}'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit("Could not expose cumulative service usage")

marker = '''func handleInternetFlowKill(w http.ResponseWriter, r *http.Request) {'''
reset_fn = '''func resetInternetServiceUsage() {
\tinternetFlowMu.Lock()
\tinternetServiceTotals = make(map[string]*internetServiceUsage)
\tinternetFlowMu.Unlock()
}

func handleInternetFlowKill(w http.ResponseWriter, r *http.Request) {'''
if reset_fn not in s:
    if marker not in s:
        raise SystemExit("Could not add service usage reset function")
    s = s.replace(marker, reset_fn, 1)

p.write_text(s)

# Reset the service/session ranking at the same time as the main Internet Data
# session counters, while preserving conntrack baselines so pre-reset bytes are not
# counted again on the next sample.
p2 = root / "main/datausage.go"
s2 = p2.read_text()
old = '''\tdataUsageSessionLimitReached = false
\tdataUsageMu.Unlock()
\tappendDataUsageLog(dataUsageLogRecord{Time: time.Now().UTC().Format(time.RFC3339), Type: "session_reset", Message: "Data usage session counters manually reset"})'''
new = '''\tdataUsageSessionLimitReached = false
\tdataUsageMu.Unlock()
\tresetInternetServiceUsage()
\tappendDataUsageLog(dataUsageLogRecord{Time: time.Now().UTC().Format(time.RFC3339), Type: "session_reset", Message: "Data usage session counters manually reset"})'''
if old in s2:
    s2 = s2.replace(old, new, 1)
elif new not in s2:
    raise SystemExit("Could not wire service usage reset into session reset")
p2.write_text(s2)
