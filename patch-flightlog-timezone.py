#!/usr/bin/env python3
import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1])
p = root / 'main/flightlog.go'
s = p.read_text()

# Imports for compact offline timezone grid.
if '"bufio"' not in s:
    s = s.replace('import (\n\t"encoding/csv"', 'import (\n\t"bufio"\n\t"encoding/csv"', 1)
if '"io"' not in s:
    s = s.replace('\t"encoding/json"\n', '\t"encoding/json"\n\t"io"\n', 1)

old = '''type flightLogSettings struct {\n\tTimezone   string `json:"Timezone"`\n\tAutoDetect bool   `json:"AutoDetect"`\n}'''
new = '''type flightLogSettings struct {\n\tTimezone     string `json:"Timezone"`\n\tAutoDetect   bool   `json:"AutoDetect"`\n\tAutoTimezone bool   `json:"AutoTimezone"`\n}'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not add AutoTimezone setting')

airport_start = s.find('type flightAirport struct {')
airport_end = s.find('\n}', airport_start)
if airport_start == -1 or airport_end == -1:
    raise SystemExit('Could not find flightAirport struct')
airport_block = s[airport_start:airport_end]
if 'json:"Timezone,omitempty"' not in airport_block:
    updated, count = re.subn(
        r'(\n\s*DistanceNM\s+float64\s+`json:"DistanceNM,omitempty"`)',
        r'\1\n\tTimezone     string  `json:"Timezone,omitempty"`',
        airport_block,
        count=1,
    )
    if count != 1:
        raise SystemExit('Could not add airport timezone')
    s = s[:airport_start] + updated + s[airport_end:]

response_start = s.find('type flightLogResponse struct {')
response_end = s.find('\n}', response_start)
if response_start == -1 or response_end == -1:
    raise SystemExit('Could not find flightLogResponse struct')
response_block = s[response_start:response_end]
if 'json:"TimezoneDatabaseReady"' not in response_block:
    updated, count = re.subn(
        r'(\n\s*AirportCount\s+int\s+`json:"AirportCount"`)',
        r'\1\n\tTimezoneDatabaseReady bool   `json:"TimezoneDatabaseReady"`\n\tLocalTimezone         string `json:"LocalTimezone"`\n\tTimezoneSource        string `json:"TimezoneSource"`',
        response_block,
        count=1,
    )
    if count != 1:
        raise SystemExit('Could not add timezone response fields')
    s = s[:response_start] + updated + s[response_end:]

marker = '''type flightCandidate struct {\n\tsince time.Time\n\tpoint flightTrackPoint\n}\n'''
helper = marker + '''\ntype flightTimezoneGrid struct {\n\tResolution float64\n\tWidth      int\n\tHeight     int\n\tZones      []string\n\tCells      []byte\n}\n'''
if 'type flightTimezoneGrid struct' not in s:
    if marker not in s:
        raise SystemExit('Could not add timezone grid type')
    s = s.replace(marker, helper, 1)

if 'flightSettings = flightLogSettings{Timezone: "UTC", AutoDetect: true, AutoTimezone: true}' not in s:
    s, count = re.subn(
        r'flightSettings\s*=\s*flightLogSettings\{Timezone: "UTC", AutoDetect: true\}',
        'flightSettings = flightLogSettings{Timezone: "UTC", AutoDetect: true, AutoTimezone: true}',
        s,
        count=1,
    )
    if count != 1:
        raise SystemExit('Could not make automatic timezone default')

if 'flightTimezoneGrid' not in s[s.find('var ('):s.find('\n)', s.find('var ('))]:
    s, count = re.subn(
        r'(\n\s*flightLastContextSample\s+time\.Time)(\n\))',
        r'\1\n\tflightTimezoneGrid        flightTimezoneGrid\n\tflightTimezoneDBReady     bool\n\tflightResolvedTimezone    = "UTC"\n\tflightTimezoneSource      = "fallback"\n\tflightTimezoneCandidate   string\n\tflightTimezoneCandidateAt time.Time\2',
        s,
        count=1,
    )
    if count != 1:
        raise SystemExit('Could not add timezone runtime state')

# Resolve timezone continuously from GPS, with 30s hysteresis away from airports.
if 'updateFlightResolvedTimezoneLocked(sample)' not in s:
    monitor_start = s.find('func flightLogMonitorLoop() {')
    monitor_end = s.find('\n}', monitor_start)
    if monitor_start == -1 or monitor_end == -1:
        raise SystemExit('Could not find Flight Log monitor loop')
    monitor_block = s[monitor_start:monitor_end]
    persist_marker = re.search(
        r'\n\t\tif flightCurrent != nil && time\.Since\(flightLastPersist\) > [^{]+ \{',
        monitor_block,
    )
    if persist_marker is None:
        raise SystemExit('Could not wire automatic timezone resolver into monitor loop')
    insert_at = monitor_start + persist_marker.start()
    s = s[:insert_at] + '\n\t\tif sample.Valid {\n\t\t\tupdateFlightResolvedTimezoneLocked(sample)\n\t\t}' + s[insert_at:]

initialize_start = s.find('func initializeFlightLog() {')
initialize_end = s.find('\n}', initialize_start)
if initialize_start == -1 or initialize_end == -1:
    raise SystemExit('Could not find Flight Log initialization')
initialize_block = s[initialize_start:initialize_end]
if '\tloadFlightTimezoneGridLocked()' not in initialize_block:
    marker = '\tloadAirportDatabaseLocked()\n'
    if marker not in initialize_block:
        raise SystemExit('Could not load timezone grid during initialization')
    initialize_block = initialize_block.replace(marker, marker + '\tloadFlightTimezoneGridLocked()\n', 1)
if 'flightTimezoneSource = "manual"' not in initialize_block:
    marker = '\tflightInitialized = true'
    manual = '\tif !flightSettings.AutoTimezone {\n\t\tflightResolvedTimezone = flightSettings.Timezone\n\t\tflightTimezoneSource = "manual"\n\t}\n'
    if marker not in initialize_block:
        raise SystemExit('Could not initialize manual timezone state')
    initialize_block = initialize_block.replace(marker, manual + marker, 1)
s = s[:initialize_start] + initialize_block + s[initialize_end:]

# Preserve automatic timezone default when upgrading from an older settings JSON.
start = s.find('func loadFlightSettingsLocked() {')
end = s.find('\nfunc saveFlightSettingsLocked()', start)
if start == -1 or end == -1:
    raise SystemExit('Could not find loadFlightSettingsLocked')
replacement = '''func loadFlightSettingsLocked() {\n\tb, err := os.ReadFile(flightSettingsPath)\n\tif err != nil {\n\t\treturn\n\t}\n\tvar raw struct {\n\t\tTimezone     string `json:"Timezone"`\n\t\tAutoDetect   *bool  `json:"AutoDetect"`\n\t\tAutoTimezone *bool  `json:"AutoTimezone"`\n\t}\n\tif json.Unmarshal(b, &raw) != nil {\n\t\treturn\n\t}\n\tif raw.Timezone != "" {\n\t\tif _, err := time.LoadLocation(raw.Timezone); err == nil {\n\t\t\tflightSettings.Timezone = raw.Timezone\n\t\t}\n\t}\n\tif raw.AutoDetect != nil {\n\t\tflightSettings.AutoDetect = *raw.AutoDetect\n\t}\n\tif raw.AutoTimezone != nil {\n\t\tflightSettings.AutoTimezone = *raw.AutoTimezone\n\t}\n}\n'''
s = s[:start] + replacement + s[end:]

# Airport CSV now has an exact timezone column.
old = '''\t\tairports = append(airports, flightAirport{Code: row[0], Name: row[1], Latitude: lat, Longitude: lon, ElevationFt: elev, Type: row[5], Municipality: row[6], Country: row[7]})'''
new = '''\t\ttz := ""\n\t\tif len(row) > 8 {\n\t\t\ttz = strings.TrimSpace(row[8])\n\t\t}\n\t\tairports = append(airports, flightAirport{Code: row[0], Name: row[1], Latitude: lat, Longitude: lon, ElevationFt: elev, Type: row[5], Municipality: row[6], Country: row[7], Timezone: tz})'''
if old in s:
    s = s.replace(old, new, 1)
elif 'Timezone: tz' not in s:
    raise SystemExit('Could not read airport timezone column')

# Insert compact timezone grid loader and GPS lookup before readFlightGPS.
marker = '''func readFlightGPS() flightLiveGPS {'''
helper = r'''func loadFlightTimezoneGridLocked() {
	paths := []string{"/opt/stratux/share/timezones.bin", "/opt/stratux/timezones.bin"}
	var f *os.File
	for _, path := range paths {
		candidate, err := os.Open(path)
		if err == nil {
			f = candidate
			break
		}
	}
	if f == nil {
		flightTimezoneDBReady = false
		return
	}
	defer f.Close()
	r := bufio.NewReader(f)
	magic, err := r.ReadString('\n')
	if err != nil || strings.TrimSpace(magic) != "SXTZ1" {
		flightTimezoneDBReady = false
		return
	}
	metaLine, err := r.ReadString('\n')
	if err != nil {
		flightTimezoneDBReady = false
		return
	}
	var meta struct {
		Resolution float64  `json:"resolution"`
		Width      int      `json:"width"`
		Height     int      `json:"height"`
		Zones      []string `json:"zones"`
	}
	if json.Unmarshal([]byte(metaLine), &meta) != nil || meta.Resolution <= 0 || meta.Width <= 0 || meta.Height <= 0 || len(meta.Zones) == 0 {
		flightTimezoneDBReady = false
		return
	}
	cells, err := io.ReadAll(r)
	if err != nil || len(cells) != meta.Width*meta.Height*2 {
		flightTimezoneDBReady = false
		return
	}
	flightTimezoneGrid = flightTimezoneGrid{Resolution: meta.Resolution, Width: meta.Width, Height: meta.Height, Zones: meta.Zones, Cells: cells}
	flightTimezoneDBReady = true
}

func flightTimezoneFromGridLocked(lat, lon float64) string {
	if !flightTimezoneDBReady || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return ""
	}
	x := int(math.Floor((lon + 180.0) / flightTimezoneGrid.Resolution))
	y := int(math.Floor((90.0 - lat) / flightTimezoneGrid.Resolution))
	if x < 0 {
		x = 0
	}
	if x >= flightTimezoneGrid.Width {
		x = flightTimezoneGrid.Width - 1
	}
	if y < 0 {
		y = 0
	}
	if y >= flightTimezoneGrid.Height {
		y = flightTimezoneGrid.Height - 1
	}
	idx := (y*flightTimezoneGrid.Width + x) * 2
	if idx < 0 || idx+1 >= len(flightTimezoneGrid.Cells) {
		return ""
	}
	zoneID := int(flightTimezoneGrid.Cells[idx]) | int(flightTimezoneGrid.Cells[idx+1])<<8
	if zoneID < 0 || zoneID >= len(flightTimezoneGrid.Zones) {
		return ""
	}
	return flightTimezoneGrid.Zones[zoneID]
}

func flightTimezoneForPositionLocked(sample flightLiveGPS) (string, string) {
	// Near a known airport, use its exact timezone. This is more precise than the
	// 0.25-degree global grid at timezone borders and is ideal for taxi/takeoff/landing.
	if flightCurrentAirport != nil && flightCurrentAirport.Timezone != "" {
		d := flightDistanceNM(sample.Latitude, sample.Longitude, flightCurrentAirport.Latitude, flightCurrentAirport.Longitude)
		if d <= 5.0 {
			return flightCurrentAirport.Timezone, "airport"
		}
	}
	if flightCurrent != nil {
		if flightCurrent.Phase == flightPhaseTaxiOut && flightCurrent.DepartureAirport != nil && flightCurrent.DepartureAirport.Timezone != "" {
			d := flightDistanceNM(sample.Latitude, sample.Longitude, flightCurrent.DepartureAirport.Latitude, flightCurrent.DepartureAirport.Longitude)
			if d <= 5.0 {
				return flightCurrent.DepartureAirport.Timezone, "departure airport"
			}
		}
		if flightCurrent.ArrivalAirport != nil && flightCurrent.ArrivalAirport.Timezone != "" {
			d := flightDistanceNM(sample.Latitude, sample.Longitude, flightCurrent.ArrivalAirport.Latitude, flightCurrent.ArrivalAirport.Longitude)
			if d <= 5.0 {
				return flightCurrent.ArrivalAirport.Timezone, "arrival airport"
			}
		}
	}
	if zone := flightTimezoneFromGridLocked(sample.Latitude, sample.Longitude); zone != "" {
		return zone, "GPS position"
	}
	return flightSettings.Timezone, "fallback"
}

func updateFlightResolvedTimezoneLocked(sample flightLiveGPS) {
	if !flightSettings.AutoTimezone {
		flightResolvedTimezone = flightSettings.Timezone
		flightTimezoneSource = "manual"
		flightTimezoneCandidate = ""
		flightTimezoneCandidateAt = time.Time{}
		return
	}
	candidate, source := flightTimezoneForPositionLocked(sample)
	if candidate == "" {
		return
	}
	if flightResolvedTimezone == "" || source != "GPS position" {
		flightResolvedTimezone = candidate
		flightTimezoneSource = source
		flightTimezoneCandidate = ""
		flightTimezoneCandidateAt = time.Time{}
		return
	}
	if candidate == flightResolvedTimezone {
		flightTimezoneSource = source
		flightTimezoneCandidate = ""
		flightTimezoneCandidateAt = time.Time{}
		return
	}
	if candidate != flightTimezoneCandidate {
		flightTimezoneCandidate = candidate
		flightTimezoneCandidateAt = time.Now()
		return
	}
	if !flightTimezoneCandidateAt.IsZero() && time.Since(flightTimezoneCandidateAt) >= 30*time.Second {
		flightResolvedTimezone = candidate
		flightTimezoneSource = source
		flightTimezoneCandidate = ""
		flightTimezoneCandidateAt = time.Time{}
	}
}

func readFlightGPS() flightLiveGPS {'''
if 'func loadFlightTimezoneGridLocked()' not in s:
    if marker not in s:
        raise SystemExit('Could not insert timezone grid runtime')
    s = s.replace(marker, helper, 1)

# API exposes resolved timezone and source.
if 'TimezoneDatabaseReady: flightTimezoneDBReady' not in s:
    response_marker = '\tresp := flightLogResponse{'
    marker_at = s.find(response_marker, s.find('func handleFlightLog('))
    if marker_at == -1:
        raise SystemExit('Could not expose resolved timezone in Flight Log API')
    local_state = '''\tlocalTimezone := flightResolvedTimezone\n\ttimezoneSource := flightTimezoneSource\n\tif localTimezone == "" {\n\t\tlocalTimezone = settings.Timezone\n\t\ttimezoneSource = "fallback"\n\t}\n'''
    s = s[:marker_at] + local_state + s[marker_at:]
    count_marker = 'AirportCount: len(flightAirports),'
    response_end = s.find('\n', marker_at + len(local_state))
    response_line = s[marker_at + len(local_state):response_end]
    if count_marker not in response_line:
        raise SystemExit('Could not add resolved timezone fields to Flight Log response')
    response_line = response_line.replace(
        count_marker,
        count_marker + ' TimezoneDatabaseReady: flightTimezoneDBReady, LocalTimezone: localTimezone, TimezoneSource: timezoneSource,',
        1,
    )
    s = s[:marker_at + len(local_state)] + response_line + s[response_end:]

# Settings save immediately switches mode; manual timezone remains a fallback.
old = '''\tflightLogMu.Lock()\n\tflightSettings = cfg\n\tsaveFlightSettingsLocked()\n\tflightLogMu.Unlock()'''
new = '''\tflightLogMu.Lock()\n\tflightSettings = cfg\n\tif !cfg.AutoTimezone {\n\t\tflightResolvedTimezone = cfg.Timezone\n\t\tflightTimezoneSource = "manual"\n\t\tflightTimezoneCandidate = ""\n\t\tflightTimezoneCandidateAt = time.Time{}\n\t}\n\tsaveFlightSettingsLocked()\n\tflightLogMu.Unlock()'''
if old in s:
    s = s.replace(old, new, 1)
elif 'if !cfg.AutoTimezone {' not in s:
    raise SystemExit('Could not wire timezone settings mode')

p.write_text(s)

# ---------------------------------------------------------------------------
# Frontend: always display the GPS-resolved timezone when automatic mode is on.
# ---------------------------------------------------------------------------
p = root / 'web/plates/js/flightlog.js'
s = p.read_text()
s = s.replace("Settings: {Timezone:'UTC', AutoDetect:true},", "Settings: {Timezone:'UTC', AutoDetect:true, AutoTimezone:true},", 1)
if "LocalTimezone: 'UTC'" not in s:
    s = s.replace("StoragePath: ''", "LocalTimezone: 'UTC',\n        TimezoneSource: 'fallback',\n        TimezoneDatabaseReady: false,\n        StoragePath: ''", 1)

old = '''    function timezone() {\n        return ($scope.settings && $scope.settings.Timezone) || 'UTC';\n    }'''
new = '''    function timezone() {\n        if ($scope.settings && $scope.settings.AutoTimezone && $scope.data && $scope.data.LocalTimezone) {\n            return $scope.data.LocalTimezone;\n        }\n        return ($scope.settings && $scope.settings.Timezone) || ($scope.data && $scope.data.LocalTimezone) || 'UTC';\n    }\n\n    $scope.localTimezoneLabel = function() {\n        return timezone();\n    };\n\n    $scope.timezoneSourceLabel = function() {\n        if ($scope.settings && !$scope.settings.AutoTimezone) return 'Manual timezone';\n        var source = ($scope.data && $scope.data.TimezoneSource) || 'GPS position';\n        return 'Auto · ' + source;\n    };'''
if old in s:
    s = s.replace(old, new, 1)
elif '$scope.localTimezoneLabel' not in s:
    raise SystemExit('Could not make frontend use resolved timezone')

old = '''            Timezone: ($scope.settings.Timezone || 'UTC').trim(),\n            AutoDetect: !!$scope.settings.AutoDetect'''
new = '''            Timezone: ($scope.settings.Timezone || 'UTC').trim(),\n            AutoDetect: !!$scope.settings.AutoDetect,\n            AutoTimezone: $scope.settings.AutoTimezone !== false'''
if old in s:
    s = s.replace(old, new, 1)
elif 'AutoTimezone: $scope.settings.AutoTimezone !== false' not in s:
    raise SystemExit('Could not save automatic timezone setting')

p.write_text(s)

p = root / 'web/plates/flightlog.html'
s = p.read_text()
s = s.replace("<small>{{settings.Timezone || 'UTC'}}</small>", "<small>{{localTimezoneLabel()}} · {{timezoneSourceLabel()}}</small>", 1)
s = s.replace('''          <span>LOCAL TIME</span>\n          <strong>{{formatLocalClock(data.GPS.TimeUTC)}}</strong>\n        </div>''', '''          <span>LOCAL TIME</span>\n          <strong>{{formatLocalClock(data.GPS.TimeUTC)}}</strong>\n          <small>{{localTimezoneLabel()}}</small>\n        </div>''', 1)

old = '''  <div class="flightlog-panel">\n    <div class="flightlog-panel-header">\n      <div><h3>Time & detection</h3><div class="flightlog-muted">All recorded event times are stored in UTC/Zulu. Local time is only a display preference.</div></div>\n      <label class="flightlog-switch"><span>Automatic flight detection</span><input type="checkbox" ng-model="settings.AutoDetect"></label>\n    </div>\n    <div class="flightlog-settings">\n      <div class="flightlog-field">\n        <label>Local timezone</label>\n        <input type="text" ng-model="settings.Timezone" placeholder="America/Toronto">\n        <small>Use an IANA timezone such as America/Toronto, America/Santiago or UTC. DST is handled automatically.</small>\n      </div>\n      <div class="flightlog-field flightlog-browser-zone" ng-if="browserTimezone">\n        <label>This browser reports</label>\n        <button class="btn btn-default" ng-click="useBrowserTimezone()"><i class="fa fa-clock-o"></i> Use {{browserTimezone}}</button>\n        <small>Useful when you move the aircraft to another region.</small>\n      </div>\n      <div class="flightlog-field flightlog-save-field">\n        <button class="btn btn-primary" ng-click="saveSettings()" ng-disabled="savingSettings"><i class="fa" ng-class="savingSettings ? 'fa-circle-o-notch fa-spin' : 'fa-save'"></i> {{savingSettings ? 'Saving...' : 'Save flight settings'}}</button>\n      </div>\n    </div>\n    <div class="flightlog-db-note" ng-class="{'ok': data.AirportDatabaseReady}"><i class="fa fa-database"></i> {{data.AirportDatabaseReady ? (data.AirportCount + ' airports available offline for departure/arrival detection') : 'Airport database not loaded yet'}}</div>\n  </div>'''
new = '''  <div class="flightlog-panel">\n    <div class="flightlog-panel-header">\n      <div><h3>Time & detection</h3><div class="flightlog-muted">Recorded events always stay in UTC/Zulu. Local time follows the aircraft automatically from GPS coordinates.</div></div>\n      <div class="flightlog-switch-stack">\n        <label class="flightlog-switch"><span>Automatic flight detection</span><input type="checkbox" ng-model="settings.AutoDetect"></label>\n        <label class="flightlog-switch"><span>Automatic timezone from GPS</span><input type="checkbox" ng-model="settings.AutoTimezone"></label>\n      </div>\n    </div>\n    <div class="flightlog-timezone-live" ng-class="{'ok': settings.AutoTimezone && data.TimezoneDatabaseReady}">\n      <div><span>CURRENT LOCAL TIMEZONE</span><strong>{{localTimezoneLabel()}}</strong><small>{{timezoneSourceLabel()}}</small></div>\n      <div><span>LOCAL CLOCK</span><strong>{{formatLocalClock(data.GPS.TimeUTC)}}</strong><small>{{formatZuluClock(data.GPS.TimeUTC)}} reference</small></div>\n    </div>\n    <div class="flightlog-settings">\n      <div class="flightlog-field">\n        <label>{{settings.AutoTimezone ? 'Fallback timezone' : 'Manual timezone'}}</label>\n        <input type="text" ng-model="settings.Timezone" placeholder="America/Toronto" ng-disabled="settings.AutoTimezone">\n        <small ng-if="settings.AutoTimezone">Used only if GPS or the offline timezone database is unavailable. GPS changes timezone automatically as the aircraft crosses regions.</small>\n        <small ng-if="!settings.AutoTimezone">Use an IANA timezone such as America/Toronto, America/Santiago or UTC. DST is handled automatically.</small>\n      </div>\n      <div class="flightlog-field flightlog-browser-zone" ng-if="browserTimezone && !settings.AutoTimezone">\n        <label>This browser reports</label>\n        <button class="btn btn-default" ng-click="useBrowserTimezone()"><i class="fa fa-clock-o"></i> Use {{browserTimezone}}</button>\n        <small>Sets the manual timezone from this device.</small>\n      </div>\n      <div class="flightlog-field flightlog-save-field">\n        <button class="btn btn-primary" ng-click="saveSettings()" ng-disabled="savingSettings"><i class="fa" ng-class="savingSettings ? 'fa-circle-o-notch fa-spin' : 'fa-save'"></i> {{savingSettings ? 'Saving...' : 'Save flight settings'}}</button>\n      </div>\n    </div>\n    <div class="flightlog-db-note" ng-class="{'ok': data.TimezoneDatabaseReady}"><i class="fa fa-globe"></i> {{data.TimezoneDatabaseReady ? 'Worldwide offline GPS timezone database ready · automatic DST-aware local time' : 'Timezone database not loaded · fallback timezone will be used'}}</div>\n    <div class="flightlog-db-note" ng-class="{'ok': data.AirportDatabaseReady}"><i class="fa fa-database"></i> {{data.AirportDatabaseReady ? (data.AirportCount + ' airports available offline for departure/arrival detection') : 'Airport database not loaded yet'}}</div>\n  </div>'''
if old in s:
    s = s.replace(old, new, 1)
elif 'Automatic timezone from GPS' not in s:
    raise SystemExit('Could not replace Time & detection UI with automatic timezone UX')

p.write_text(s)

# Styling for resolved timezone card and switch stack.
p = root / 'web/css/flightlog.css'
s = p.read_text()
extra = '''
.flightlog-switch-stack {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 12px;
}
.flightlog-timezone-live {
  display: grid;
  grid-template-columns: 1.5fr 1fr;
  gap: 12px;
  padding: 14px 18px;
  border-bottom: 1px solid var(--sx-border);
  background: var(--sx-card-soft);
}
.flightlog-timezone-live > div {
  padding: 11px 13px;
  border: 1px solid var(--sx-border);
  border-radius: 8px;
  background: var(--sx-card);
}
.flightlog-timezone-live > div:first-child {
  border-color: var(--sx-primary);
  background: var(--sx-primary-soft);
}
.flightlog-timezone-live span {
  display: block;
  color: var(--sx-muted);
  font-size: 9px;
  font-weight: 800;
  letter-spacing: .07em;
}
.flightlog-timezone-live strong {
  display: block;
  margin-top: 4px;
  color: var(--sx-text);
  font-size: 17px;
}
.flightlog-timezone-live small {
  display: block;
  margin-top: 3px;
  color: var(--sx-muted);
  font-size: 10px;
}
.flightlog-field input:disabled {
  border-color: var(--sx-border);
  background: var(--sx-card-soft);
  color: var(--sx-muted);
  cursor: not-allowed;
}
@media (max-width: 700px) {
  .flightlog-switch-stack {
    display: grid;
    width: 100%;
    justify-content: stretch;
  }
  .flightlog-timezone-live {
    grid-template-columns: 1fr;
    padding: 12px 14px;
  }
}
'''
if '.flightlog-timezone-live' not in s:
    s += extra
p.write_text(s)
