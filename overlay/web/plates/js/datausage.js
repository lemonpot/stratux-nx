appControllers.controller('DataUsageCtrl', function($scope, $http, $interval) {
    $scope.data = {
        TotalBytes: 0,
        UploadBytes: 0,
        DownloadBytes: 0,
        UploadBps: 0,
        DownloadBps: 0,
        SessionSeconds: 0,
        Clients: [],
        RecentEvents: [],
        WANOnline: false,
        MonitoringActive: false
    };

    $scope.settings = {
        WarningMB: 250,
        AutoBlockMB: 500,
        SessionLimitMB: 1000,
        AutoBlockEnabled: true
    };

    $scope.errorMessage = '';
    $scope.savingSettings = false;
    $scope.protectionPending = false;
    $scope.resetPending = false;
    $scope.deviceRows = [];
    $scope.limitsDraft = null;
    $scope.deviceEdit = null;
    $scope.showFlows = false;
    var settingsLoaded = false;
    var devicePolicyDrafts = {};

    function deviceKey(client) {
        return String((client && (client.MAC || client.IP)) || '');
    }

    function showFeedback(message, type) {
        if (window.StratuxUI && window.StratuxUI.toast) {
            window.StratuxUI.toast(message, type || 'success', 3000);
        }
    }

    $scope.mb = function(value) {
        return Number(value || 0) * 1024 * 1024;
    };

    $scope.formatBytes = function(bytes) {
        bytes = Number(bytes || 0);
        if (bytes < 1024) return bytes.toFixed(0) + ' B';
        if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
        if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
        return (bytes / (1024 * 1024 * 1024)).toFixed(2) + ' GB';
    };

    $scope.formatMB = function(mb) {
        mb = Number(mb || 0);
        if (mb >= 1024 && mb % 1024 === 0) return (mb / 1024) + ' GB';
        return mb + ' MB';
    };

    $scope.formatRate = function(bytesPerSecond) {
        bytesPerSecond = Number(bytesPerSecond || 0);
        if (bytesPerSecond < 1024) return bytesPerSecond.toFixed(0) + ' B/s';
        if (bytesPerSecond < 1024 * 1024) return (bytesPerSecond / 1024).toFixed(1) + ' KB/s';
        return (bytesPerSecond / (1024 * 1024)).toFixed(2) + ' MB/s';
    };

    $scope.formatDuration = function(seconds) {
        seconds = Number(seconds || 0);
        var h = Math.floor(seconds / 3600);
        var m = Math.floor((seconds % 3600) / 60);
        if (h > 0) return h + 'h ' + m + 'm';
        return m + 'm';
    };

    $scope.sessionPercent = function() {
        var limit = $scope.mb($scope.settings.SessionLimitMB);
        if (!limit) return 0;
        return Math.min(100, ($scope.data.TotalBytes / limit) * 100);
    };

    $scope.limitProgressClass = function() {
        var p = $scope.sessionPercent();
        if (p >= 100) return 'danger';
        if (p >= 75) return 'warning';
        return 'safe';
    };

    $scope.deviceLimitMB = function(client) {
        if (client && client.Exempt) return 0;
        return Number((client && client.LimitMB) || $scope.settings.AutoBlockMB || 0);
    };

    $scope.deviceSummary = function(row) {
        var c = row.client;
        if (!row.known) return 'Not connected';
        var used = $scope.formatBytes(c.TotalBytes);
        if (!c.Connected) return 'Not connected · ' + used + ' this session';
        if (c.Exempt) return used + ' · no limit';
        var limit = $scope.formatMB($scope.deviceLimitMB(c));
        return used + ' of ' + limit + (Number(c.LimitMB || 0) > 0 ? ' (custom)' : '');
    };

    // --- Device list: connected devices plus remembered ones ---
    var SAVED_KEY = 'stratux_nx_saved_devices';

    function loadSavedDevices() {
        try {
            return JSON.parse(localStorage.getItem(SAVED_KEY)) || {};
        } catch (e) { return {}; }
    }

    function persistSavedDevices() {
        localStorage.setItem(SAVED_KEY, JSON.stringify($scope.savedDevices));
    }

    $scope.savedDevices = loadSavedDevices();

    function buildDeviceRows() {
        var rows = [];
        var seen = {};
        var clients = ($scope.data.Clients || []).slice().sort(function(a, b) {
            if (a.Connected !== b.Connected) return a.Connected ? -1 : 1;
            return Number(b.TotalBytes || 0) - Number(a.TotalBytes || 0);
        });
        clients.forEach(function(c) {
            var saved = c.MAC && $scope.savedDevices[c.MAC];
            seen[c.MAC] = true;
            rows.push({key: deviceKey(c), client: c, known: true, name: (saved && saved.name) || c.Hostname || 'Unknown device'});
        });
        Object.keys($scope.savedDevices).forEach(function(mac) {
            if (seen[mac]) return;
            var saved = $scope.savedDevices[mac];
            rows.push({key: mac, client: {MAC: mac, IP: saved.ip, Hostname: saved.name, Connected: false}, known: false, name: saved.name});
        });
        $scope.deviceRows = rows;

        if ($scope.deviceEdit) {
            var key = $scope.deviceEdit.key;
            var current = rows.filter(function(r) { return r.key === key; })[0];
            if (current) {
                $scope.deviceEdit.client = current.client;
                $scope.deviceEdit.known = current.known;
            }
        }
    }

    // --- Per-device limits ---
    $scope.policyDraft = function(client) {
        var key = deviceKey(client);
        if (!devicePolicyDrafts[key]) {
            devicePolicyDrafts[key] = {
                mode: client.Exempt ? 'unlimited' : (Number(client.LimitMB || 0) > 0 ? 'custom' : 'standard'),
                limitMB: Number(client.LimitMB || $scope.settings.AutoBlockMB || 500),
                pending: false
            };
        }
        return devicePolicyDrafts[key];
    };

    $scope.applyDevicePolicy = function(client, rollbackMode) {
        if (!client) return false;
        var draft = $scope.policyDraft(client);
        var limit = draft.mode === 'custom' ? Number(draft.limitMB) : 0;
        var invalid = '';
        if (draft.mode === 'custom' && (!limit || limit < 1)) {
            invalid = 'Enter a device limit of at least 1 MB.';
        } else if (draft.mode === 'custom' && limit > Number($scope.settings.SessionLimitMB || 0)) {
            invalid = 'A device limit cannot be higher than the everyone limit of ' + $scope.formatMB($scope.settings.SessionLimitMB) + '.';
        }
        if (invalid) {
            if (rollbackMode) draft.mode = rollbackMode;
            showFeedback(invalid, 'error');
            return false;
        }
        draft.pending = true;
        $http.post('/dataUsage/policy', {
            ip: client.IP,
            mac: client.MAC || '',
            exempt: draft.mode === 'unlimited',
            limitMB: limit
        }).then(function() {
            draft.pending = false;
            client.Exempt = draft.mode === 'unlimited';
            client.LimitMB = draft.mode === 'custom' ? limit : 0;
            client.Blocked = false;
            var name = client.Hostname || 'This device';
            if (draft.mode === 'unlimited') {
                showFeedback(name + ' now has no data limit.');
            } else if (draft.mode === 'custom') {
                showFeedback(name + ' will pause at ' + limit + ' MB.');
            } else {
                showFeedback(name + ' now uses the standard ' + $scope.settings.AutoBlockMB + ' MB limit.');
            }
            $scope.errorMessage = '';
            buildDeviceRows();
        }, function(response) {
            draft.pending = false;
            if (rollbackMode) draft.mode = rollbackMode;
            $scope.errorMessage = 'Could not save the device limit: ' + ((response && response.data) || 'unknown error');
        });
        return true;
    };

    $scope.setFollowsLimits = function(client, follows) {
        var draft = $scope.policyDraft(client);
        var previousMode = draft.mode;
        draft.mode = follows ? 'standard' : 'unlimited';
        $scope.applyDevicePolicy(client, previousMode);
    };

    $scope.openDevice = function(row) {
        var c = row.client;
        var saved = c.MAC && $scope.savedDevices[c.MAC];
        $scope.deviceEdit = {
            key: row.key,
            client: c,
            known: row.known,
            title: row.name,
            mode: c.Exempt ? 'unlimited' : (Number(c.LimitMB || 0) > 0 ? 'custom' : 'standard'),
            limitMB: Number(c.LimitMB || $scope.settings.AutoBlockMB || 500),
            name: saved ? saved.name : '',
            remember: !!saved
        };
    };

    $scope.closeDevice = function() {
        $scope.deviceEdit = null;
    };

    $scope.saveDevice = function() {
        var edit = $scope.deviceEdit;
        var c = edit.client;
        if (c.MAC) {
            if (edit.remember) {
                $scope.savedDevices[c.MAC] = {
                    name: (edit.name || '').trim() || c.Hostname || 'Unknown device',
                    mac: c.MAC,
                    ip: c.IP
                };
            } else {
                delete $scope.savedDevices[c.MAC];
            }
            persistSavedDevices();
        }
        if (edit.known) {
            var current = c.Exempt ? 'unlimited' : (Number(c.LimitMB || 0) > 0 ? 'custom' : 'standard');
            var changed = edit.mode !== current || (edit.mode === 'custom' && Number(edit.limitMB) !== Number(c.LimitMB));
            if (changed) {
                var draft = $scope.policyDraft(c);
                var previousMode = draft.mode;
                draft.mode = edit.mode;
                draft.limitMB = Number(edit.limitMB);
                if (!$scope.applyDevicePolicy(c, previousMode)) return;
            }
        }
        $scope.deviceEdit = null;
        buildDeviceRows();
    };

    $scope.toggleBlock = function(client) {
        if (!client || client.actionPending) return;
        var blocked = !client.Blocked;
        client.actionPending = true;
        $http.post('/dataUsage/block', {
            ip: client.IP,
            blocked: blocked
        }).then(function() {
            client.actionPending = false;
            client.Blocked = blocked;
            var name = client.Hostname || 'This device';
            showFeedback(blocked ?
                'Internet paused for ' + name + '. Stratux NX remains available.' :
                'Internet restored for ' + name + '.');
            $scope.errorMessage = '';
        }, function(response) {
            client.actionPending = false;
            $scope.errorMessage = 'Could not change Internet access for ' + client.IP + ': ' + (response.data || 'unknown error');
        });
    };

    // --- Session and global limits ---
    $scope.refresh = function() {
        $http.get('/dataUsage', {cache: false}).then(function(response) {
            var d = response.data || {};
            $scope.data = d;
            $scope.data.Clients = d.Clients || [];
            $scope.data.RecentEvents = d.RecentEvents || [];
            if (!settingsLoaded && d.Settings) {
                $scope.settings = angular.copy(d.Settings);
                settingsLoaded = true;
            }
            buildDeviceRows();
        }, function() {
            $scope.errorMessage = 'Unable to read Internet usage monitor. The service may still be starting.';
        });
    };

    $scope.saveSettings = function(message, onError, onSuccess) {
        var warning = Number($scope.settings.WarningMB);
        var block = Number($scope.settings.AutoBlockMB);
        var session = Number($scope.settings.SessionLimitMB);
        if (!warning || !block || !session || warning > block || block > session) {
            $scope.errorMessage = 'Limits must be: warning ≤ device limit ≤ everyone limit.';
            if (onError) onError();
            return;
        }
        $scope.savingSettings = true;
        $http.post('/dataUsage/settings', {
            warningMB: warning,
            autoBlockMB: block,
            sessionLimitMB: session,
            autoBlockEnabled: !!$scope.settings.AutoBlockEnabled
        }).then(function() {
            $scope.savingSettings = false;
            $scope.errorMessage = '';
            showFeedback(message || 'Limits saved.');
            if (onSuccess) onSuccess();
            $scope.refresh();
        }, function(response) {
            $scope.savingSettings = false;
            $scope.errorMessage = 'Could not save limits: ' + (response.data || 'unknown error');
            if (onError) onError();
        });
    };

    $scope.toggleProtection = function() {
        if ($scope.protectionPending || $scope.savingSettings) return;
        var previous = $scope.settings.AutoBlockEnabled;
        $scope.settings.AutoBlockEnabled = !previous;
        $scope.protectionPending = true;
        $scope.saveSettings($scope.settings.AutoBlockEnabled ?
            'Automatic limits are on.' :
            'Automatic limits are off.', function() {
                $scope.settings.AutoBlockEnabled = previous;
                $scope.protectionPending = false;
            }, function() {
                $scope.protectionPending = false;
            });
    };

    $scope.openLimits = function() {
        $scope.limitsDraft = angular.copy($scope.settings);
    };

    $scope.closeLimits = function() {
        $scope.limitsDraft = null;
    };

    $scope.saveLimits = function() {
        var previous = angular.copy($scope.settings);
        ['WarningMB', 'AutoBlockMB', 'SessionLimitMB'].forEach(function(field) {
            $scope.settings[field] = Number($scope.limitsDraft[field]);
        });
        $scope.saveSettings('Limits saved.', function() {
            $scope.settings = previous;
        }, function() {
            $scope.limitsDraft = null;
        });
    };

    $scope.resetSession = function() {
        if (!window.confirm('Start a new session? Usage goes back to 0 MB and paused devices get Internet again. Your limits are kept.')) {
            return;
        }
        $scope.resetPending = true;
        $http.post('/dataUsage/reset', {}).then(function() {
            $scope.resetPending = false;
            showFeedback('New Internet data session started at 0 MB.');
            $scope.refresh();
        }, function(response) {
            $scope.resetPending = false;
            $scope.errorMessage = 'Could not reset session: ' + (response.data || 'unknown error');
        });
    };

    $scope.refresh();
    var timer = $interval($scope.refresh, 2000);
    $scope.$on('$destroy', function() {
        $interval.cancel(timer);
        // Mobile Angular UI keeps yielded modal content after the page is left.
        angular.forEach(document.querySelectorAll('.nxd-modal'), function(el) {
            var modal = el.closest('.modal');
            if (modal) angular.element(modal).remove();
        });
    });
});
