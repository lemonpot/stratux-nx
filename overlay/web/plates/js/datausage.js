appControllers.controller('DataUsageCtrl', function($scope, $http, $interval, $timeout) {
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

    $scope.Math = window.Math;
    $scope.errorMessage = '';
    $scope.feedbackMessage = '';
    $scope.feedbackType = 'success';
    $scope.savingSettings = false;
    $scope.protectionPending = false;
    $scope.resetPending = false;
    $scope.refreshPending = false;
    var settingsLoaded = false;
    var feedbackTimer = null;
    var devicePolicyDrafts = {};

    function deviceKey(client) {
        return String((client && (client.MAC || client.IP)) || '');
    }

    function showFeedback(message, type) {
        $scope.feedbackMessage = message;
        $scope.feedbackType = type || 'success';
        if (feedbackTimer) $timeout.cancel(feedbackTimer);
        feedbackTimer = $timeout(function() {
            $scope.feedbackMessage = '';
        }, 4500);
        if (window.StratuxUI && window.StratuxUI.toast) {
            window.StratuxUI.toast(message, type || 'success', 3000);
        }
    }
    $scope.showFeedback = showFeedback;

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

    $scope.formatTime = function(value) {
        if (!value) return 'now';
        var d = new Date(value);
        if (isNaN(d.getTime())) return value;
        return d.toLocaleString();
    };

    $scope.sessionPercent = function() {
        var limit = $scope.mb($scope.settings.SessionLimitMB);
        if (!limit) return 0;
        return Math.min(100, ($scope.data.TotalBytes / limit) * 100);
    };

    $scope.remainingBytes = function() {
        var remaining = $scope.mb($scope.settings.SessionLimitMB) - Number($scope.data.TotalBytes || 0);
        return Math.max(0, remaining);
    };

    $scope.deviceLimitMB = function(client) {
        if (client && client.Exempt) return 0;
        return Number((client && client.LimitMB) || $scope.settings.AutoBlockMB || 0);
    };

    $scope.devicePercent = function(client) {
        var limit = $scope.mb($scope.deviceLimitMB(client));
        if (!limit) return 0;
        return Math.min(100, (Number(client.TotalBytes || 0) / limit) * 100);
    };

    $scope.limitProgressClass = function() {
        var p = $scope.sessionPercent();
        if (p >= 100) return 'danger';
        if (p >= 75) return 'warning';
        return 'safe';
    };

    $scope.deviceProgressClass = function(client) {
        if (client.Blocked) return 'danger';
        var percent = $scope.devicePercent(client);
        if (percent >= 100) return 'danger';
        if (percent >= 75) return 'warning';
        return 'safe';
    };

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

    $scope.chooseDevicePolicy = function(client, mode) {
        var draft = $scope.policyDraft(client);
        var previousMode = draft.mode;
        draft.mode = mode;
        if (mode === 'custom') {
            if (!draft.limitMB) draft.limitMB = Number($scope.settings.AutoBlockMB || 500);
            return;
        }
        $scope.applyDevicePolicy(client, previousMode);
    };

    $scope.applyDevicePolicy = function(client, rollbackMode) {
        if (!client) return;
        var draft = $scope.policyDraft(client);
        var limit = draft.mode === 'custom' ? Number(draft.limitMB) : 0;
        if (draft.mode === 'custom' && (!limit || limit < 1)) {
            $scope.errorMessage = 'Enter a device limit of at least 1 MB.';
            return;
        }
        if (draft.mode === 'custom' && limit > Number($scope.settings.SessionLimitMB || 0)) {
            $scope.errorMessage = 'This device limit cannot be higher than the session limit of ' + $scope.settings.SessionLimitMB + ' MB.';
            return;
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
                showFeedback(name + ' now has no automatic data limit.');
            } else if (draft.mode === 'custom') {
                showFeedback(name + ' will pause at ' + limit + ' MB.');
            } else {
                showFeedback(name + ' now uses the standard ' + $scope.settings.AutoBlockMB + ' MB limit.');
            }
            $scope.errorMessage = '';
            $timeout($scope.refresh, 350);
        }, function(response) {
            draft.pending = false;
            if (rollbackMode) draft.mode = rollbackMode;
            $scope.errorMessage = 'Could not save the device limit: ' + ((response && response.data) || 'unknown error');
        });
    };

    $scope.eventIcon = function(event) {
        switch (event.Type) {
            case 'auto_block':
            case 'manual_block':
            case 'session_limit': return 'fa-ban';
            case 'manual_unblock': return 'fa-unlock';
            case 'warning': return 'fa-exclamation-triangle';
            case 'client_connected': return 'fa-wifi';
            case 'settings': return 'fa-shield';
            case 'session_reset':
            case 'session_start': return 'fa-play';
            default: return 'fa-clock-o';
        }
    };

    $scope.eventClass = function(event) {
        switch (event.Type) {
            case 'auto_block':
            case 'manual_block':
            case 'session_limit': return 'danger';
            case 'warning': return 'warning';
            case 'manual_unblock': return 'success';
            default: return '';
        }
    };

    $scope.refresh = function() {
        $scope.refreshPending = true;
        $http.get('/dataUsage', {cache: false}).then(function(response) {
            var d = response.data || {};
            $scope.data = d;
            $scope.data.Clients = d.Clients || [];
            $scope.data.RecentEvents = d.RecentEvents || [];
            if (!settingsLoaded && d.Settings) {
                $scope.settings = angular.copy(d.Settings);
                settingsLoaded = true;
            }
            $scope.errorMessage = '';
            $scope.refreshPending = false;
        }, function() {
            $scope.refreshPending = false;
            $scope.errorMessage = 'Unable to read Internet usage monitor. The service may still be starting.';
        });
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
            $timeout($scope.refresh, 350);
        }, function(response) {
            client.actionPending = false;
            $scope.errorMessage = 'Could not change Internet access for ' + client.IP + ': ' + (response.data || 'unknown error');
        });
    };

    $scope.saveSettings = function(message, onError) {
        var warning = Number($scope.settings.WarningMB);
        var block = Number($scope.settings.AutoBlockMB);
        var session = Number($scope.settings.SessionLimitMB);
        if (!warning || !block || !session || warning > block || block > session) {
            $scope.errorMessage = 'Limits must be: Warning ≤ Auto-block ≤ Session limit.';
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
            showFeedback(message || 'Protection settings saved. The new limits are active.');
            $scope.refresh();
        }, function(response) {
            $scope.savingSettings = false;
            $scope.errorMessage = 'Could not save safety limits: ' + (response.data || 'unknown error');
            if (onError) onError();
        });
    };

    $scope.toggleProtection = function() {
        if ($scope.protectionPending || $scope.savingSettings) return;
        var previous = $scope.settings.AutoBlockEnabled;
        $scope.settings.AutoBlockEnabled = !$scope.settings.AutoBlockEnabled;
        $scope.protectionPending = true;
        $scope.saveSettings($scope.settings.AutoBlockEnabled ?
            'Automatic data protection is now on.' :
            'Automatic data protection is now off.', function() {
                $scope.settings.AutoBlockEnabled = previous;
            });
        var stop = $scope.$watch('savingSettings', function(saving) {
            if (!saving) {
                $scope.protectionPending = false;
                stop();
            }
        });
    };

    $scope.resetSession = function() {
        if (!window.confirm('Start a new data session? Current counters will reset to zero. The historical log will NOT be deleted.')) {
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

    // --- Saved devices (localStorage) ---
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

    $scope.isDeviceSaved = function(mac) {
        return mac && $scope.savedDevices[mac];
    };

    $scope.saveDevice = function(client) {
        if (!client.MAC) return;
        $scope.savedDevices[client.MAC] = {
            name: client.Hostname || 'Unknown device',
            mac: client.MAC,
            ip: client.IP,
            lastSeen: new Date().toISOString(),
            totalBytes: client.TotalBytes || 0
        };
        persistSavedDevices();
        showFeedback((client.Hostname || 'Device') + ' saved for future sessions.');
    };

    $scope.removeDevice = function(mac) {
        delete $scope.savedDevices[mac];
        persistSavedDevices();
        showFeedback('Saved device removed.');
    };

    $scope.renameDevice = function(mac) {
        var dev = $scope.savedDevices[mac];
        if (!dev) return;
        var name = window.prompt('Device name:', dev.name);
        if (name !== null && name.trim()) {
            dev.name = name.trim();
            persistSavedDevices();
            showFeedback('Device renamed to ' + dev.name + '.');
        }
    };

    $scope.savedDeviceList = function() {
        var list = [];
        var saved = $scope.savedDevices;
        for (var mac in saved) {
            if (saved.hasOwnProperty(mac)) {
                var d = angular.copy(saved[mac]);
                d.isOnline = false;
                for (var i = 0; i < $scope.data.Clients.length; i++) {
                    if ($scope.data.Clients[i].MAC === mac && $scope.data.Clients[i].Connected) {
                        d.isOnline = true;
                        d.ip = $scope.data.Clients[i].IP;
                        d.totalBytes = $scope.data.Clients[i].TotalBytes;
                        break;
                    }
                }
                list.push(d);
            }
        }
        return list;
    };

    $scope.refresh();
    var timer = $interval($scope.refresh, 2000);
    $scope.$on('$destroy', function() {
        $interval.cancel(timer);
        if (feedbackTimer) $timeout.cancel(feedbackTimer);
    });
});
