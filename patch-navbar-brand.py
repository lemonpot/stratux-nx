#!/usr/bin/env python3
import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1])

# The center navbar is the application brand, not the page title. Mobile
# Angular UI's ui-content-for/ui-yield-to mechanism keeps the last supplied
# value around, so a page like Internet Data or Flight Log could leave its
# name stuck in the navbar after navigating elsewhere. Keep the navbar brand
# static and put page names only inside each page's own hero/header.
p = root / "web/index.html"
s = p.read_text()
old = '<div class="navbar-brand navbar-brand-center" ui-yield-to="title">'
new = '<div class="navbar-brand navbar-brand-center">'
if old in s:
    s = s.replace(old, new, 1)
elif new not in s:
    raise SystemExit('Could not find Stratux navbar brand container')
# Replace any upstream brand content with the NX icon and name. Mobile Angular
# UI may otherwise turn the original image into a plain-text page title.
brand_link = re.search(
    r'(<div class="navbar-brand navbar-brand-center">\s*<a\s+href="#/">).*?(</a>)',
    s,
    flags=re.S,
)
if not brand_link:
    raise SystemExit('Could not find Stratux navbar brand link')
s = s[:brand_link.start()] + brand_link.group(1) + (
    '<img class="sx-brand-logo" src="img/logo-nx.png" alt=""> '
    'Stratux NX'
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
if 'class="sx-brand-logo" src="img/logo-nx.png"' not in index:
    raise SystemExit('Static Stratux NX navbar logo is missing')
for rel in ("web/plates/datausage.html", "web/plates/flightlog.html"):
    p = root / rel
    if p.exists() and 'ui-content-for="title"' in p.read_text():
        raise SystemExit(rel + ' still publishes a sticky navbar title')

print('Navbar brand set to Stratux NX with logo; page titles remain inside page content.')
