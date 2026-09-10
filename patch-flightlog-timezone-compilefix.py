#!/usr/bin/env python3
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
p = root / 'main/flightlog.go'
s = p.read_text()

# Go uses a single package-level identifier namespace for types and vars.
# The timezone patch defines the type `flightTimezoneGrid`; keep that type name
# and rename the runtime instance so the generated backend compiles.
s = s.replace('\tflightTimezoneGrid        flightTimezoneGrid\n', '\tflightTimezoneGridData    flightTimezoneGrid\n', 1)
s = s.replace('flightTimezoneGrid = flightTimezoneGrid{', 'flightTimezoneGridData = flightTimezoneGrid{')
s = s.replace('flightTimezoneGrid.Resolution', 'flightTimezoneGridData.Resolution')
s = s.replace('flightTimezoneGrid.Width', 'flightTimezoneGridData.Width')
s = s.replace('flightTimezoneGrid.Height', 'flightTimezoneGridData.Height')
s = s.replace('flightTimezoneGrid.Cells', 'flightTimezoneGridData.Cells')
s = s.replace('flightTimezoneGrid.Zones', 'flightTimezoneGridData.Zones')

if '\tflightTimezoneGrid        flightTimezoneGrid\n' in s:
    raise SystemExit('Timezone grid type/variable name collision still present')
if 'flightTimezoneGridData    flightTimezoneGrid' not in s:
    raise SystemExit('Timezone grid runtime instance was not found after rename')
if 'flightTimezoneGridData.Resolution' not in s:
    raise SystemExit('Timezone grid runtime references were not renamed')

p.write_text(s)
