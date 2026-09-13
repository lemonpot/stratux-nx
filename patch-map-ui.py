#!/usr/bin/env python3
"""Wire Stratux NX map and page UI enhancements into upstream."""
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

# --- index.html: load map CSS, pages CSS and map JS ---
p = root / "web/index.html"
s = p.read_text()

if 'css/map-nx.css' not in s:
    marker = '<link rel="stylesheet" href="css/modern-ui.css" />'
    if marker not in s:
        raise SystemExit('Could not find modern-ui.css marker in web/index.html')
    s = s.replace(marker, marker + '\n\t<link rel="stylesheet" href="css/map-nx.css" />', 1)

if 'css/pages-nx.css' not in s:
    marker = '<link rel="stylesheet" href="css/map-nx.css" />'
    if marker not in s:
        raise SystemExit('Could not find map-nx.css marker in web/index.html')
    s = s.replace(marker, marker + '\n\t<link rel="stylesheet" href="css/pages-nx.css" />', 1)

if 'js/map-nx.js' not in s:
    marker = '<script src="js/modern-ui.js"></script>'
    if marker not in s:
        raise SystemExit('Could not find modern-ui.js marker in web/index.html')
    s = s.replace(marker, marker + '\n\t<script src="js/map-nx.js"></script>', 1)

p.write_text(s)

# --- AppCache: register new assets ---
p = root / "web/stratux.appcache"
s = p.read_text()
entries = ['/css/map-nx.css', '/css/pages-nx.css', '/js/map-nx.js']
if any(e not in s for e in entries):
    marker = '\nNETWORK:\n'
    if marker not in s:
        raise SystemExit('Could not find NETWORK section in stratux.appcache')
    missing = ''.join(e + '\n' for e in entries if e not in s)
    s = s.replace(marker, '\n' + missing + 'NETWORK:\n', 1)
p.write_text(s)

print('Map and page NX UI enhancements wired into index and appcache.')
