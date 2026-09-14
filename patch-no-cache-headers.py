#!/usr/bin/env python3
"""Force no-cache headers on all HTTP responses from the Stratux web server."""
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
p = root / "main/managementinterface.go"
s = p.read_text()

# Replace the cached default server with a no-cache version
old = 'w.Header().Set("Cache-Control", "max-age=360") // 5 min, so that if user installs update, he will revalidate soon enough'
new = 'w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")\n\tw.Header().Set("Pragma", "no-cache")\n\tw.Header().Set("Expires", "0")'

if old in s:
    s = s.replace(old, new, 1)
elif 'no-store' not in s:
    raise SystemExit("Could not find Cache-Control header in defaultServer")

p.write_text(s)
print("HTTP no-cache headers forced on all web responses.")
