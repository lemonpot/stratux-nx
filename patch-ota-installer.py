#!/usr/bin/env python3
"""Make Debian OTA installation safe inside Stratux's boot-time updater."""

import pathlib
import sys


root = pathlib.Path(sys.argv[1])

prestart = root / "debian/stratux-pre-start.sh"
source = prestart.read_text()

old_dpkg = 'if dpkg -i --force-depends "${UPDATE_TEMP_FILE}"; then'
new_dpkg = 'if NX_OTA_INSTALL=1 dpkg -i --force-depends "${UPDATE_TEMP_FILE}"; then'
logged_dpkg = 'if NX_OTA_INSTALL=1 dpkg -i --force-depends "${UPDATE_TEMP_FILE}" >>"${INSTALL_LOG}" 2>&1; then'
if new_dpkg not in source and logged_dpkg not in source:
    if old_dpkg not in source:
        raise SystemExit("Could not find boot-time dpkg command")
    source = source.replace(old_dpkg, new_dpkg, 1)

old_install_order = '''\t\t# Move the deb to /tmp and re-enable overlay BEFORE calling dpkg.
\t\t# The dpkg postinst runs 'systemctl daemon-reload && systemctl start stratux'
\t\t# which will kill this ExecStartPre process via daemon-reload. By cleaning up
\t\t# first the system is in a consistent state even if dpkg kills this script.
\t\tUPDATE_TEMP_FILE="/tmp/$(basename "${UPDATE_PACKAGE_FILE}")"
\t\tmv "${UPDATE_PACKAGE_FILE}" "${UPDATE_TEMP_FILE}"
\t\t/sbin/overlayctl enable
\t\tif NX_OTA_INSTALL=1 dpkg -i --force-depends "${UPDATE_TEMP_FILE}"; then'''
new_install_order = '''\t\t# Keep the overlay disabled while dpkg writes to the real ext4 root.
\t\t# NX_OTA_INSTALL prevents package scripts from stopping or restarting this
\t\t# service while dpkg is running from ExecStartPre.
\t\tUPDATE_TEMP_FILE="/tmp/$(basename "${UPDATE_PACKAGE_FILE}")"
\t\tmv "${UPDATE_PACKAGE_FILE}" "${UPDATE_TEMP_FILE}"
\t\tINSTALL_LOG="${TEMP_DIRECTORY}/last-install.log"
\t\tif NX_OTA_INSTALL=1 dpkg -i --force-depends "${UPDATE_TEMP_FILE}" >>"${INSTALL_LOG}" 2>&1; then'''
if new_install_order not in source:
    if old_install_order not in source:
        raise SystemExit("Could not keep overlay disabled during package install")
    source = source.replace(old_install_order, new_install_order, 1)

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

old_finish = '''\t\tif NX_OTA_INSTALL=1 dpkg -i --force-depends "${UPDATE_TEMP_FILE}" >>"${INSTALL_LOG}" 2>&1; then
\t\t\twLog "Package installed successfully."
\t\telse
\t\t\twLog "ERROR: dpkg failed to install ${UPDATE_TEMP_FILE}."
\t\tfi
\t\trm -f "${UPDATE_TEMP_FILE}"
\t\twLog "Finished. Rebooting..."
\t\treboot'''
new_finish = '''\t\tif NX_OTA_INSTALL=1 dpkg -i --force-depends "${UPDATE_TEMP_FILE}" >>"${INSTALL_LOG}" 2>&1; then
\t\t\twLog "Package installed successfully."
\t\t\trm -f "${UPDATE_TEMP_FILE}"
\t\telse
\t\t\twLog "ERROR: dpkg failed to install ${UPDATE_TEMP_FILE}. See ${INSTALL_LOG}."
\t\t\tmkdir -p "${TEMP_DIRECTORY}/failed"
\t\t\tmv "${UPDATE_TEMP_FILE}" "${TEMP_DIRECTORY}/failed/"
\t\tfi
\t\t/sbin/overlayctl enable
\t\tsync
\t\twLog "Finished. Rebooting..."
\t\treboot'''
if new_finish not in source:
    if old_finish in source:
        source = source.replace(old_finish, new_finish, 1)
    else:
        raise SystemExit("Could not find package install reboot")

prestart.write_text(source)

preinst = root / "debian/preinst.dpkg"
source = preinst.read_text()

preinst_marker = 'echo "Running pre-installation script and stopping the stratux service"\n'
preinst_guard = '''echo "Running pre-installation script and stopping the stratux service"
if [ "${NX_OTA_INSTALL:-0}" = "1" ]; then
\techo "Stratux NX OTA: keeping stratux.service active until dpkg completes"
\t# During an upgrade dpkg executes the previously installed prerm after this
\t# new preinst. Add the OTA guard to that legacy script once so it cannot stop
\t# the ExecStartPre process that is currently running dpkg.
\tLEGACY_PRERM="/var/lib/dpkg/info/stratux.prerm"
\tif [ -f "${LEGACY_PRERM}" ] && ! grep -q "NX_OTA_INSTALL" "${LEGACY_PRERM}"; then
\t\tPRERM_TEMP="$(mktemp)"
\t\t{
\t\t\thead -n 1 "${LEGACY_PRERM}"
\t\t\tprintf '%s\\n' 'if [ "${NX_OTA_INSTALL:-0}" = "1" ]; then exit 0; fi'
\t\t\ttail -n +2 "${LEGACY_PRERM}"
\t\t} >"${PRERM_TEMP}"
\t\tcat "${PRERM_TEMP}" >"${LEGACY_PRERM}"
\t\trm -f "${PRERM_TEMP}"
\t\tchmod 0755 "${LEGACY_PRERM}"
\tfi
\texit 0
fi
'''
if preinst_guard not in source:
    if preinst_marker not in source:
        raise SystemExit("Could not find pre-install service marker")
    source = source.replace(preinst_marker, preinst_guard, 1)
preinst.write_text(source)

prerm = root / "debian/prerm.dpkg"
source = prerm.read_text()

prerm_marker = 'echo "Running pre-removal script for the stratux service"\n'
prerm_guard = '''echo "Running pre-removal script for the stratux service"
if [ "${NX_OTA_INSTALL:-0}" = "1" ]; then
\techo "Stratux NX OTA: skipping service stop during package replacement"
\texit 0
fi
'''
if prerm_guard not in source:
    if prerm_marker not in source:
        raise SystemExit("Could not find pre-removal service marker")
    source = source.replace(prerm_marker, prerm_guard, 1)
prerm.write_text(source)

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
