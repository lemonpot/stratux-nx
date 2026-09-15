#!/usr/bin/env python3
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
path = root / "main/gen_gdl90.go"
source = path.read_text()

weather_call = "\tcaptureFlightWeatherText(wm)\n"
if weather_call not in source:
    marker = "\twm.LocaltimeReceived = stratuxClock.Time\n\n"
    if marker not in source:
        raise SystemExit("Could not find parsed weather registration marker")
    source = source.replace(marker, marker + weather_call + "\n", 1)

nexrad_call = "\t\t\t\tcaptureFlightNEXRADFrame(f.Product_id, len(f.NEXRAD))\n"
if nexrad_call not in source:
    marker = "\t\t\t\tUpdateUATStats(f.Product_id)\n"
    if marker not in source:
        raise SystemExit("Could not find UAT product statistics marker")
    source = source.replace(marker, marker + nexrad_call, 1)

gps_status_call = "\tcaptureFlightGPSStatusSnapshot(globalStatus.GPS_solution, globalStatus.GPS_satellites_locked)\n"
if gps_status_call not in source:
    marker = "\tglobalStatus.AHRS_LogFiles_Size = ahrsLogSize\n"
    if marker not in source:
        raise SystemExit("Could not find status snapshot marker")
    source = source.replace(marker, marker + gps_status_call, 1)

reception_call = "\tcaptureFlightReceptionSnapshot(UAT_messages_last_minute, ES_messages_last_minute)\n\n"
if reception_call not in source:
    marker = "\t// Update average signal strength over last minute for all ADSB towers.\n"
    if marker not in source:
        raise SystemExit("Could not find reception snapshot marker")
    source = source.replace(marker, reception_call + marker, 1)

path.write_text(source)

if source.count("captureFlightWeatherText(wm)") != 1:
    raise SystemExit("Weather capture hook must appear exactly once")
if source.count("captureFlightNEXRADFrame(f.Product_id, len(f.NEXRAD))") != 1:
    raise SystemExit("NEXRAD capture hook must appear exactly once")
if source.count("captureFlightGPSStatusSnapshot(globalStatus.GPS_solution, globalStatus.GPS_satellites_locked)") != 1:
    raise SystemExit("GPS status capture hook must appear exactly once")
if source.count("captureFlightReceptionSnapshot(UAT_messages_last_minute, ES_messages_last_minute)") != 1:
    raise SystemExit("Reception capture hook must appear exactly once")
