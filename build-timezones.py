#!/usr/bin/env python3
import array
import csv
import json
import math
import pathlib
import shutil
import subprocess
import sys
import tempfile

root = pathlib.Path(sys.argv[1])
resolution = 0.25
width = int(round(360.0 / resolution))
height = int(round(180.0 / resolution))


def load_timezonefinder():
    try:
        from timezonefinder import TimezoneFinder
        return TimezoneFinder
    except Exception:
        pass

    deps = pathlib.Path(tempfile.mkdtemp(prefix='stratux-timezonefinder-'))
    print('Installing build-only timezonefinder dependency...')
    subprocess.check_call([
        sys.executable, '-m', 'pip', 'install', '--quiet', '--disable-pip-version-check',
        '--target', str(deps), 'timezonefinder>=7,<9'
    ])
    sys.path.insert(0, str(deps))
    from timezonefinder import TimezoneFinder
    return TimezoneFinder


TimezoneFinder = load_timezonefinder()
tf = TimezoneFinder(in_memory=True)


def ocean_fallback(lon):
    # IANA Etc/GMT signs are intentionally reversed: Etc/GMT+5 is UTC-05:00.
    offset = int(math.floor((lon + 7.5) / 15.0))
    offset = max(-12, min(12, offset))
    if offset == 0:
        return 'Etc/UTC'
    if offset > 0:
        return 'Etc/GMT-%d' % offset
    return 'Etc/GMT+%d' % (-offset)


def zone_at(lat, lon):
    try:
        zone = tf.timezone_at(lng=float(lon), lat=float(lat))
    except Exception:
        zone = None
    return zone or ocean_fallback(float(lon))


# Add an exact IANA timezone to every airport/facility. This gives takeoff and
# landing airports exact local-time context even when they sit near a timezone border.
airports_path = root / 'image_build/stage2/10-stratux/files/airports.csv'
if not airports_path.exists():
    raise SystemExit('airports.csv must be generated before build-timezones.py')

with airports_path.open('r', newline='', encoding='utf-8') as f:
    rows = list(csv.reader(f))
if not rows or len(rows[0]) < 8:
    raise SystemExit('Unexpected airports.csv format')

header = rows[0]
if 'timezone' not in header:
    header = header + ['timezone']
    enriched = [header]
    total = len(rows) - 1
    for idx, row in enumerate(rows[1:], 1):
        if len(row) < 8:
            continue
        try:
            tz = zone_at(float(row[2]), float(row[3]))
        except Exception:
            tz = ''
        enriched.append(row + [tz])
        if idx % 20000 == 0:
            print('Resolved timezone for %d/%d airport records...' % (idx, total))
    rows = enriched
    with airports_path.open('w', newline='', encoding='utf-8') as f:
        csv.writer(f).writerows(rows)

czba = next((r for r in rows[1:] if r and r[0] == 'CZBA'), None)
if not czba or len(czba) < 9 or czba[8] != 'America/Toronto':
    raise SystemExit('Timezone sanity check failed for CZBA: %r' % (czba[8] if czba and len(czba) > 8 else None))

# Global 0.25-degree GPS -> IANA timezone grid. Runtime lookup is O(1) and the
# resulting binary is only ~2 MB, so the Raspberry Pi does not need Internet or
# heavyweight polygon libraries. Exact airport timezone above overrides this grid
# when parked/taking off/landing near a known field.
print('Building global %.2f-degree offline timezone grid (%d x %d)...' % (resolution, width, height))
zone_ids = {}
zones = []
cells = array.array('H')

for y in range(height):
    lat = 90.0 - (y + 0.5) * resolution
    for x in range(width):
        lon = -180.0 + (x + 0.5) * resolution
        zone = zone_at(lat, lon)
        zid = zone_ids.get(zone)
        if zid is None:
            zid = len(zones)
            if zid >= 65535:
                raise SystemExit('Too many timezone IDs for uint16 grid')
            zone_ids[zone] = zid
            zones.append(zone)
        cells.append(zid)
    if y % 90 == 0:
        print('Timezone grid: %d%%' % int((y * 100) / max(1, height)))

if sys.byteorder != 'little':
    cells.byteswap()

out = root / 'image_build/stage2/10-stratux/files/timezones.bin'
out.parent.mkdir(parents=True, exist_ok=True)
meta = {
    'resolution': resolution,
    'width': width,
    'height': height,
    'zones': zones,
    'source': 'timezonefinder / timezone-boundary data',
}
with out.open('wb') as f:
    f.write(b'SXTZ1\n')
    f.write((json.dumps(meta, separators=(',', ':')) + '\n').encode('utf-8'))
    f.write(cells.tobytes())

expected = width * height * 2
actual_payload = len(cells) * 2
if actual_payload != expected:
    raise SystemExit('Timezone grid size mismatch')

print('Wrote %s with %d cells, %d IANA zones, %.2f MB payload' % (
    out, len(cells), len(zones), actual_payload / 1024.0 / 1024.0))
