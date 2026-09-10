#!/usr/bin/env python3
import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1])

# ---------------------------------------------------------------------------
# index.html: load the global modern UI after the legacy/theme CSS so it can
# consistently modernize all existing screens without rewriting every plate.
# ---------------------------------------------------------------------------
p = root / "web/index.html"
s = p.read_text()
if 'css/modern-ui.css' not in s:
    marker = '<link rel="stylesheet" id="themeStylesheet" href="" />'
    if marker not in s:
        raise SystemExit('Could not find themeStylesheet marker in web/index.html')
    s = s.replace(marker, marker + '\n\t<link rel="stylesheet" href="css/modern-ui.css" />', 1)
if 'js/modern-ui.js' not in s:
    marker = '<script src="plates/js/developer.js"></script>'
    if marker not in s:
        raise SystemExit('Could not find developer.js marker in web/index.html')
    s = s.replace(marker, marker + '\n\t<script src="js/modern-ui.js"></script>', 1)
p.write_text(s)

# ---------------------------------------------------------------------------
# AppCache: make the new shared UX assets available in standalone/iPad mode.
# ---------------------------------------------------------------------------
p = root / "web/stratux.appcache"
s = p.read_text()
entries = ['/css/modern-ui.css', '/js/modern-ui.js']
if any(e not in s for e in entries):
    marker = '\nNETWORK:\n'
    if marker not in s:
        raise SystemExit('Could not find NETWORK section in stratux.appcache')
    missing = ''.join(e + '\n' for e in entries if e not in s)
    s = s.replace(marker, '\n' + missing + 'NETWORK:\n', 1)
p.write_text(s)

# ---------------------------------------------------------------------------
# Settings controller UX audit.
# 1) Ordinary settings changes now return a promise and show unobtrusive
#    success/error feedback instead of silently failing.
# 2) WiFi settings no longer claim success before the POST finishes. The modal
#    opens in a real saving state, then shows a 30-second restart countdown.
# 3) Reboot/shutdown display an explicit blocking progress state.
# ---------------------------------------------------------------------------
p = root / "web/plates/js/settings.js"
s = p.read_text()

set_settings_pattern = re.compile(
    r'\tfunction setSettings\(msg\) \{.*?\n\t\}\n\n\tgetSettings\(\);',
    re.S,
)
set_settings_replacement = r'''\tfunction setSettings(msg, options) {
\t\toptions = options || {};
\t\treturn $http.post(URL_SETTINGS_SET, msg).then(function (response) {
\t\t\tloadSettings(response.data);
\t\t\tif (!options.silent && window.StratuxUI) {
\t\t\t\twindow.StratuxUI.toast(options.successMessage || "Setting saved", "success", 1800);
\t\t\t}
\t\t\treturn response;
\t\t}, function (response) {
\t\t\t$scope.rawSettings = "error setting settings";
\t\t\tif (!options.silent && window.StratuxUI) {
\t\t\t\twindow.StratuxUI.toast(options.errorMessage || "Stratux could not save this setting", "error", 4200);
\t\t\t}
\t\t\treturn Promise.reject(response);
\t\t});
\t}

\tgetSettings();'''
if not set_settings_pattern.search(s):
    if 'function setSettings(msg, options)' not in s:
        raise SystemExit('Could not patch setSettings in settings.js')
else:
    s = set_settings_pattern.sub(set_settings_replacement, s, count=1)

wifi_pattern = re.compile(
    r'\t\$scope\.updateWiFi = function\(action\) \{.*?\n\t\};\n\n\t\$scope\.wifiModeStr',
    re.S,
)
wifi_replacement = r'''\tvar wifiApplyTimer = null;
\t$scope.wifiApplying = false;
\t$scope.wifiApplyState = "idle";
\t$scope.wifiApplySeconds = 30;
\t$scope.wifiApplyError = "";

\tfunction stopWiFiApplyTimer() {
\t\tif (wifiApplyTimer !== null) {
\t\t\t$window.clearInterval(wifiApplyTimer);
\t\t\twifiApplyTimer = null;
\t\t}
\t}

\tfunction startWiFiRestartCountdown() {
\t\tstopWiFiApplyTimer();
\t\t$scope.wifiApplySeconds = 30;
\t\twifiApplyTimer = $window.setInterval(function () {
\t\t\t$scope.wifiApplySeconds = Math.max(0, $scope.wifiApplySeconds - 1);
\t\t\tif ($scope.wifiApplySeconds <= 0) {
\t\t\t\tstopWiFiApplyTimer();
\t\t\t\t$scope.wifiApplying = false;
\t\t\t\t$scope.wifiApplyState = "done";
\t\t\t}
\t\t\t$scope.$applyAsync();
\t\t}, 1000);
\t}

\t$scope.wifiApplyProgress = function () {
\t\tif ($scope.wifiApplyState === "saving") return 8;
\t\tif ($scope.wifiApplyState === "done") return 100;
\t\tif ($scope.wifiApplyState === "error") return 100;
\t\treturn Math.max(12, Math.min(98, ((30 - Number($scope.wifiApplySeconds || 0)) / 30) * 100));
\t};

\t$scope.wifiReload = function () {
\t\t$window.location.reload();
\t};

\t$scope.updateWiFi = function(action) {
\t\tif ($scope.wifiApplying) return;
\t\t$scope.WiFiErrors = {
\t\t\t'WiFiSSID': '',
\t\t\t'WiFiPassphrase': '',
\t\t\t'Errors': false
\t\t};

\t\tif (($scope.WiFiSSID === undefined) || ($scope.WiFiSSID === null) || !isValidSSID($scope.WiFiSSID)) {
\t\t\t$scope.WiFiErrors.WiFiSSID = "Your Network Name (SSID) must be 1-32 characters and use supported characters.";
\t\t\t$scope.WiFiErrors.Errors = true;
\t\t}

\t\tif ($scope.WiFiSecurityEnabled) {
\t\t\tif (!$scope.WiFiPassphrase || !isValidWPA($scope.WiFiPassphrase)) {
\t\t\t\t$scope.WiFiErrors.WiFiPassphrase = "Your WiFi password must use standard printable characters.";
\t\t\t\t$scope.WiFiErrors.Errors = true;
\t\t\t}
\t\t\tif (!$scope.WiFiPassphrase || $scope.WiFiPassphrase.length < 8 || $scope.WiFiPassphrase.length > 63) {
\t\t\t\t$scope.WiFiErrors.WiFiPassphrase = "Your WiFi password must be between 8 and 63 characters long.";
\t\t\t\t$scope.WiFiErrors.Errors = true;
\t\t\t}
\t\t}

\t\tif ($scope.WiFiErrors.Errors) {
\t\t\t$scope.Ui.turnOn("modalErrorWiFi");
\t\t\treturn;
\t\t}

\t\tvar newsettings = {
\t\t\t"WiFiCountry": $scope.WiFiCountry || "",
\t\t\t"WiFiSSID": $scope.WiFiSSID,
\t\t\t"WiFiSecurityEnabled": $scope.WiFiSecurityEnabled,
\t\t\t"WiFiPassphrase": $scope.WiFiPassphrase,
\t\t\t"WiFiChannel": parseInt($scope.WiFiChannel),
\t\t\t"WiFiIPAddress": $scope.WiFiIPAddress,
\t\t\t"WiFiMode": parseInt($scope.WiFiMode),
\t\t\t"WiFiDirectPin": $scope.WiFiDirectPin,
\t\t\t"WiFiClientNetworks": $scope.WiFiClientNetworks,
\t\t\t"WiFiInternetPassThroughEnabled": $scope.WiFiInternetPassThroughEnabled
\t\t};

\t\tstopWiFiApplyTimer();
\t\t$scope.wifiApplying = true;
\t\t$scope.wifiApplyState = "saving";
\t\t$scope.wifiApplySeconds = 30;
\t\t$scope.wifiApplyError = "";
\t\t$scope.Ui.turnOn("modalSuccessWiFi");

\t\t$http.post(URL_SETTINGS_SET, angular.toJson(newsettings)).then(function (response) {
\t\t\tloadSettings(response.data);
\t\t\t$scope.wifiApplyState = "restarting";
\t\t\tstartWiFiRestartCountdown();
\t\t}, function (response) {
\t\t\t$scope.wifiApplying = false;
\t\t\t$scope.wifiApplyState = "error";
\t\t\t$scope.wifiApplyError = (response && response.data) ? String(response.data) : "Stratux did not confirm the WiFi settings change.";
\t\t});
\t};

\t$scope.$on('$destroy', function () {
\t\tstopWiFiApplyTimer();
\t});

\t$scope.wifiModeStr'''
if not wifi_pattern.search(s):
    if 'wifiApplyState = "idle"' not in s:
        raise SystemExit('Could not patch updateWiFi in settings.js')
else:
    s = wifi_pattern.sub(wifi_replacement, s, count=1)

reboot_pattern = re.compile(
    r'\t\$scope\.postReboot = function \(\) \{.*?\n\t\};',
    re.S,
)
reboot_replacement = r'''\t$scope.postReboot = function () {
\t\tif (window.StratuxUI) {
\t\t\twindow.StratuxUI.showBusy("Rebooting Stratux", "The receiver will disappear for a short time. Reconnect to Stratux WiFi when it comes back.");
\t\t}
\t\t$http.post(URL_REBOOT).finally(function () {
\t\t\t$window.setTimeout(function () { $window.location.href = "/"; }, 1400);
\t\t});
\t};'''
if reboot_pattern.search(s):
    s = reboot_pattern.sub(reboot_replacement, s, count=1)
elif 'window.StratuxUI.showBusy("Rebooting Stratux"' not in s:
    raise SystemExit('Could not patch postReboot in settings.js')

shutdown_pattern = re.compile(
    r'\t\$scope\.postShutdown = function \(\) \{.*?\n\t\};',
    re.S,
)
shutdown_replacement = r'''\t$scope.postShutdown = function () {
\t\tif (window.StratuxUI) {
\t\t\twindow.StratuxUI.showBusy("Shutting down Stratux", "Wait for the shutdown to complete before removing power.");
\t\t}
\t\t$http.post(URL_SHUTDOWN).finally(function () {
\t\t\t$window.setTimeout(function () { $window.location.href = "/"; }, 1400);
\t\t});
\t};'''
if shutdown_pattern.search(s):
    s = shutdown_pattern.sub(shutdown_replacement, s, count=1)
elif 'window.StratuxUI.showBusy("Shutting down Stratux"' not in s:
    raise SystemExit('Could not patch postShutdown in settings.js')

p.write_text(s)

# ---------------------------------------------------------------------------
# Settings HTML: clearer WiFi action and a stateful progress modal. Never echo
# the WiFi password back in the modal.
# ---------------------------------------------------------------------------
p = root / "web/plates/settings.html"
s = p.read_text()

old_button = '''<button class="btn btn-primary btn-block" ng-click="updateWiFi()">Submit WiFi
                                Changes</button>'''
new_button = '''<button class="btn btn-primary btn-block" ng-click="updateWiFi()" ng-disabled="wifiApplying">
                                <i class="fa" ng-class="wifiApplying ? 'fa-circle-o-notch fa-spin' : 'fa-wifi'"></i>
                                {{wifiApplying ? 'Applying WiFi changes...' : 'Apply WiFi changes'}}
                            </button>'''
if old_button in s:
    s = s.replace(old_button, new_button, 1)
elif "Applying WiFi changes..." not in s:
    raise SystemExit('Could not patch WiFi submit button in settings.html')

wifi_modal_pattern = re.compile(
    r'    <!-- WiFi Success Modal -->.*?    <!-- WiFi Error Modal -->',
    re.S,
)
wifi_modal_replacement = '''    <!-- WiFi Apply / Restart Modal -->
    <div class="modal" ui-if="modalSuccessWiFi" ui-state="modalSuccessWiFi" id="WiFiSuccessModal">
        <div class="modal-overlay"></div>
        <div class="vertical-alignment-helper center-block">
            <div class="modal-dialog vertical-align-center">
                <div class="modal-content">
                    <div class="modal-header">
                        <h4 class="modal-title" ng-if="wifiApplyState=='saving'">Applying WiFi changes</h4>
                        <h4 class="modal-title" ng-if="wifiApplyState=='restarting'">WiFi settings saved — restarting network</h4>
                        <h4 class="modal-title" ng-if="wifiApplyState=='done'">WiFi settings applied</h4>
                        <h4 class="modal-title" ng-if="wifiApplyState=='error'">WiFi change could not be confirmed</h4>
                    </div>
                    <div class="modal-body">
                        <div class="sx-wifi-status">
                            <div class="sx-wifi-status-icon" ng-if="wifiApplyState=='saving'"><i class="fa fa-circle-o-notch fa-spin"></i></div>
                            <div class="sx-wifi-status-icon success" ng-if="wifiApplyState=='restarting' || wifiApplyState=='done'"><i class="fa fa-check"></i></div>
                            <div class="sx-wifi-status-icon error" ng-if="wifiApplyState=='error'"><i class="fa fa-exclamation-triangle"></i></div>
                            <div class="sx-wifi-status-copy">
                                <h4 ng-if="wifiApplyState=='saving'">Saving your configuration...</h4>
                                <p ng-if="wifiApplyState=='saving'">Stratux is writing the new WiFi configuration. Do not close this window yet.</p>
                                <h4 ng-if="wifiApplyState=='restarting'">The configuration was accepted.</h4>
                                <p ng-if="wifiApplyState=='restarting'">Network services are restarting. Your browser may temporarily lose connection. About {{wifiApplySeconds}} seconds remaining.</p>
                                <h4 ng-if="wifiApplyState=='done'">Restart window complete.</h4>
                                <p ng-if="wifiApplyState=='done'">If the SSID changed, reconnect your device to the new Stratux WiFi network. Otherwise you can continue normally.</p>
                                <h4 ng-if="wifiApplyState=='error'">Stratux did not return a successful save response.</h4>
                                <p ng-if="wifiApplyState=='error'">{{wifiApplyError}}</p>
                            </div>
                        </div>

                        <div class="sx-wifi-progress" ng-if="wifiApplyState!='error'">
                            <div ng-style="{'width': wifiApplyProgress() + '%'}"></div>
                        </div>

                        <ul class="sx-wifi-steps" ng-if="wifiApplyState!='error'">
                            <li><i class="fa" ng-class="wifiApplyState=='saving' ? 'fa-circle-o-notch fa-spin' : 'fa-check-circle'"></i><span>Save WiFi configuration</span></li>
                            <li><i class="fa" ng-class="wifiApplyState=='restarting' ? 'fa-circle-o-notch fa-spin' : (wifiApplyState=='done' ? 'fa-check-circle' : 'fa-circle-o')"></i><span>Restart WiFi services</span></li>
                            <li><i class="fa" ng-class="wifiApplyState=='done' ? 'fa-check-circle' : 'fa-circle-o'"></i><span>Reconnect if the network name or mode changed</span></li>
                        </ul>

                        <div class="sx-wifi-summary" ng-if="wifiApplyState!='error'">
                            <div><span>Mode</span><strong>{{wifiModeStr()}}</strong></div>
                            <div><span>Stratux SSID</span><strong>{{WiFiSSID}}</strong></div>
                            <div><span>Security</span><strong>{{WiFiSecurityEnabled ? 'Password protected' : 'Open network'}}</strong></div>
                            <div><span>Stratux IP</span><strong>{{WiFiIPAddress}}</strong></div>
                        </div>
                    </div>
                    <div class="modal-footer">
                        <a ui-turn-off="modalSuccessWiFi" class="btn btn-default" ng-if="wifiApplyState=='restarting'">Run in background</a>
                        <a ui-turn-off="modalSuccessWiFi" class="btn btn-default" ng-if="wifiApplyState=='error'">Close</a>
                        <a ui-turn-off="modalSuccessWiFi" class="btn btn-primary" ng-if="wifiApplyState=='done'" ng-click="wifiReload()"><i class="fa fa-refresh"></i> Continue</a>
                    </div>
                </div>
            </div>
        </div>
    </div>
    <!-- WiFi Error Modal -->'''
if wifi_modal_pattern.search(s):
    s = wifi_modal_pattern.sub(wifi_modal_replacement, s, count=1)
elif 'WiFi settings saved — restarting network' not in s:
    raise SystemExit('Could not patch WiFi progress modal in settings.html')

p.write_text(s)
