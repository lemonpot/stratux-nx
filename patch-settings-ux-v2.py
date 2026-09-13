#!/usr/bin/env python3
"""Improve Settings page UX: rename Commands → System, clarify manual upload,
add busy overlay on reboot."""
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

# ---------------------------------------------------------------------------
# Patch settings.html
# ---------------------------------------------------------------------------
p = root / "web/plates/settings.html"
s = p.read_text()

# 1. Rename "Commands" panel heading → "System"
old_heading = '<div class="panel-heading">Commands</div>'
new_heading = '<div class="panel-heading">System</div>'
if old_heading in s:
    s = s.replace(old_heading, new_heading, 1)

# 2. Clarify the manual file-upload section.
#    Replace the old "Click to select System Update file" fake-btn text and
#    add a descriptive "Manual update" label with a helper note.
old_upload = '                    <!-- Upload. Temporary. -->\n                    <div class="col-xs-12">'
new_upload = (
    '                    <!-- Manual .deb upload -->\n'
    '                    <div class="col-xs-12">\n'
    '                        <p style="margin:0 0 4px;font-weight:700;font-size:13px;">Manual update</p>\n'
    '                        <p style="margin:0 0 8px;font-size:11px;color:#888;">Upload a .deb file to update without internet</p>\n'
    '                    </div>\n'
    '                    <div class="col-xs-12">'
)
if old_upload in s:
    s = s.replace(old_upload, new_upload, 1)

old_btn_text = '<span class="fake-btn fake-btn-block">Click to select System Update file</span>'
new_btn_text = '<span class="fake-btn fake-btn-block">Choose .deb update file</span>'
if old_btn_text in s:
    s = s.replace(old_btn_text, new_btn_text, 1)

p.write_text(s)

# ---------------------------------------------------------------------------
# Patch settings.js -- show busy overlay on reboot
# ---------------------------------------------------------------------------
p = root / "web/plates/js/settings.js"
s = p.read_text()

# Add a StratuxUI.showBusy call at the start of postReboot so users see a
# progress indicator immediately after confirming the reboot modal.
old_reboot = '''\t$scope.postReboot = function () {
\t\t$window.location.href = "/";'''
new_reboot = '''\t$scope.postReboot = function () {
\t\tif (window.StratuxUI) window.StratuxUI.showBusy('Rebooting\\u2026', 'Please wait while Stratux restarts.');
\t\t$window.location.href = "/";'''
if old_reboot in s and 'showBusy' not in s:
    s = s.replace(old_reboot, new_reboot, 1)

p.write_text(s)

print("Settings UX v2 applied.")
