#!/usr/bin/env python3
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
p = root / "web/plates/js/settings.js"
s = p.read_text()

# The modern setSettings wrapper is used by many fire-and-forget controls. Do not
# create an unhandled rejected native Promise when one of those saves fails.
s = s.replace('return Promise.reject(response);', 'return response;', 1)

# A transient settings GET failure should not visually reset every toggle to false.
old = '''\t\t}, function (response) {
\t\t\t$scope.rawSettings = "error getting settings";
\t\t\tfor (i = 0; i < toggles.length; i++) {
\t\t\t\tsettings[toggles[i]] = false;
\t\t\t}
\t\t});
\t}'''
new = '''\t\t}, function (response) {
\t\t\t$scope.rawSettings = "error getting settings";
\t\t\tif (window.StratuxUI) {
\t\t\t\twindow.StratuxUI.toast("Could not load Stratux settings. Existing values were left unchanged.", "error", 5000);
\t\t\t}
\t\t});
\t}'''
if old in s:
    s = s.replace(old, new, 1)
elif 'Existing values were left unchanged' not in s:
    raise SystemExit('Could not patch settings GET failure handling')

# System update: the old UI used a blocking browser alert and immediately redirected,
# which hid the useful status. Keep the user on the page and explain the next step.
old = '''\t\t}).success(function (data) {
\t\t\t$scope.uploading_update = false;
\t\t\talert("success. wait 5 minutes and refresh home page to verify new version.");
\t\t\twindow.location.replace("/");
\t\t\t$scope.$apply();
\t\t}).error(function (data) {
\t\t\t$scope.uploading_update = false;
\t\t\talert("error");
\t\t\t$scope.$apply();
\t\t});'''
new = '''\t\t}).success(function (data) {
\t\t\t$scope.uploading_update = false;
\t\t\tif (window.StratuxUI) {
\t\t\t\twindow.StratuxUI.toast("Update uploaded. Stratux is installing it now; wait about 5 minutes, reconnect if needed, then refresh Status to verify the version.", "success", 9000, "Update accepted");
\t\t\t} else {
\t\t\t\talert("Update uploaded. Wait about 5 minutes, then refresh Status to verify the version.");
\t\t\t}
\t\t\t$scope.$applyAsync();
\t\t}).error(function (data) {
\t\t\t$scope.uploading_update = false;
\t\t\tif (window.StratuxUI) {
\t\t\t\twindow.StratuxUI.toast("The system update upload failed. Check the file and try again.", "error", 6000, "Update failed");
\t\t\t} else {
\t\t\t\talert("System update upload failed.");
\t\t\t}
\t\t\t$scope.$applyAsync();
\t\t});'''
if old in s:
    s = s.replace(old, new, 1)
elif 'Update accepted' not in s:
    raise SystemExit('Could not patch system update feedback')

# Pong firmware upload gets the same non-blocking feedback pattern.
old = '''\t\t}).success(function (data) {
\t\t\t$scope.uploading_pong_update = false;
\t\t\talert("Success. Watch the LEDs on the Pong for successful programming");
\t\t\t$scope.$apply();
\t\t}).error(function (data) {
\t\t\t$scope.uploading_pong_update = false;
\t\t\talert("error");
\t\t\t$scope.$apply();
\t\t});'''
new = '''\t\t}).success(function (data) {
\t\t\t$scope.uploading_pong_update = false;
\t\t\tif (window.StratuxUI) {
\t\t\t\twindow.StratuxUI.toast("Pong firmware was sent. Watch the Pong LEDs for programming completion.", "success", 6500, "Pong update started");
\t\t\t} else {
\t\t\t\talert("Pong update started. Watch the LEDs for completion.");
\t\t\t}
\t\t\t$scope.$applyAsync();
\t\t}).error(function (data) {
\t\t\t$scope.uploading_pong_update = false;
\t\t\tif (window.StratuxUI) {
\t\t\t\twindow.StratuxUI.toast("Pong firmware upload failed. Check the update file and try again.", "error", 6000, "Pong update failed");
\t\t\t} else {
\t\t\t\talert("Pong firmware upload failed.");
\t\t\t}
\t\t\t$scope.$applyAsync();
\t\t});'''
if old in s:
    s = s.replace(old, new, 1)
elif 'Pong update started' not in s:
    raise SystemExit('Could not patch Pong update feedback')

p.write_text(s)
