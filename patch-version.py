#!/usr/bin/env python3
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
version = "2.1-beta1"

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
