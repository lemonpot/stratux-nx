#!/usr/bin/env python3
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

# ---------------------------------------------------------------------------
# Backend: persistent per-device exemption from automatic Internet limits.
# Usage is still counted in the session total, but exempt devices are never
# automatically warned/blocked and remain online when the global limit trips.
# ---------------------------------------------------------------------------
p = root / "main/datausage.go"
s = p.read_text()

old = '''\tWarningReached bool    `json:"warningReached"`\n}'''
new = '''\tWarningReached bool    `json:"warningReached"`\n\tExempt         bool    `json:"exempt"`\n}'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not add Exempt to dataUsageClient')

old = '''\tdataUsageConfigPath string\n\tdataUsageInitialized bool'''
new = '''\tdataUsageConfigPath string\n\tdataUsagePolicyPath string\n\tdataUsageExemptKeys = make(map[string]bool)\n\tdataUsageManualBlocked = make(map[string]bool)\n\tdataUsageInitialized bool'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not add data usage device policy state')

old = '''\thttp.HandleFunc("/dataUsage/block", handleDataUsageBlock)\n\thttp.HandleFunc("/dataUsage/settings", handleDataUsageSettings)'''
new = '''\thttp.HandleFunc("/dataUsage/block", handleDataUsageBlock)\n\thttp.HandleFunc("/dataUsage/exempt", handleDataUsageExempt)\n\thttp.HandleFunc("/dataUsage/settings", handleDataUsageSettings)'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not register data usage exemption endpoint')

old = '''\tdataUsageConfigPath = filepath.Join(dataUsageStorageDir, "data-usage-settings.json")\n\tloadDataUsageSettingsLocked()'''
new = '''\tdataUsageConfigPath = filepath.Join(dataUsageStorageDir, "data-usage-settings.json")\n\tdataUsagePolicyPath = filepath.Join(dataUsageStorageDir, "data-usage-device-policies.json")\n\tloadDataUsageSettingsLocked()\n\tloadDataUsagePoliciesLocked()'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not initialize data usage device policy path')

marker = '''func saveDataUsageSettingsLocked() {\n\tb, _ := json.MarshalIndent(dataUsageConfig, "", "  ")\n\t_ = os.WriteFile(dataUsageConfigPath, b, 0644)\n}\n'''
helper = marker + '''\nfunc dataUsagePolicyKey(ip, mac string) string {\n\tmac = strings.ToLower(strings.TrimSpace(mac))\n\tif mac != "" && mac != "00:00:00:00:00:00" {\n\t\treturn "mac:" + mac\n\t}\n\treturn "ip:" + strings.TrimSpace(ip)\n}\n\nfunc loadDataUsagePoliciesLocked() {\n\tb, err := os.ReadFile(dataUsagePolicyPath)\n\tif err != nil {\n\t\treturn\n\t}\n\tvar policies map[string]bool\n\tif json.Unmarshal(b, &policies) == nil && policies != nil {\n\t\tdataUsageExemptKeys = policies\n\t}\n}\n\nfunc saveDataUsagePoliciesLocked() {\n\tb, _ := json.MarshalIndent(dataUsageExemptKeys, "", "  ")\n\t_ = os.WriteFile(dataUsagePolicyPath, b, 0644)\n}\n'''
if 'func dataUsagePolicyKey(' not in s:
    if marker not in s:
        raise SystemExit('Could not add data usage device policy helpers')
    s = s.replace(marker, helper, 1)

old = '''\t\tc.Connected = true\n\t\tc.LastSeen = now.UTC().Format(time.RFC3339)\n\t\tc.Blocked = blocked[ip]\n\t\tc.UploadBytes = upload[ip]'''
new = '''\t\tc.Connected = true\n\t\tc.LastSeen = now.UTC().Format(time.RFC3339)\n\t\tpolicyKey := dataUsagePolicyKey(ip, c.MAC)\n\t\tc.Exempt = dataUsageExemptKeys[policyKey] || dataUsageExemptKeys[dataUsagePolicyKey(ip, "")]\n\t\tc.Blocked = blocked[ip]\n\t\tc.UploadBytes = upload[ip]'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not apply device exemption during refresh')

old = '''\t\tshouldWarn := c != nil && warningBytes > 0 && c.TotalBytes >= warningBytes && !dataUsageWarned[ip]'''
new = '''\t\tshouldWarn := c != nil && !c.Exempt && warningBytes > 0 && c.TotalBytes >= warningBytes && !dataUsageWarned[ip]'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not exempt devices from warning threshold')

old = '''\t\tshouldBlock := c != nil && cfg.AutoBlockEnabled && autoBlockBytes > 0 && c.TotalBytes >= autoBlockBytes && !c.Blocked'''
new = '''\t\tshouldBlock := c != nil && !c.Exempt && cfg.AutoBlockEnabled && autoBlockBytes > 0 && c.TotalBytes >= autoBlockBytes && !c.Blocked'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not exempt devices from auto block')

old = '''\t\tfor ip := range discovered {\n\t\t\t_ = setClientBlocked(ip, true)\n\t\t}\n\t\tif firstHit {\n\t\t\tappendDataUsageLog(dataUsageLogRecord{Time: now.UTC().Format(time.RFC3339), Type: "session_limit", Message: fmt.Sprintf("Session hard limit of %d MB reached; all client Internet access blocked", cfg.SessionLimitMB), Bytes: total})\n\t\t}'''
new = '''\t\tfor ip := range discovered {\n\t\t\tdataUsageMu.Lock()\n\t\t\tc := dataUsageClients[ip]\n\t\t\texempt := c != nil && c.Exempt\n\t\t\tdataUsageMu.Unlock()\n\t\t\tif !exempt {\n\t\t\t\t_ = setClientBlocked(ip, true)\n\t\t\t}\n\t\t}\n\t\tif firstHit {\n\t\t\tappendDataUsageLog(dataUsageLogRecord{Time: now.UTC().Format(time.RFC3339), Type: "session_limit", Message: fmt.Sprintf("Session hard limit of %d MB reached; protected clients were blocked while unrestricted devices stayed online", cfg.SessionLimitMB), Bytes: total})\n\t\t}'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not exempt devices from global session block')

old = '''\tif err := setClientBlocked(req.IP, req.Blocked); err != nil {\n\t\thttp.Error(w, err.Error(), http.StatusInternalServerError)\n\t\treturn\n\t}\n\tdataUsageMu.Lock()\n\tc := dataUsageClients[req.IP]'''
new = '''\tif err := setClientBlocked(req.IP, req.Blocked); err != nil {\n\t\thttp.Error(w, err.Error(), http.StatusInternalServerError)\n\t\treturn\n\t}\n\tdataUsageMu.Lock()\n\tdataUsageManualBlocked[req.IP] = req.Blocked\n\tc := dataUsageClients[req.IP]'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not track manual Internet blocks')

marker = '''func handleDataUsageSettings(w http.ResponseWriter, r *http.Request) {'''
handler = '''func handleDataUsageExempt(w http.ResponseWriter, r *http.Request) {\n\tif r.Method != http.MethodPost {\n\t\thttp.Error(w, "POST required", http.StatusMethodNotAllowed)\n\t\treturn\n\t}\n\tvar req struct {\n\t\tIP     string `json:"ip"`\n\t\tMAC    string `json:"mac"`\n\t\tExempt bool   `json:"exempt"`\n\t}\n\tif json.NewDecoder(r.Body).Decode(&req) != nil || net.ParseIP(req.IP) == nil {\n\t\thttp.Error(w, "invalid request", http.StatusBadRequest)\n\t\treturn\n\t}\n\n\tdataUsageMu.Lock()\n\tc := dataUsageClients[req.IP]\n\tmac := strings.TrimSpace(req.MAC)\n\thost, bytes := "", uint64(0)\n\tif c != nil {\n\t\tif mac == "" {\n\t\t\tmac = c.MAC\n\t\t}\n\t\thost, bytes = c.Hostname, c.TotalBytes\n\t}\n\tkey := dataUsagePolicyKey(req.IP, mac)\n\tif req.Exempt {\n\t\tdataUsageExemptKeys[key] = true\n\t} else {\n\t\tdelete(dataUsageExemptKeys, key)\n\t\tdelete(dataUsageExemptKeys, dataUsagePolicyKey(req.IP, ""))\n\t}\n\tif c != nil {\n\t\tc.Exempt = req.Exempt\n\t\tif req.Exempt {\n\t\t\tc.WarningReached = false\n\t\t\tdelete(dataUsageWarned, req.IP)\n\t\t}\n\t}\n\tif req.Exempt {\n\t\tdelete(dataUsageManualBlocked, req.IP)\n\t}\n\tsaveDataUsagePoliciesLocked()\n\tdataUsageMu.Unlock()\n\n\tif req.Exempt {\n\t\t_ = setClientBlocked(req.IP, false)\n\t}\n\taction, message := "device_protected", "Automatic Internet limits enabled for device"\n\tif req.Exempt {\n\t\taction, message = "device_exempt", "Device marked unrestricted; automatic limits will not block it"\n\t}\n\tappendDataUsageLog(dataUsageLogRecord{Time: time.Now().UTC().Format(time.RFC3339), Type: action, IP: req.IP, MAC: mac, Hostname: host, Message: message, Bytes: bytes})\n\tw.Header().Set("Content-Type", "application/json")\n\t_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})\n}\n\nfunc handleDataUsageSettings(w http.ResponseWriter, r *http.Request) {'''
if 'func handleDataUsageExempt(' not in s:
    if marker not in s:
        raise SystemExit('Could not add exemption HTTP handler')
    s = s.replace(marker, handler, 1)

# Session reset should remove automatic blocks so the new session really starts
# clean, while preserving devices the user explicitly blocked manually.
old = '''\tdataUsageMu.Lock()\n\tdataUsagePrevious = make(map[string]dataUsageCounter)\n\tdataUsageWarned = make(map[string]bool)\n\tfor _, c := range dataUsageClients {'''
new = '''\tdataUsageMu.Lock()\n\tdataUsagePrevious = make(map[string]dataUsageCounter)\n\tdataUsageWarned = make(map[string]bool)\n\tknownIPs := make([]string, 0, len(dataUsageClients))\n\tfor ip := range dataUsageClients {\n\t\tknownIPs = append(knownIPs, ip)\n\t}\n\tfor _, c := range dataUsageClients {'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not collect clients during session reset')

old = '''\tdataUsageSessionLimitReached = false\n\tdataUsageMu.Unlock()\n\tresetInternetServiceUsage()\n\tappendDataUsageLog(dataUsageLogRecord{Time: time.Now().UTC().Format(time.RFC3339), Type: "session_reset", Message: "Data usage session counters manually reset"})'''
new = '''\tdataUsageSessionLimitReached = false\n\tmanualBlocks := make(map[string]bool, len(dataUsageManualBlocked))\n\tfor ip, blocked := range dataUsageManualBlocked {\n\t\tmanualBlocks[ip] = blocked\n\t}\n\tdataUsageMu.Unlock()\n\tfor _, ip := range knownIPs {\n\t\tif !manualBlocks[ip] {\n\t\t\t_ = setClientBlocked(ip, false)\n\t\t}\n\t}\n\tresetInternetServiceUsage()\n\tappendDataUsageLog(dataUsageLogRecord{Time: time.Now().UTC().Format(time.RFC3339), Type: "session_reset", Message: "Data usage session reset to zero; automatic blocks were cleared"})'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not make reset clear automatic blocks')

p.write_text(s)

# ---------------------------------------------------------------------------
# Frontend controller: exemption action, prominent reset feedback and log icons.
# ---------------------------------------------------------------------------
p = root / "web/plates/js/datausage.js"
s = p.read_text()

old = '''            case 'settings': return 'fa-shield';\n            case 'session_reset':'''
new = '''            case 'settings': return 'fa-shield';\n            case 'device_exempt': return 'fa-star';\n            case 'device_protected': return 'fa-shield';\n            case 'session_reset':'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not add exemption log icon')

old = '''            case 'manual_unblock': return 'success';\n            default: return '';'''
new = '''            case 'manual_unblock':\n            case 'device_exempt':\n            case 'device_protected': return 'success';\n            default: return '';'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not add exemption log class')

marker = '''    $scope.saveSettings = function() {'''
helper = '''    $scope.toggleExempt = function(client) {\n        if (!client || client.policyPending) return;\n        client.policyPending = true;\n        $http.post('/dataUsage/exempt', {\n            ip: client.IP,\n            mac: client.MAC || '',\n            exempt: !client.Exempt\n        }).then(function() {\n            client.policyPending = false;\n            if (window.StratuxUI) {\n                window.StratuxUI.toast(client.Exempt ? 'Automatic limits restored for this device' : 'Device is now unrestricted', 'success', 2200);\n            }\n            $scope.refresh();\n        }, function(response) {\n            client.policyPending = false;\n            $scope.errorMessage = 'Could not change device policy: ' + (response.data || 'unknown error');\n        });\n    };\n\n    $scope.nearSessionLimit = function() {\n        return $scope.sessionPercent() >= 75;\n    };\n\n    $scope.saveSettings = function() {'''
if '$scope.toggleExempt' not in s:
    if marker not in s:
        raise SystemExit('Could not add device exemption controller action')
    s = s.replace(marker, helper, 1)

old = '''        if (!window.confirm('Start a new data session? Current counters will reset to zero. The historical log will NOT be deleted.')) {\n            return;\n        }\n        $http.post('/dataUsage/reset', {}).then(function() {\n            $scope.refresh();'''
new = '''        if (!window.confirm('Start a new data session at 0 MB? Historical logs and unrestricted-device preferences will be kept. Automatic blocks will be cleared.')) {\n            return;\n        }\n        $http.post('/dataUsage/reset', {}).then(function() {\n            if (window.StratuxUI) window.StratuxUI.toast('New Internet data session started at 0 MB', 'success', 2200);\n            $scope.refresh();'''
if old in s:
    s = s.replace(old, new, 1)
elif 'New Internet data session started at 0 MB' not in s:
    raise SystemExit('Could not improve session reset feedback')

p.write_text(s)

# ---------------------------------------------------------------------------
# Internet Data HTML: reset beside the hard limit and a persistent unrestricted
# policy button directly on each device card.
# ---------------------------------------------------------------------------
p = root / "web/plates/datausage.html"
s = p.read_text()

old = '''      <label class="data-switch-row">\n        <span>Automatic protection</span>\n        <input type="checkbox" ng-model="settings.AutoBlockEnabled">\n      </label>'''
new = '''      <div class="data-safety-actions">\n        <button class="btn data-session-reset" ng-class="{'is-urgent': nearSessionLimit()}" ng-click="resetSession()">\n          <i class="fa fa-refresh"></i> {{nearSessionLimit() ? 'Reset session now' : 'Reset session'}}\n        </button>\n        <label class="data-switch-row">\n          <span>Automatic protection</span>\n          <input type="checkbox" ng-model="settings.AutoBlockEnabled">\n        </label>\n      </div>'''
if old in s:
    s = s.replace(old, new, 1)
elif 'data-session-reset' not in s:
    raise SystemExit('Could not add prominent session reset button')

old = '''              <span ng-if="client.MAC">{{client.MAC}}</span>\n            </div>\n          </div>\n          <button class="btn data-block-btn" ng-class="client.Blocked ? 'btn-success' : 'btn-danger'" ng-click="toggleBlock(client)" ng-disabled="client.actionPending">\n            <i class="fa" ng-class="client.Blocked ? 'fa-unlock' : 'fa-ban'"></i>\n            {{client.actionPending ? 'Working...' : (client.Blocked ? 'RESTORE INTERNET' : 'BLOCK INTERNET')}}\n          </button>'''
new = '''              <span ng-if="client.MAC">{{client.MAC}}</span>\n              <span class="data-status-badge unrestricted" ng-if="client.Exempt"><i class="fa fa-star"></i> UNRESTRICTED</span>\n            </div>\n          </div>\n          <div class="data-device-actions">\n            <button class="btn data-policy-btn" ng-class="client.Exempt ? 'is-exempt' : ''" ng-click="toggleExempt(client)" ng-disabled="client.policyPending">\n              <i class="fa" ng-class="client.Exempt ? 'fa-shield' : 'fa-star-o'"></i>\n              {{client.policyPending ? 'Saving...' : (client.Exempt ? 'USE LIMITS' : 'IGNORE LIMITS')}}\n            </button>\n            <button class="btn data-block-btn" ng-class="client.Blocked ? 'btn-success' : 'btn-danger'" ng-click="toggleBlock(client)" ng-disabled="client.actionPending">\n              <i class="fa" ng-class="client.Blocked ? 'fa-unlock' : 'fa-ban'"></i>\n              {{client.actionPending ? 'Working...' : (client.Blocked ? 'RESTORE INTERNET' : 'BLOCK INTERNET')}}\n            </button>\n          </div>'''
if old in s:
    s = s.replace(old, new, 1)
elif 'data-policy-btn' not in s:
    raise SystemExit('Could not add unrestricted-device action')

old = '''            <span ng-if="!client.Blocked && client.TotalBytes < mb(settings.WarningMB)">Normal usage</span>\n            <span ng-if="!client.Blocked && client.TotalBytes >= mb(settings.WarningMB) && client.TotalBytes < mb(settings.AutoBlockMB)">Warning threshold reached</span>\n            <span ng-if="client.Blocked">Internet blocked</span>'''
new = '''            <span ng-if="client.Exempt">Unrestricted — usage is still counted in the session total</span>\n            <span ng-if="!client.Exempt && !client.Blocked && client.TotalBytes < mb(settings.WarningMB)">Normal usage</span>\n            <span ng-if="!client.Exempt && !client.Blocked && client.TotalBytes >= mb(settings.WarningMB) && client.TotalBytes < mb(settings.AutoBlockMB)">Warning threshold reached</span>\n            <span ng-if="!client.Exempt && client.Blocked">Internet blocked</span>'''
if old in s:
    s = s.replace(old, new, 1)
elif 'Unrestricted — usage is still counted' not in s:
    raise SystemExit('Could not add unrestricted device status copy')

p.write_text(s)
