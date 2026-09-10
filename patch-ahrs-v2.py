#!/usr/bin/env python3
import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1])

# ---------------------------------------------------------------------------
# Backend setting: keep Legacy as the compatibility/default mode. Existing
# stratux.conf files simply deserialize AHRSV2_Enabled as false until selected.
# ---------------------------------------------------------------------------
p = root / "main/gen_gdl90.go"
s = p.read_text()
old = "\tBMP_Sensor_Enabled   bool\n\tIMU_Sensor_Enabled   bool\n\tNetworkOutputs"
new = "\tBMP_Sensor_Enabled   bool\n\tIMU_Sensor_Enabled   bool\n\tAHRSV2_Enabled       bool // Adaptive AHRS v2 beta; false keeps upstream SimpleAHRS\n\tNetworkOutputs"
if old in s:
    s = s.replace(old, new, 1)
elif "AHRSV2_Enabled" not in s:
    raise SystemExit("Could not add AHRSV2_Enabled to settings struct")
p.write_text(s)

# Settings POST handler.
p = root / "main/managementinterface.go"
s = p.read_text()
marker = '\t\t\t\t\tcase "BMP_Sensor_Enabled":'
addition = '\t\t\t\t\tcase "AHRSV2_Enabled":\n\t\t\t\t\t\tglobalSettings.AHRSV2_Enabled = val.(bool)\n'
if 'case "AHRSV2_Enabled":' not in s:
    if marker not in s:
        raise SystemExit("Could not find BMP settings case for AHRS v2 insertion")
    s = s.replace(marker, addition + marker, 1)
p.write_text(s)

# Situation API fields so the UI can show which estimator is actually published
# and the v2 confidence score.
p = root / "main/gps.go"
s = p.read_text()
old = "\tAHRSStatus           uint8\n}"
new = "\tAHRSStatus           uint8\n\tAHRSAlgorithm        string  // Selected estimator currently published to clients\n\tAHRSConfidence       float64 // 0-100; meaningful for Adaptive AHRS v2\n\tAHRSStationary       bool    // v2 stationary detector state\n}"
if old in s:
    s = s.replace(old, new, 1)
elif "AHRSAlgorithm" not in s:
    raise SystemExit("Could not add AHRS diagnostics to SituationData")
p.write_text(s)

# Replace only the estimator constructor. selectableAHRS runs Legacy and v2 in
# parallel and presents the selected implementation through the same interface,
# so the existing calibration, web, GDL90 and ForeFlight paths stay intact.
p = root / "main/sensors.go"
s = p.read_text()
old = "\ts := ahrs.NewSimpleAHRS()"
new = "\ts := newSelectableAHRS()"
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit("Could not switch sensorAttitudeSender to selectable AHRS")

old = "\t\t\tmySituation.muAttitude.Unlock()\n\n\t\t\tmakeAHRSGDL90Report()"
new = "\t\t\tmySituation.AHRSAlgorithm = s.AlgorithmName()\n\t\t\tmySituation.AHRSConfidence = s.Confidence() * 100\n\t\t\tmySituation.AHRSStationary = s.Stationary()\n\t\t\tmySituation.muAttitude.Unlock()\n\n\t\t\tmakeAHRSGDL90Report()"
if old in s:
    s = s.replace(old, new, 1)
elif "mySituation.AHRSAlgorithm = s.AlgorithmName()" not in s:
    raise SystemExit("Could not publish AHRS algorithm diagnostics")
p.write_text(s)

# Preserve V2 across temporary invalidity; Reset is reserved for explicit cage.
p = root / "main/sensors.go"
s = p.read_text()
old = "\t\t\t\tmySituation.AHRSLastAttitudeTime = time.Time{}\n\t\t\t\ts.Reset()"
new = "\t\t\t\tmySituation.AHRSLastAttitudeTime = time.Time{}\n\t\t\t\ts.ResetInvalid()"
if old in s:
    s = s.replace(old, new, 1)
elif "s.ResetInvalid()" not in s:
    raise SystemExit("Could not preserve adaptive state on invalid samples")
p.write_text(s)

# ---------------------------------------------------------------------------
# Settings controller: load selector state and save mode immediately. The
# setSettings helper has already been upgraded by patch-ux-audit.py.
# ---------------------------------------------------------------------------
p = root / "web/plates/js/settings.js"
s = p.read_text()
old = "\t\t$scope.IMU_Sensor_Enabled = settings.IMU_Sensor_Enabled;\n\t\t$scope.BMP_Sensor_Enabled = settings.BMP_Sensor_Enabled;"
new = "\t\t$scope.IMU_Sensor_Enabled = settings.IMU_Sensor_Enabled;\n\t\t$scope.AHRSV2_Enabled = settings.AHRSV2_Enabled === true;\n\t\t$scope.BMP_Sensor_Enabled = settings.BMP_Sensor_Enabled;"
if old in s:
    s = s.replace(old, new, 1)
elif "$scope.AHRSV2_Enabled = settings.AHRSV2_Enabled === true;" not in s:
    raise SystemExit("Could not load AHRSV2_Enabled in settings.js")

marker = "\t$scope.updateGLimits = function () {"
selector_fn = r'''	$scope.selectAHRSAlgorithm = function (useV2) {
		var next = !!useV2;
		if ($scope.AHRSV2_Enabled === next) return;
		var previous = !!$scope.AHRSV2_Enabled;
		$scope.AHRSV2_Enabled = next;
		setSettings({"AHRSV2_Enabled": next}, {
			successMessage: next ? "Adaptive AHRS v2 selected" : "Old Stratux AHRS selected",
			errorMessage: "Could not change the AHRS algorithm"
		}).catch(function () {
			$scope.AHRSV2_Enabled = previous;
		});
	};

'''
if "$scope.selectAHRSAlgorithm" not in s:
    if marker not in s:
        raise SystemExit("Could not find updateGLimits marker in settings.js")
    s = s.replace(marker, selector_fn + marker, 1)
p.write_text(s)

# ---------------------------------------------------------------------------
# Settings UI: explicit two-mode cards. Legacy is clearly named as the old
# upstream implementation; v2 is clearly marked Beta and explains its behavior.
# ---------------------------------------------------------------------------
p = root / "web/plates/settings.html"
s = p.read_text()
marker = '''                <div class="panel-heading">AHRS</div>
                <div class="panel-body">'''
selector_html = '''                <div class="panel-heading">AHRS</div>
                <div class="panel-body">
                    <div class="sx-ahrs-selector" ng-show="IMU_Sensor_Enabled">
                        <div class="sx-ahrs-selector-title">
                            <div><strong>Attitude algorithm</strong><small>Select which AHRS solution Stratux publishes to this page and ForeFlight/GDL90.</small></div>
                            <span class="sx-ahrs-beta" ng-show="AHRSV2_Enabled">BETA</span>
                        </div>
                        <div class="sx-ahrs-mode-grid">
                            <button type="button" class="sx-ahrs-mode" ng-class="{'is-selected': !AHRSV2_Enabled}" ng-click="selectAHRSAlgorithm(false)">
                                <span class="sx-ahrs-mode-radio"><i class="fa" ng-class="!AHRSV2_Enabled ? 'fa-dot-circle-o' : 'fa-circle-o'"></i></span>
                                <span><strong>Old Stratux AHRS</strong><small>Legacy upstream SimpleAHRS. Kept unchanged for compatibility and comparison.</small></span>
                            </button>
                            <button type="button" class="sx-ahrs-mode sx-ahrs-mode-v2" ng-class="{'is-selected': AHRSV2_Enabled}" ng-click="selectAHRSAlgorithm(true)">
                                <span class="sx-ahrs-mode-radio"><i class="fa" ng-class="AHRSV2_Enabled ? 'fa-dot-circle-o' : 'fa-circle-o'"></i></span>
                                <span><strong>Adaptive AHRS v2 <em>Beta</em></strong><small>Adaptive accelerometer trust, stationary gyro-bias learning, quaternion corrections, guarded alignment and quality diagnostics.</small></span>
                            </button>
                        </div>
                        <div class="sx-ahrs-mode-note" ng-show="AHRSV2_Enabled"><i class="fa fa-info-circle"></i> V2 is experimental. Validate attitude against a known reference before relying on it in flight. Both algorithms use the same sensor orientation and calibration.</div>
                    </div>'''
if "sx-ahrs-selector" not in s:
    if marker not in s:
        raise SystemExit("Could not find AHRS panel in settings.html")
    s = s.replace(marker, selector_html, 1)
p.write_text(s)

# ---------------------------------------------------------------------------
# GPS/AHRS page: show the active estimator and confidence directly beside AHRS.
# ---------------------------------------------------------------------------
p = root / "web/plates/gps.html"
s = p.read_text()
old = '''\t\t\t\t<span ng-hide="ConnectState == 'Connected'" class="label label-danger">{{ConnectState}}</span>'''
new = '''\t\t\t\t<span ng-hide="ConnectState == 'Connected'" class="label label-danger">{{ConnectState}}</span>
                <span class="sx-ahrs-active-mode" ng-if="ahrs_algorithm">{{ahrs_algorithm}}</span>
                <span class="sx-ahrs-confidence" ng-if="ahrs_algorithm && AHRSV2_Enabled">Quality {{ahrs_confidence}}/100<span ng-if="ahrs_stationary"> · stationary</span></span>'''
if old in s:
    s = s.replace(old, new, 1)
elif "sx-ahrs-active-mode" not in s:
    raise SystemExit("Could not add active AHRS mode to gps.html")
p.write_text(s)

p = root / "web/plates/js/gps.js"
s = p.read_text()
# Settings are already loaded on this controller; expose the configured mode so
# confidence is only presented as a v2 concept.
old = "$scope.IMU_Sensor_Enabled = settings.IMU_Sensor_Enabled;"
new = "$scope.IMU_Sensor_Enabled = settings.IMU_Sensor_Enabled;\n        $scope.AHRSV2_Enabled = settings.AHRSV2_Enabled === true;"
if old in s and "$scope.AHRSV2_Enabled" not in s:
    s = s.replace(old, new, 1)

# Insert diagnostic values whenever a situation response is processed.
needle = "$scope.ahrs_gload = situation.AHRSGLoad.toFixed(2);"
addition = '''$scope.ahrs_algorithm = situation.AHRSAlgorithm || ($scope.AHRSV2_Enabled ? 'Adaptive AHRS v2 (Beta)' : 'Old Stratux AHRS (Legacy)');
            $scope.ahrs_confidence = Math.max(0, Math.min(100, Math.round(Number(situation.AHRSConfidence || 0))));
            $scope.ahrs_stationary = !!situation.AHRSStationary;
            '''
if "$scope.ahrs_algorithm = situation.AHRSAlgorithm" not in s:
    if needle not in s:
        raise SystemExit("Could not find AHRS G-load update in gps.js")
    s = s.replace(needle, addition + needle, 1)
p.write_text(s)

# ---------------------------------------------------------------------------
# Help/documentation exposed in the installed web UI.
# ---------------------------------------------------------------------------
p = root / "web/plates/settings-help.html"
s = p.read_text()
old = '''    <h4>AHRS</h4>
    <p><strong>Calibrate AHRS Sensors</strong> guides initial setup of the AHRS function,'''
new = '''    <h4>AHRS</h4>
    <p><strong>Attitude algorithm</strong> offers two implementations. <strong>Old Stratux AHRS</strong> is the unchanged upstream SimpleAHRS compatibility mode. <strong>Adaptive AHRS v2 (Beta)</strong> adds adaptive accelerometer weighting, stationary gyro-bias learning, bounded correction, GPS acceleration compensation, guarded initialization, and quality diagnostics. The selected solution is the one published to the Stratux display and AHRS-capable GDL90 clients such as ForeFlight. V2 is experimental and is not a certified flight instrument.</p>
    <p><strong>Calibrate AHRS Sensors</strong> guides initial setup of the AHRS function,'''
if old in s:
    s = s.replace(old, new, 1)
elif "Adaptive AHRS v2 (Beta)" not in s:
    raise SystemExit("Could not extend Settings AHRS help")
p.write_text(s)

# Settings reference documentation in the source tree.
p = root / "docs/settings-reference.md"
s = p.read_text()
old = "| `IMU_Sensor_Enabled` | bool | IMU / AHRS sensor (ICM-20948, MPU-9250 family). |"
new = old + "\n| `AHRSV2_Enabled` | bool | Selects Adaptive AHRS v2 (Beta) when true; false keeps Old Stratux AHRS (upstream SimpleAHRS). |"
if old in s and "`AHRSV2_Enabled`" not in s:
    s = s.replace(old, new, 1)
p.write_text(s)

# GDL90 docs: make it explicit that this is not a web-only visual switch.
p = root / "docs/integration/gdl90.md"
s = p.read_text()
section = '''

### Selectable AHRS algorithm in the custom build

This build can publish either **Old Stratux AHRS (Legacy)** or **Adaptive AHRS v2 (Beta)**. The selected estimator writes the shared `mySituation` AHRS fields before the existing AHRS GDL90/ForeFlight messages are generated, so the algorithm selection affects connected EFB attitude data as well as the local web display. `AHRSV2_Enabled=false` preserves upstream SimpleAHRS behavior.
'''
if "Selectable AHRS algorithm in the custom build" not in s:
    s += section
p.write_text(s)

# ---------------------------------------------------------------------------
# Styling integrated into the shared modern UI.
# ---------------------------------------------------------------------------
p = root / "web/css/modern-ui-extensions.css"
s = p.read_text()
css = r'''
/* Selectable AHRS algorithms ------------------------------------------------ */
.sx-ahrs-selector{margin:0 0 18px;padding:16px;border:1px solid #dbe3ee;border-radius:14px;background:#f8fafc}.sx-ahrs-selector-title{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;margin-bottom:12px}.sx-ahrs-selector-title strong{display:block;font-size:14px;color:#111827}.sx-ahrs-selector-title small{display:block;margin-top:3px;color:#64748b;font-size:11px;line-height:1.45}.sx-ahrs-beta{padding:4px 8px;border-radius:999px;background:#dbeafe;color:#1d4ed8;font-size:9px;font-weight:800;letter-spacing:.08em}.sx-ahrs-mode-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px}.sx-ahrs-mode{display:flex;align-items:flex-start;gap:10px;width:100%;min-height:86px;padding:13px;border:1px solid #d7dee8;border-radius:12px;background:#fff;text-align:left;color:#111827;box-shadow:none}.sx-ahrs-mode:hover{border-color:#93c5fd}.sx-ahrs-mode.is-selected{border:2px solid #2563eb;background:#eff6ff;padding:12px}.sx-ahrs-mode-radio{padding-top:1px;color:#2563eb;font-size:16px}.sx-ahrs-mode strong{display:block;font-size:13px}.sx-ahrs-mode strong em{display:inline-block;margin-left:5px;padding:2px 5px;border-radius:999px;background:#dbeafe;color:#1d4ed8;font-size:8px;font-style:normal;letter-spacing:.06em}.sx-ahrs-mode small{display:block;margin-top:5px;color:#64748b;font-size:10px;line-height:1.45;font-weight:400}.sx-ahrs-mode-note{margin-top:10px;padding:9px 11px;border-radius:9px;background:#fff7ed;color:#9a3412;font-size:10px;line-height:1.45}.sx-ahrs-mode-note i{margin-right:5px}.sx-ahrs-active-mode{display:inline-block;margin-left:7px;padding:3px 7px;border-radius:999px;background:#eef2ff;color:#3730a3;font-size:9px;font-weight:800;vertical-align:middle}.sx-ahrs-confidence{float:right;margin-top:1px;color:#64748b;font-size:9px;font-weight:700}.stratux-dark .sx-ahrs-selector{background:#111827;border-color:#334155}.stratux-dark .sx-ahrs-selector-title strong,.stratux-dark .sx-ahrs-mode strong{color:#f8fafc}.stratux-dark .sx-ahrs-mode{background:#0f172a;border-color:#334155;color:#f8fafc}.stratux-dark .sx-ahrs-mode.is-selected{background:#172554;border-color:#3b82f6}.stratux-dark .sx-ahrs-mode-note{background:#431407;color:#fed7aa}@media(max-width:700px){.sx-ahrs-mode-grid{grid-template-columns:1fr}.sx-ahrs-confidence{float:none;display:block;margin:5px 0 0}}
'''
if ".sx-ahrs-selector{" not in s:
    s += css
p.write_text(s)

print("Selectable Old Stratux AHRS / Adaptive AHRS v2 beta wired successfully")

