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

    $scope.Math = window.Math;
    $scope.errorMessage = '';
    $scope.savingSettings = false;
    var settingsLoaded = false;

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

    $scope.devicePercent = function(client) {
        var limit = $scope.mb($scope.settings.AutoBlockMB);
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
        if (Number(client.TotalBytes || 0) >= $scope.mb($scope.settings.AutoBlockMB)) return 'danger';
        if (Number(client.TotalBytes || 0) >= $scope.mb($scope.settings.WarningMB)) return 'warning';
        return 'safe';
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
        }, function() {
            $scope.errorMessage = 'Unable to read Internet usage monitor. The service may still be starting.';
        });
    };

    $scope.toggleBlock = function(client) {
        client.actionPending = true;
        $http.post('/dataUsage/block', {
            ip: client.IP,
            blocked: !client.Blocked
        }).then(function() {
            client.actionPending = false;
            $scope.refresh();
        }, function(response) {
            client.actionPending = false;
            $scope.errorMessage = 'Could not change Internet access for ' + client.IP + ': ' + (response.data || 'unknown error');
        });
    };

    $scope.saveSettings = function() {
        var warning = Number($scope.settings.WarningMB);
        var block = Number($scope.settings.AutoBlockMB);
        var session = Number($scope.settings.SessionLimitMB);
        if (!warning || !block || !session || warning > block || block > session) {
            $scope.errorMessage = 'Limits must be: Warning ≤ Auto-block ≤ Session limit.';
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
            $scope.refresh();
        }, function(response) {
            $scope.savingSettings = false;
            $scope.errorMessage = 'Could not save safety limits: ' + (response.data || 'unknown error');
        });
    };

    $scope.resetSession = function() {
        if (!window.confirm('Start a new data session? Current counters will reset to zero. The historical log will NOT be deleted.')) {
            return;
        }
        $http.post('/dataUsage/reset', {}).then(function() {
            $scope.refresh();
        }, function(response) {
            $scope.errorMessage = 'Could not reset session: ' + (response.data || 'unknown error');
        });
    };

    $scope.refresh();
    var timer = $interval($scope.refresh, 2000);
    $scope.$on('$destroy', function() {
        $interval.cancel(timer);
    });
});
