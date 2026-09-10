#!/usr/bin/env python3
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

# Keep automatic detection conservative enough that GPS drift does not invent flights.
p = root / 'main/flightlog.go'
s = p.read_text()
s = s.replace('''\tif !sample.Valid || sample.HorizontalAccuracyM > 250 {\n\t\treturn\n\t}''', '''\tif !sample.Valid || sample.HorizontalAccuracyM > 100 {\n\t\treturn\n\t}''', 1)
s = s.replace('''\t\tif speed >= 3 {\n\t\t\tif flightMoveCandidate.since.IsZero() {\n\t\t\t\tflightMoveCandidate = flightCandidate{since: now, point: pointFromGPS(sample)}\n\t\t\t}\n\t\t\tif now.Sub(flightMoveCandidate.since) >= 8*time.Second {''', '''\t\tif speed >= 4 {\n\t\t\tif flightMoveCandidate.since.IsZero() {\n\t\t\t\tflightMoveCandidate = flightCandidate{since: now, point: pointFromGPS(sample)}\n\t\t\t}\n\t\t\tif now.Sub(flightMoveCandidate.since) >= 10*time.Second {''', 1)

# When parked, only call something the current airport when we are actually close to it.
s = s.replace('''\t\tif sample.Valid && sample.GroundSpeedKt < 20 && time.Since(flightLastAirportResolve) > 15*time.Second {\n\t\t\tflightCurrentAirport = nearestAirportLocked(sample.Latitude, sample.Longitude, sample.AltitudeFt, 8.0)''', '''\t\tif sample.Valid && sample.GroundSpeedKt < 20 && time.Since(flightLastAirportResolve) > 15*time.Second {\n\t\t\tflightCurrentAirport = nearestAirportLocked(sample.Latitude, sample.Longitude, sample.AltitudeFt, 3.0)''', 1)

old = '''\tcase flightPhaseAirborne:\n\t\tnearby := nearestAirportLocked(sample.Latitude, sample.Longitude, sample.AltitudeFt, 8.0)\n\t\taltitudePlausible := nearby != nil && math.Abs(sample.AltitudeFt-nearby.ElevationFt) < 1800\n\t\tlandingEvidence := (speed <= 35 && absVS <= 4.0 && altitudePlausible) || speed <= 20'''
new = '''\tcase flightPhaseAirborne:\n\t\t// Only resolve an airport at plausible approach speeds. That keeps cruise CPU\n\t\t// cost tiny even with the worldwide offline airport database.\n\t\tvar nearby *flightAirport\n\t\tif speed <= 65 {\n\t\t\tnearby = nearestAirportLocked(sample.Latitude, sample.Longitude, sample.AltitudeFt, 5.0)\n\t\t}\n\t\theightAboveAirport := 99999.0\n\t\tif nearby != nil {\n\t\t\theightAboveAirport = math.Abs(sample.AltitudeFt - nearby.ElevationFt)\n\t\t}\n\t\t// GPS-only touchdown estimate: low AGL, approach speed and a flattened\n\t\t// vertical rate. The recorded landing time is the first qualifying sample.\n\t\tlandingEvidence := (nearby != nil && speed <= 60 && heightAboveAirport <= 55 && absVS <= 3.0) || speed <= 20'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not optimize airborne airport resolution')

# Shorter persistence window after first touchdown evidence keeps the timestamp close
# to touchdown while still rejecting one-sample noise.
s = s.replace('''\t\t\tif now.Sub(flightLandingCandidate.since) >= 10*time.Second {''', '''\t\t\tif now.Sub(flightLandingCandidate.since) >= 4*time.Second {''', 1)

# The offline database intentionally contains every OurAirports facility, including
# heliports, seaplane bases, balloonports and closed fields. For this fixed-wing
# logger, automatic airport assignment must only choose active land airports. That
# prevents a nearby hospital heliport or historical closed field from winning the
# nearest-airport calculation while preserving those records for future UI/search.
marker = '''func nearestAirportLocked(lat, lon, altitudeFt, maxNM float64) *flightAirport {'''
helper = '''func flightAirportUsableForFixedWing(a *flightAirport) bool {\n\tif a == nil {\n\t\treturn false\n\t}\n\tswitch strings.ToLower(strings.TrimSpace(a.Type)) {\n\tcase "small_airport", "medium_airport", "large_airport":\n\t\treturn true\n\tdefault:\n\t\treturn false\n\t}\n}\n\nfunc nearestAirportLocked(lat, lon, altitudeFt, maxNM float64) *flightAirport {'''
if 'func flightAirportUsableForFixedWing(' not in s:
    if marker not in s:
        raise SystemExit('Could not add fixed-wing airport suitability filter')
    s = s.replace(marker, helper, 1)

old = '''\tfor i := range flightAirports {\n\t\ta := &flightAirports[i]\n\t\td := flightDistanceNM(lat, lon, a.Latitude, a.Longitude)'''
new = '''\tfor i := range flightAirports {\n\t\ta := &flightAirports[i]\n\t\tif !flightAirportUsableForFixedWing(a) {\n\t\t\tcontinue\n\t\t}\n\t\td := flightDistanceNM(lat, lon, a.Latitude, a.Longitude)'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not enforce fixed-wing suitability in nearest-airport resolver')

p.write_text(s)

# Avoid String.padStart so older iPad Safari versions can still render Zulu times.
p = root / 'web/plates/js/flightlog.js'
s = p.read_text()
if 'function pad2(value)' not in s:
    marker = '''    function validDate(value) {'''
    helper = '''    function pad2(value) {\n        value = String(value);\n        return value.length < 2 ? '0' + value : value;\n    }\n\n    function validDate(value) {'''
    if marker not in s:
        raise SystemExit('Could not add Flight Log pad2 helper')
    s = s.replace(marker, helper, 1)
s = s.replace("String(d.getUTCHours()).padStart(2,'0')", "pad2(d.getUTCHours())")
s = s.replace("String(d.getUTCMinutes()).padStart(2,'0')", "pad2(d.getUTCMinutes())")
s = s.replace("String(d.getUTCSeconds()).padStart(2,'0')", "pad2(d.getUTCSeconds())")
s = s.replace("String(d.getUTCMonth()+1).padStart(2,'0')", "pad2(d.getUTCMonth()+1)")
s = s.replace("String(d.getUTCDate()).padStart(2,'0')", "pad2(d.getUTCDate())")
s = s.replace("String(m).padStart(2,'0')", "pad2(m)")
s = s.replace("String(s).padStart(2,'0')", "pad2(s)")
p.write_text(s)
