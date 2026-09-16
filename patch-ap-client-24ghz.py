#!/usr/bin/env python3
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
path = root / "debian/wpa_supplicant.conf.template"
source = path.read_text()

old = '''network={
ssid="{{.SSID}}"
{{if .Password}}psk="{{.Password}}"{{else}}key_mgmt=NONE{{end}}
}'''
new = '''network={
ssid="{{.SSID}}"
# AP+Client shares one radio with the Stratux AP. Keep it on 2.4 GHz so
# 2.4 GHz-only avionics such as uAvionix AV-Link remain connected.
freq_list=2412 2417 2422 2427 2432 2437 2442 2447 2452 2457 2462 2467 2472
{{if .Password}}psk="{{.Password}}"{{else}}key_mgmt=NONE{{end}}
}'''

if new not in source:
    if old not in source:
        raise SystemExit("Could not find AP+Client network template")
    source = source.replace(old, new, 1)

path.write_text(source)
