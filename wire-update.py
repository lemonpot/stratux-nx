#!/usr/bin/env python3
"""Wire Stratux NX OTA update page into upstream."""
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

# --- index.html: CSS, JS and sidebar menu entry ---
p = root / "web/index.html"
s = p.read_text()

if 'css/update.css' not in s:
    marker = '<link rel="stylesheet" href="css/modern-ui.css" />'
    if marker not in s:
        raise SystemExit('Could not find modern-ui.css marker in web/index.html')
    s = s.replace(marker, marker + '\n\t<link rel="stylesheet" href="css/update.css" />', 1)

if 'plates/js/update.js' not in s:
    marker = '<script src="js/modern-ui.js"></script>'
    if marker not in s:
        raise SystemExit('Could not find modern-ui.js marker in web/index.html')
    s = s.replace(marker, '<script src="plates/js/update.js"></script>\n\t' + marker, 1)

if 'href="#/update"' not in s:
    marker = '<a class="list-group-item" href="#/settings"><i class="fa fa-cog"></i> Settings     <i class="fa fa-chevron-right pull-right"></i></a>'
    if marker not in s:
        raise SystemExit('Could not find Settings menu marker in web/index.html')
    addition = marker + '\n\t\t\t\t\t<a class="list-group-item" href="#/update"><i class="fa fa-cloud-download"></i> Update <i class="fa fa-chevron-right pull-right"></i></a>'
    s = s.replace(marker, addition, 1)

p.write_text(s)

# --- main.js: register Angular route ---
p = root / "web/js/main.js"
s = p.read_text()

if ".state('update'" not in s:
    marker = "\t\t.state('settings', {"
    route = "\t\t.state('update', {\n\t\t\turl: '/update',\n\t\t\ttemplateUrl: 'plates/update.html',\n\t\t\tcontroller: 'UpdateCtrl',\n\t\t\treloadOnSearch: false\n\t\t})\n"
    if marker not in s:
        raise SystemExit('Could not find settings route marker in web/js/main.js')
    s = s.replace(marker, route + marker, 1)

p.write_text(s)

# --- AppCache ---
p = root / "web/stratux.appcache"
s = p.read_text()
entries = ['/plates/update.html', '/plates/js/update.js', '/css/update.css']
if any(e not in s for e in entries):
    marker = '\nNETWORK:\n'
    if marker not in s:
        raise SystemExit('Could not find NETWORK section in stratux.appcache')
    missing = ''.join(e + '\n' for e in entries if e not in s)
    s = s.replace(marker, '\n' + missing + 'NETWORK:\n', 1)
p.write_text(s)

# --- Go: register HTTP handlers in managementinterface.go ---
p = root / "main/managementinterface.go"
s = p.read_text()

if '/update/check' not in s:
    marker = 'http.HandleFunc("/cageAHRS"'
    if marker not in s:
        raise SystemExit('Could not find cageAHRS handler in managementinterface.go')
    handlers = '''http.HandleFunc("/update/status", handleUpdateStatus)
\thttp.HandleFunc("/update/check", handleUpdateCheck)
\thttp.HandleFunc("/update/install", handleUpdateInstall)
\t'''
    s = s.replace(marker, handlers + marker, 1)

# --- Go: call initUpdater() from managementInterface() ---
if 'initUpdater()' not in s:
    # Find the start of the management interface goroutine or the ListenAndServe.
    marker2 = 'managementAddr :='
    if marker2 not in s:
        # Try alternative patterns.
        marker2 = 'log.Fatal(http.ListenAndServe'
        if marker2 in s:
            s = s.replace(marker2, 'go initUpdater()\n\t' + marker2, 1)
    else:
        s = s.replace(marker2, 'go initUpdater()\n\t' + marker2, 1)

p.write_text(s)

print('Update page + Go handlers wired into Stratux.')
