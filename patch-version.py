#!/usr/bin/env python3
import pathlib
import subprocess
import sys

root = pathlib.Path(sys.argv[1])

# Build a unique version per commit so OTA always detects changes.
# Format: 2.1-beta1.YYYYMMDD.shortsha
base = "2.1-beta1"
try:
    date_str = subprocess.check_output(
        ["git", "log", "-1", "--format=%cd", "--date=format:%Y%m%d"],
        cwd=str(pathlib.Path(__file__).parent), text=True).strip()
    sha_str = subprocess.check_output(
        ["git", "rev-parse", "--short=7", "HEAD"],
        cwd=str(pathlib.Path(__file__).parent), text=True).strip()
    version = "%s.%s.%s" % (base, date_str, sha_str)
except Exception:
    version = base

p = root / "Makefile"
s = p.read_text()
old = "VERSIONSTR := $(shell ./scripts/getversion.sh)"
new = "VERSIONSTR := %s" % version
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit("Could not override Stratux VERSIONSTR")
p.write_text(s)

print("Stratux custom version set to %s" % version)
