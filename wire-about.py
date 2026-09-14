#!/usr/bin/env python3
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

index_path = root / "web/index.html"
index = index_path.read_text()

if 'css/about.css' not in index:
    marker = '<link rel="stylesheet" id="themeStylesheet" href="" />'
    if marker not in index:
        raise SystemExit("Could not find stylesheet marker for About")
    index = index.replace(
        marker,
        '<link rel="stylesheet" href="css/about.css" />\n\t' + marker,
        1,
    )

menu_header = '<h3 class="scrollable-header app-name">Menu</h3>'
sidebar_brand = '''<div class="scrollable-header app-name sx-sidebar-brand" aria-label="Stratux NX">
				<img class="sx-sidebar-brand-light" src="img/logo-stratux-light.png" alt="Stratux NX">
				<img class="sx-sidebar-brand-dark" src="img/logo-stratux-dark.png" alt="Stratux NX">
			</div>'''
if menu_header in index:
    index = index.replace(menu_header, sidebar_brand, 1)
elif 'sx-sidebar-brand' not in index:
    raise SystemExit("Could not find sidebar header for Stratux NX logo")

if 'href="#/about"' not in index:
    marker = '<a class="list-group-item" href="#/map"><i class="fa fa-map"></i>    Map <i class="fa fa-chevron-right pull-right"></i></a>'
    if marker not in index:
        raise SystemExit("Could not find Map menu item for About")
    about_link = '<a class="list-group-item" href="#/about"><i class="fa fa-info-circle"></i> About <i class="fa fa-chevron-right pull-right"></i></a>'
    index = index.replace(marker, marker + '\n\t\t\t\t\t' + about_link, 1)

index_path.write_text(index)

main_path = root / "web/js/main.js"
main = main_path.read_text()
if ".state('about'" not in main:
    marker = "\t\t.state('settings', {"
    if marker not in main:
        raise SystemExit("Could not find Settings route marker for About")
    route = (
        "\t\t.state('about', {\n"
        "\t\t\turl: '/about',\n"
        "\t\t\ttemplateUrl: 'plates/about.html',\n"
        "\t\t\treloadOnSearch: false\n"
        "\t\t})\n"
    )
    main = main.replace(marker, route + marker, 1)
main_path.write_text(main)

required = [
    root / "web/plates/about.html",
    root / "web/css/about.css",
    root / "web/img/logo-stratux-light.png",
    root / "web/img/logo-stratux-dark.png",
    root / "web/img/logo-android3.png",
    root / "web/img/logo-apple3.png",
]
missing = [str(path.relative_to(root)) for path in required if not path.is_file()]
if missing:
    raise SystemExit("Missing About/branding assets: " + ", ".join(missing))

print("About page, themed Stratux NX logos and application icons wired.")
