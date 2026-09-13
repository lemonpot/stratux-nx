#!/usr/bin/env bash
set -euo pipefail

TARGET="${1:-stratux}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

python3 - "$TARGET" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])

# index.html: Internet Data CSS, JS and menu entry
p = root / "web/index.html"
s = p.read_text()
if 'css/datausage.css' not in s:
    s = s.replace('<link rel="stylesheet" href="css/main.css" />', '<link rel="stylesheet" href="css/main.css" />\n\t<link rel="stylesheet" href="css/datausage.css" />')
if 'css/datausage_flows.css' not in s:
    s = s.replace('<link rel="stylesheet" href="css/datausage.css" />', '<link rel="stylesheet" href="css/datausage.css" />\n\t<link rel="stylesheet" href="css/datausage_flows.css" />')
if 'css/modern-ui-extensions.css' not in s:
    marker = '<link rel="stylesheet" id="themeStylesheet" href="" />'
    if marker not in s:
        raise SystemExit('Could not find theme stylesheet marker for modern UI extensions')
    s = s.replace(marker, marker + '\n\t<link rel="stylesheet" href="css/modern-ui-extensions.css" />', 1)
if 'plates/js/datausage.js' not in s:
    s = s.replace('<script src="plates/js/logs.js"></script>', '<script src="plates/js/logs.js"></script>\n\t<script src="plates/js/datausage.js"></script>')
if 'plates/js/datausage_flows.js' not in s:
    s = s.replace('<script src="plates/js/datausage.js"></script>', '<script src="plates/js/datausage.js"></script>\n\t<script src="plates/js/datausage_flows.js"></script>')
if 'href="#/datausage"' not in s:
    marker = '<a class="list-group-item" href="#/logs"><i class="fa fa-file-text-o"></i> Logs     <i class="fa fa-chevron-right pull-right"></i></a>'
    addition = marker + '\n\t\t\t\t\t<a class="list-group-item" href="#/datausage"><i class="fa fa-bar-chart"></i> Internet Data <i class="fa fa-chevron-right pull-right"></i></a>'
    if marker not in s:
        raise SystemExit('Could not find Logs menu marker in web/index.html')
    s = s.replace(marker, addition, 1)
p.write_text(s)

# main.js: register Internet Data Angular route
p = root / "web/js/main.js"
s = p.read_text()
if ".state('datausage'" not in s:
    marker = "\t\t.state('settings', {"
    route = "\t\t.state('datausage', {\n\t\t\turl: '/datausage',\n\t\t\ttemplateUrl: 'plates/datausage.html',\n\t\t\tcontroller: 'DataUsageCtrl',\n\t\t\treloadOnSearch: false\n\t\t})\n"
    if marker not in s:
        raise SystemExit('Could not find settings route marker in web/js/main.js')
    s = s.replace(marker, route + marker, 1)
p.write_text(s)

# datausage.html: place live connection inspector before activity log.
p = root / "web/plates/datausage.html"
s = p.read_text()
include = "  <div ng-include=\"'plates/datausage_flows.html'\"></div>\n\n"
if 'datausage_flows.html' not in s:
    marker = '  <div class="data-panel">\n    <div class="data-panel-header">\n      <div>\n        <h3>Activity log</h3>'
    if marker not in s:
        raise SystemExit('Could not find Activity log panel marker in web/plates/datausage.html')
    s = s.replace(marker, include + marker, 1)
p.write_text(s)

# AppCache: Internet Data and modern shared assets.
p = root / "web/stratux.appcache"
s = p.read_text()
entries = [
    '/plates/datausage.html',
    '/plates/js/datausage.js',
    '/css/datausage.css',
    '/plates/datausage_flows.html',
    '/plates/js/datausage_flows.js',
    '/css/datausage_flows.css',
    '/css/modern-ui-extensions.css',
]
if any(e not in s for e in entries):
    marker = '\nNETWORK:\n'
    if marker not in s:
        raise SystemExit('Could not find NETWORK section in stratux.appcache')
    missing = ''.join(e + '\n' for e in entries if e not in s)
    s = s.replace(marker, '\n' + missing + 'NETWORK:\n', 1)
p.write_text(s)

# The live-flow inspector uses conntrack to enumerate and kill selected 5-tuples.
p = root / "image_build/stage2/10-stratux/01-run.sh"
s = p.read_text()
old = 'apt install --yes dnsmasq ifplugd iptables'
new = 'apt install --yes dnsmasq ifplugd iptables conntrack avahi-utils'
if new not in s:
    if old not in s:
        raise SystemExit('Could not find Stratux runtime package install line')
    s = s.replace(old, new, 1)

# Conntrack accounting must be enabled before client connections are created.
if '90-stratux-data-monitor.conf' not in s:
    s += '''\n\n# Internet Data monitor: keep byte/packet counters on new conntrack entries.\nmkdir -p ${ROOTFS_DIR}/etc/sysctl.d\necho "net.netfilter.nf_conntrack_acct=1" > ${ROOTFS_DIR}/etc/sysctl.d/90-stratux-data-monitor.conf\n'''
p.write_text(s)
PY

# Custom build identity shown by Stratux and used in the Debian package name.
python3 "$SCRIPT_DIR/patch-version.py" "$TARGET"

# Internet usage backend, service ranking, unrestricted device policies and UX.
python3 "$SCRIPT_DIR/patch-flow-backend.py" "$TARGET"
python3 "$SCRIPT_DIR/patch-data-policies.py" "$TARGET"
python3 "$SCRIPT_DIR/patch-data-policy-style.py" "$TARGET"

# Shared Stratux UX modernization, map enhancements and settings workflow fixes.
python3 "$SCRIPT_DIR/patch-modern-ui.py" "$TARGET"
python3 "$SCRIPT_DIR/patch-map-ui.py" "$TARGET"
python3 "$SCRIPT_DIR/patch-ux-audit.py" "$TARGET"
python3 "$SCRIPT_DIR/patch-navbar-brand.py" "$TARGET"

# Selectable AHRS. The upstream SimpleAHRS is preserved as "Old Stratux AHRS";
# Adaptive AHRS v2 runs in parallel and can be selected from Settings.
python3 "$SCRIPT_DIR/patch-ahrs-v2.py" "$TARGET"

# Flight Log: generate complete offline airport + GPS timezone databases, wire
# route/menu/assets and harden automatic flight detection for Raspberry Pi/iPad.
python3 "$SCRIPT_DIR/build-airports.py" "$TARGET"
python3 "$SCRIPT_DIR/build-timezones.py" "$TARGET"
python3 "$SCRIPT_DIR/wire-flightlog.py" "$TARGET"
python3 "$SCRIPT_DIR/patch-flightlog-hardening.py" "$TARGET"
python3 "$SCRIPT_DIR/patch-flightlog-live-timing.py" "$TARGET"
python3 "$SCRIPT_DIR/patch-flightlog-timezone.py" "$TARGET"
python3 "$SCRIPT_DIR/patch-flightlog-timezone-compilefix.py" "$TARGET"

# OTA update page and Go backend handlers.
python3 "$SCRIPT_DIR/wire-update.py" "$TARGET"

# Run navbar-brand cleanup after Flight Log wiring too, so no custom page can
# publish a sticky value into Mobile Angular UI's title yield.
python3 "$SCRIPT_DIR/patch-navbar-brand.py" "$TARGET"

# Stratux still uses the legacy HTML AppCache. Make the manifest change whenever
# this customization branch changes so browsers cannot keep older UI assets.
CACHE_VERSION="$(git -C "$SCRIPT_DIR" rev-parse --short=12 HEAD 2>/dev/null || date +%s)"
sed -i '/^# Internet Data build:/d' "$TARGET/web/stratux.appcache"
printf '\n# Internet Data build: %s\n' "$CACHE_VERSION" >> "$TARGET/web/stratux.appcache"

if command -v gofmt >/dev/null 2>&1; then
  gofmt -w "$TARGET/main/datausage.go" "$TARGET/main/datausage_wire.go" "$TARGET/main/datausage_flows.go" "$TARGET/main/flightlog.go" "$TARGET/main/ahrs_v2.go"
fi

echo "Stratux 2.1-beta1: Internet Data, unrestricted-device policies, global modern UI, settings UX, fixed Stratux navbar brand, selectable Old Stratux AHRS / Adaptive AHRS v2, automatic Flight Log, and GPS-resolved local timezone wired into Stratux source."
