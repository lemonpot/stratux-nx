#!/usr/bin/env python3
import pathlib, sys
root = pathlib.Path(sys.argv[1])
p = root / 'main/flightlog.go'
s = p.read_text()
old = '''\tif !off.IsZero() && !takeoff.IsZero() && takeoff.After(off) {\n\t\trec.TaxiOutSeconds = int64(takeoff.Sub(off).Seconds())\n\t}'''
new = '''\tif !off.IsZero() {\n\t\ttaxiOutEnd := now\n\t\tif !takeoff.IsZero() {\n\t\t\ttaxiOutEnd = takeoff\n\t\t}\n\t\tif taxiOutEnd.After(off) {\n\t\t\trec.TaxiOutSeconds = int64(taxiOutEnd.Sub(off).Seconds())\n\t\t}\n\t}'''
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not patch live taxi-out duration')
p.write_text(s)
