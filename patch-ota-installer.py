#!/usr/bin/env python3
"""Make Debian OTA installation safe inside Stratux's boot-time updater."""

import pathlib
import sys


root = pathlib.Path(sys.argv[1])

prestart = root / "debian/stratux-pre-start.sh"
source = prestart.read_text()

old_dpkg = 'if dpkg -i --force-depends "${UPDATE_TEMP_FILE}"; then'
new_dpkg = 'if NX_OTA_INSTALL=1 dpkg -i --force-depends "${UPDATE_TEMP_FILE}"; then'
if new_dpkg not in source:
    if old_dpkg not in source:
        raise SystemExit("Could not find boot-time dpkg command")
    source = source.replace(old_dpkg, new_dpkg, 1)

old_stage_reboot = '''/sbin/overlayctl disable
\t\t\twLog "Package staged. Rebooting to install on bare ext4..."
\t\t\treboot'''
new_stage_reboot = '''/sbin/overlayctl disable
\t\t\tsync
\t\t\twLog "Package staged. Rebooting to install on bare ext4..."
\t\t\treboot'''
if new_stage_reboot not in source:
    if old_stage_reboot not in source:
        raise SystemExit("Could not find package staging reboot")
    source = source.replace(old_stage_reboot, new_stage_reboot, 1)

old_finish = '''rm -f "${UPDATE_TEMP_FILE}"
\t\twLog "Finished. Rebooting..."
\t\treboot'''
new_finish = '''rm -f "${UPDATE_TEMP_FILE}"
\t\tsync
\t\twLog "Finished. Rebooting..."
\t\treboot'''
if new_finish not in source:
    if old_finish not in source:
        raise SystemExit("Could not find package install reboot")
    source = source.replace(old_finish, new_finish, 1)

prestart.write_text(source)

postinst = root / "debian/postinst.dpkg"
source = postinst.read_text()

start = 'systemctl daemon-reload\n'
end = 'fi\nexit 0\n'
if 'NX_OTA_INSTALL' not in source:
    start_at = source.find(start)
    end_at = source.rfind(end)
    if start_at < 0 or end_at < 0:
        raise SystemExit("Could not find post-install service block")
    service_block = source[start_at:end_at + len("fi\n")]
    guarded = (
        '# Boot-time OTA runs from stratux.service ExecStartPre. Restarting the\n'
        '# same service here interrupts dpkg and can leave the update incomplete.\n'
        'if [ "${NX_OTA_INSTALL:-0}" != "1" ]; then\n'
        + "".join("\t" + line if line.strip() else line for line in service_block.splitlines(True))
        + 'fi\n'
    )
    source = source[:start_at] + guarded + source[end_at + len("fi\n"):]

postinst.write_text(source)

print("Boot-time OTA installer hardened.")
