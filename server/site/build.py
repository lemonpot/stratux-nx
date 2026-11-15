#!/usr/bin/env python3
"""Build the static stratuxnx.com site.

Usage: python3 build.py OUTPUT_DIR

Each file in pages/ starts with a one-line JSON comment:
<!--{"path": "/download/", "title": "...", "description": "...", "crumb": "Download"}-->
The rest of the file is placed inside layout.html.
"""
import datetime
import html
import json
import re
import shutil
import sys
from pathlib import Path

SITE = "https://www.stratuxnx.com"
ROOT = Path(__file__).resolve().parent


def breadcrumb(meta):
    if meta["path"] == "/":
        return "", None
    visible = f'<nav class="crumbs" aria-label="Breadcrumb"><a href="/">Stratux NX</a><span>/</span><span>{html.escape(meta["crumb"])}</span></nav>'
    data = {
        "@context": "https://schema.org",
        "@type": "BreadcrumbList",
        "itemListElement": [
            {"@type": "ListItem", "position": 1, "name": "Stratux NX", "item": SITE + "/"},
            {"@type": "ListItem", "position": 2, "name": meta["crumb"], "item": SITE + meta["path"]},
        ],
    }
    return visible, data


def faq_data(body):
    items = []
    for question, answer in re.findall(r"<details[^>]*><summary>(.*?)</summary>(.*?)</details>", body, re.S):
        text = html.unescape(re.sub(r"<[^>]+>", "", answer)).strip()
        items.append({"@type": "Question", "name": html.unescape(question).strip(),
                      "acceptedAnswer": {"@type": "Answer", "text": re.sub(r"\s+", " ", text)}})
    return {"@context": "https://schema.org", "@type": "FAQPage", "mainEntity": items}


def build(out):
    out = Path(out)
    if out.exists():
        shutil.rmtree(out)
    shutil.copytree(ROOT / "assets", out / "assets")
    layout = (ROOT / "layout.html").read_text()
    today = datetime.date.today().isoformat()
    version = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%d%H%M")
    urls = []

    for source in sorted((ROOT / "pages").glob("*.html")):
        text = source.read_text()
        match = re.match(r"<!--(\{.*?\})-->\n", text)
        if not match:
            sys.exit(f"{source.name}: missing metadata comment")
        meta = json.loads(match.group(1))
        body = text[match.end():]
        path = meta["path"]

        crumbs, crumb_data = breadcrumb(meta)
        body = body.replace("<!--crumbs-->", crumbs)
        scripts = re.findall(r'<script type="application/ld\+json">.*?</script>\n?', body, re.S)
        for script in scripts:
            body = body.replace(script, "")
        if crumb_data:
            scripts.append('<script type="application/ld+json">' + json.dumps(crumb_data) + "</script>\n")
        if meta.get("faq"):
            scripts.append('<script type="application/ld+json">' + json.dumps(faq_data(body)) + "</script>\n")

        page = layout
        for key, value in {
            "title": html.escape(meta["title"]),
            "description": html.escape(meta["description"]),
            "url": SITE + path,
            "version": version,
            "jsonld": "".join("  " + s.strip() + "\n" for s in scripts).rstrip("\n"),
            "content": body.strip("\n"),
        }.items():
            page = page.replace("{{" + key + "}}", value)
        if path != "/":
            page = page.replace(f'<a href="{path}">', f'<a href="{path}" aria-current="page">', 1)

        target = out / path.strip("/") / "index.html" if path != "/" else out / "index.html"
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(page)
        urls.append((path, meta.get("priority", "0.8")))

    sitemap = ['<?xml version="1.0" encoding="UTF-8"?>', '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">']
    for path, priority in sorted(urls, key=lambda u: (u[0] != "/", u[0])):
        sitemap.append(f"  <url><loc>{SITE}{path}</loc><lastmod>{today}</lastmod><priority>{priority}</priority></url>")
    sitemap.append("</urlset>")
    (out / "sitemap.xml").write_text("\n".join(sitemap) + "\n")
    (out / "robots.txt").write_text(f"User-agent: *\nAllow: /\n\nSitemap: {SITE}/sitemap.xml\n")
    print(f"Built {len(urls)} pages into {out}")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit("Usage: python3 build.py OUTPUT_DIR")
    build(sys.argv[1])
