#!/usr/bin/env python3
"""Preserve Stratux NX and third-party notices in distributed Debian packages."""

import pathlib
import shutil
import subprocess
import sys


root = pathlib.Path(sys.argv[1]).resolve()
project = pathlib.Path(__file__).parent.resolve()


def commit(directory: pathlib.Path) -> str:
    return subprocess.check_output(
        ["git", "rev-parse", "HEAD"], cwd=directory, text=True
    ).strip()


upstream_commit = commit(root)
nx_commit = commit(project)

debian = root / "debian"
shutil.copyfile(project / "LICENSE", debian / "stratux-nx-license")

notices = (project / "THIRD_PARTY_NOTICES.md").read_text()
notices += (
    "\n## Exact build inputs\n\n"
    f"- Stratux upstream commit: `{upstream_commit}`\n"
    f"- Stratux NX customization commit: `{nx_commit}`\n"
)
(debian / "STRATUX-NX-NOTICES.md").write_text(notices)

control_path = debian / "control.dpkg"
control = control_path.read_text()
control = control.replace(
    "Maintainer: Admin admin@stratux.me",
    "Maintainer: Lemonpot <tomas@lemonpot.com>",
)
control = control.replace(
    "Vcs-Git: git@github.com:stratux/stratux.git",
    "Vcs-Git: https://github.com/lemonpot/stratux-nx.git",
)
control = control.replace(
    "Description: A low cost ADS-B receivers for pilots.",
    "Homepage: https://github.com/lemonpot/stratux-nx\n"
    "Description: Stratux NX ADS-B and flight data receiver software.",
)
if "Maintainer: Lemonpot" not in control or "Homepage: https://github.com/lemonpot/stratux-nx" not in control:
    raise SystemExit("Could not update Debian package identity")
control_path.write_text(control)

makefile_path = root / "Makefile"
makefile = makefile_path.read_text()
license_install = """\t# Install copyright, source and third-party license notices
\tmkdir -p $(DEBPKG_BASE)/usr/share/doc/stratux/licenses
\tcp -f debian/copyright $(DEBPKG_BASE)/usr/share/doc/stratux/copyright
\tpython3 scripts/collect-licenses.py --source . --output $(DEBPKG_BASE)/usr/share/doc/stratux/licenses
"""
if "scripts/collect-licenses.py --source ." not in makefile:
    marker = "\tmkdir -p $(DEBPKG_BASE)/lib/systemd/system/\n"
    if marker not in makefile:
        raise SystemExit("Could not find Debian package directory marker")
    makefile = makefile.replace(marker, marker + license_install, 1)
makefile_path.write_text(makefile)

for required in (
    debian / "copyright",
    debian / "stratux-nx-license",
    debian / "STRATUX-NX-NOTICES.md",
    root / "scripts/collect-licenses.py",
):
    if not required.is_file():
        raise SystemExit(f"Required licensing file is missing: {required}")

print("Stratux NX licenses, package notices and exact source revisions wired.")
