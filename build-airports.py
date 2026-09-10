#!/usr/bin/env python3
import csv
import io
import pathlib
import sys
import urllib.request

root = pathlib.Path(sys.argv[1])
url = 'https://raw.githubusercontent.com/davidmegginson/ourairports-data/main/airports.csv'
print('Downloading complete current OurAirports global airport database...')
with urllib.request.urlopen(url, timeout=45) as response:
    raw = response.read().decode('utf-8-sig')

reader = csv.DictReader(io.StringIO(raw))
rows = []
seen = set()
type_counts = {}
for row in reader:
    # Keep every aviation facility in OurAirports that has usable coordinates.
    # This includes active land airports, heliports, seaplane bases, balloonports
    # and closed airports. The complete database stays available offline; the
    # automatic fixed-wing flight detector applies its own suitability filter.
    lat = (row.get('latitude_deg') or '').strip()
    lon = (row.get('longitude_deg') or '').strip()
    if not lat or not lon:
        continue

    ident = (row.get('ident') or '').strip().upper()
    icao = (row.get('icao_code') or '').strip().upper()
    gps = (row.get('gps_code') or '').strip().upper()
    local = (row.get('local_code') or '').strip().upper()
    iata = (row.get('iata_code') or '').strip().upper()
    code = icao or gps or ident or local or iata
    if not code:
        continue

    facility_type = (row.get('type') or 'unknown').strip() or 'unknown'
    key = ((row.get('id') or '').strip(), lat, lon)
    if key in seen:
        continue
    seen.add(key)
    type_counts[facility_type] = type_counts.get(facility_type, 0) + 1

    rows.append([
        code,
        (row.get('name') or code).strip(),
        lat,
        lon,
        (row.get('elevation_ft') or '0').strip() or '0',
        facility_type,
        (row.get('municipality') or '').strip(),
        (row.get('iso_country') or '').strip(),
    ])

rows.sort(key=lambda r: (r[0], r[2], r[3]))
out = root / 'image_build/stage2/10-stratux/files/airports.csv'
out.parent.mkdir(parents=True, exist_ok=True)
with out.open('w', newline='', encoding='utf-8') as f:
    writer = csv.writer(f)
    writer.writerow(['code', 'name', 'latitude_deg', 'longitude_deg', 'elevation_ft', 'type', 'municipality', 'country'])
    writer.writerows(rows)

if not any(r[0] == 'CZBA' for r in rows):
    raise SystemExit('Airport database sanity check failed: CZBA is missing')
if len(rows) < 80000:
    raise SystemExit('Airport database sanity check failed: expected at least 80,000 global facilities, got %d' % len(rows))
for required_type in ('small_airport', 'medium_airport', 'large_airport', 'heliport', 'seaplane_base'):
    if type_counts.get(required_type, 0) == 0:
        raise SystemExit('Airport database sanity check failed: missing facility type %s' % required_type)
# OurAirports has historically represented closed fields as either "closed" or
# "closed_airport" in different exports. Accept either spelling while keeping
# the full database untouched.
if type_counts.get('closed_airport', 0) == 0 and type_counts.get('closed', 0) == 0:
    raise SystemExit('Airport database sanity check failed: no closed facilities found')

print('Wrote %d global aviation facilities to %s' % (len(rows), out))
print('Facility types: ' + ', '.join('%s=%d' % item for item in sorted(type_counts.items())))
