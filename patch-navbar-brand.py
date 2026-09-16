#!/usr/bin/env python3
import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1])

# The center navbar is the application brand, not the page title. Mobile
# Angular UI's ui-content-for/ui-yield-to mechanism keeps the last supplied
# value around, so a page like Internet Data or Flight Log could leave its
# name stuck in the navbar after navigating elsewhere. Keep a compact static
# name for mobile only; desktop already has the full sidebar brand.
p = root / "web/index.html"
s = p.read_text()
old = '<div class="navbar-brand navbar-brand-center" ui-yield-to="title">'
new = '<div class="navbar-brand navbar-brand-center">'
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not find Stratux navbar brand container')
# Replace any upstream brand content with the compact mobile name. Repeating
# the full logo in both the sidebar and navbar made the desktop shell noisy.
brand_link = re.search(
    r'(<div class="navbar-brand navbar-brand-center">\s*<a\s+href="#/">).*?(</a>)',
    s,
    flags=re.S,
)
if not brand_link:
    raise SystemExit('Could not find Stratux navbar brand link')
s = s[:brand_link.start()] + brand_link.group(1) + (
    '<span class="sx-navbar-name">Stratux NX</span>'
) + brand_link.group(2) + s[brand_link.end():]
p.write_text(s)

# Remove our custom page title publishers as well. They are no longer needed
# and removing them prevents stale title state in Mobile Angular UI itself.
for rel in ("web/plates/datausage.html", "web/plates/flightlog.html"):
    p = root / rel
    if not p.exists():
        continue
    s = p.read_text()
    s = re.sub(
        r'^\s*<div\s+ui-content-for="title"[^>]*>.*?</div>\s*',
        '',
        s,
        count=1,
        flags=re.S,
    )
    p.write_text(s)

# Hard validation: the finished shell must always own the visible Stratux
# navbar brand and custom pages must not publish into the title yield.
index = (root / "web/index.html").read_text()
if 'navbar-brand navbar-brand-center" ui-yield-to="title"' in index:
    raise SystemExit('Navbar is still using dynamic page-title yield')
if '>\n\t\t\t\t<a href="#/">' not in index and '<a href="#/">' not in index:
    raise SystemExit('Static Stratux home brand link is missing')
if 'class="sx-navbar-name">Stratux NX</span>' not in index:
    raise SystemExit('Compact Stratux NX navbar name is missing')
for rel in ("web/plates/datausage.html", "web/plates/flightlog.html"):
    p = root / rel
    if p.exists() and 'ui-content-for="title"' in p.read_text():
        raise SystemExit(rel + ' still publishes a sticky navbar title')

print('Navbar simplified for desktop with a compact mobile Stratux NX name.')
