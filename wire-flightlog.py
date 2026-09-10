#!/usr/bin/env python3
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

# Load Flight Log assets and add navigation.
p = root / 'web/index.html'
s = p.read_text()
if 'css/flightlog.css' not in s:
    marker = '<link rel="stylesheet" href="css/datausage_flows.css" />'
    if marker not in s:
        marker = '<link rel="stylesheet" href="css/main.css" />'
    s = s.replace(marker, marker + '\n\t<link rel="stylesheet" href="css/flightlog.css" />', 1)
if 'plates/js/flightlog.js' not in s:
    marker = '<script src="plates/js/datausage_flows.js"></script>'
    if marker not in s:
        marker = '<script src="plates/js/logs.js"></script>'
    s = s.replace(marker, marker + '\n\t<script src="plates/js/flightlog.js"></script>', 1)
if 'href="#/flightlog"' not in s:
    marker = '<a class="list-group-item" href="#/datausage"><i class="fa fa-bar-chart"></i> Internet Data <i class="fa fa-chevron-right pull-right"></i></a>'
    if marker not in s:
        raise SystemExit('Could not find Internet Data menu entry for Flight Log navigation')
    addition = marker + '\n\t\t\t\t\t<a class="list-group-item" href="#/flightlog"><i class="fa fa-plane"></i> Flight Log <i class="fa fa-chevron-right pull-right"></i></a>'
    s = s.replace(marker, addition, 1)
p.write_text(s)

# Angular route.
p = root / 'web/js/main.js'
s = p.read_text()
if ".state('flightlog'" not in s:
    marker = "\t\t.state('settings', {"
    route = "\t\t.state('flightlog', {\n\t\t\turl: '/flightlog',\n\t\t\ttemplateUrl: 'plates/flightlog.html',\n\t\t\tcontroller: 'FlightLogCtrl',\n\t\t\treloadOnSearch: false\n\t\t})\n"
    if marker not in s:
        raise SystemExit('Could not find settings route marker for Flight Log')
    s = s.replace(marker, route + marker, 1)
p.write_text(s)

# Standalone / iPad application cache.
p = root / 'web/stratux.appcache'
s = p.read_text()
entries = ['/plates/flightlog.html', '/plates/js/flightlog.js', '/css/flightlog.css']
if any(e not in s for e in entries):
    marker = '\nNETWORK:\n'
    if marker not in s:
        raise SystemExit('Could not find NETWORK section in appcache')
    missing = ''.join(e + '\n' for e in entries if e not in s)
    s = s.replace(marker, '\n' + missing + 'NETWORK:\n', 1)
p.write_text(s)

# Install compact worldwide airport + timezone data into the finished Raspberry Pi image.
p = root / 'image_build/stage2/10-stratux/01-run.sh'
s = p.read_text()
if '/opt/stratux/share/airports.csv' not in s:
    marker = 'install -m 644 files/motd "${ROOTFS_DIR}/etc/motd"'
    if marker not in s:
        raise SystemExit('Could not find motd install marker for airport database')
    addition = marker + '\n\n# Flight Log offline aviation databases\nmkdir -p "${ROOTFS_DIR}/opt/stratux/share"\ninstall -m 644 files/airports.csv "${ROOTFS_DIR}/opt/stratux/share/airports.csv"\ninstall -m 644 files/timezones.bin "${ROOTFS_DIR}/opt/stratux/share/timezones.bin"'
    s = s.replace(marker, addition, 1)
elif '/opt/stratux/share/timezones.bin' not in s:
    marker = 'install -m 644 files/airports.csv "${ROOTFS_DIR}/opt/stratux/share/airports.csv"'
    if marker not in s:
        raise SystemExit('Could not find airport database install line')
    s = s.replace(marker, marker + '\ninstall -m 644 files/timezones.bin "${ROOTFS_DIR}/opt/stratux/share/timezones.bin"', 1)
p.write_text(s)
