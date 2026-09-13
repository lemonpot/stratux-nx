#!/usr/bin/env python3
"""Remove HTML5 AppCache manifest, add no-cache meta tags, and bust
CSS/JS includes with a version query parameter derived from this
project's git HEAD.  Runs against the upstream Stratux web/index.html."""

import pathlib
import re
import subprocess
import sys

root = pathlib.Path(sys.argv[1])
script_dir = str(pathlib.Path(__file__).parent)

# Compute a short build hash from the custom project repo.
try:
    build_version = subprocess.check_output(
        ["git", "rev-parse", "--short=8", "HEAD"],
        cwd=script_dir).strip().decode()
except Exception:
    build_version = "dev"

p = root / "web/index.html"
s = p.read_text()

# 1. Strip the AppCache manifest attribute from <html>.
s = re.sub(r'(<html\b[^>]*?)\s+manifest="[^"]*"', r'\1', s)

# 2. Inject cache-busting meta tags right after <head> (or after the
#    first <meta charset> if present).
cache_meta = (
    '\n\t<meta http-equiv="Cache-Control" '
    'content="no-cache, no-store, must-revalidate">'
    '\n\t<meta http-equiv="Pragma" content="no-cache">'
    '\n\t<meta http-equiv="Expires" content="0">'
)
if 'http-equiv="Cache-Control"' not in s:
    head_tag = re.search(r'<head[^>]*>', s)
    if not head_tag:
        raise SystemExit("Could not find <head> in web/index.html")
    insert_pos = head_tag.end()
    s = s[:insert_pos] + cache_meta + s[insert_pos:]

# 3. Append ?v=BUILD_VERSION to every local CSS href and JS src.
vq = "?v=" + build_version

def bust(match):
    full = match.group(0)
    if "?v=" in full:
        return re.sub(r'\?v=[^"\'>\s]+', "?v=" + build_version, full)
    quote = match.group(0)[-1]  # trailing quote character
    return full[:-1] + vq + quote

s = re.sub(r'href="css/[^"]*"', bust, s)
s = re.sub(r'src="(plates/js|js)/[^"]*"', bust, s)

p.write_text(s)
print("Cache killed: manifest removed, meta tags added, %s appended to assets" % vq)
