appControllers.controller('GuardianCtrl', function($scope, $http, $interval, $q) {
    $scope.guardian = {
        loading: true,
        state: 'checking',
        title: 'Checking the system',
        message: 'NX Guardian is reading the receiver, GPS and system state.',
        checks: [],
        checkedAt: null
    };

    function check(id, title, state, value, detail, action, route) {
        return {
            id: id,
            title: title,
            state: state,
            value: value,
            detail: detail,
            action: action || '',
            route: route || ''
        };
    }

    function receiverCheck(status, settings) {
        var enabled = (settings.UAT_Enabled ? 1 : 0) + (settings.ES_Enabled ? 1 : 0);
        if (!enabled) {
            return check('receivers', 'ADS-B receivers', 'warning', 'Not enabled',
                'Enable at least one receiver for traffic reception.', 'Open Settings', '#/settings');
        }
        if (settings.UAT_Enabled && !status.UATRadio_connected) {
            return check('receivers', 'ADS-B receivers', 'critical', 'UAT receiver missing',
                'The configured 978 MHz receiver is not responding.', 'Review radios', '#/settings');
        }
        if (Number(status.Devices || 0) < 1) {
            return check('receivers', 'ADS-B receivers', 'critical', 'Receiver missing',
                'No SDR receiver was detected for the enabled ADS-B configuration.', 'Review radios', '#/settings');
        }
        var bands = [];
        if (settings.UAT_Enabled) bands.push('978 UAT');
        if (settings.ES_Enabled) bands.push('1090ES');
        var esConfirmed = !settings.ES_Enabled || Number(status.ES_messages_last_minute || 0) > 0;
        return check('receivers', 'ADS-B receivers', esConfirmed ? 'ready' : 'info', bands.join(' + '),
            esConfirmed ? 'Configured receivers are responding or have recorded reception.' :
                '1090ES is configured. No reception has been recorded yet, which can be normal without nearby traffic.');
    }

    function gpsCheck(status, settings) {
        if (!settings.GPS_Enabled) {
            return check('gps', 'GPS position', 'warning', 'Disabled',
                'Traffic can still be received, but ownship position and automatic Flight Log are limited.', 'Open GPS settings', '#/settings');
        }
        if (!status.GPS_connected) {
            return check('gps', 'GPS position', 'critical', 'GPS not responding',
                'The configured GPS receiver is not communicating.', 'Open GPS / AHRS', '#/gps');
        }
        if (status.GPS_solution === 'No Fix' || status.GPS_solution === 'Disconnected' || status.GPS_solution === 'Unknown') {
            return check('gps', 'GPS position', 'warning', 'Waiting for position',
                'The receiver is connected but has no position fix. This is common indoors.', 'Open GPS / AHRS', '#/gps');
        }
        var accuracy = Number(status.GPS_position_accuracy);
        var detail = Number.isFinite(accuracy) && accuracy < 10000 ?
            Number(status.GPS_satellites_locked || 0) + ' satellites in solution · ' + accuracy.toFixed(1) + ' m reported accuracy.' :
            Number(status.GPS_satellites_locked || 0) + ' satellites in solution.';
        return check('gps', 'GPS position', 'ready', status.GPS_solution || 'Position fixed', detail, 'Open GPS / AHRS', '#/gps');
    }

    function storageCheck(status, flightLog) {
        if (flightLog && flightLog.StorageError) {
            return check('storage', 'Flight storage', 'critical', 'Recording not protected',
                flightLog.StorageError + ' Keep the receiver powered and repair or replace its storage before the next flight.',
                'Open Flight Log', '#/flightlog');
        }
        var freeMiB = Number(status.DiskBytesFree || 0) / 1048576;
        if (freeMiB < 80) {
            return check('storage', 'Storage', 'critical', freeMiB.toFixed(0) + ' MiB free',
                'Very little persistent storage remains. Export logs and remove data before updating.', 'Manage logs', '#/logs');
        }
        if (freeMiB < 160) {
            return check('storage', 'Storage', 'warning', freeMiB.toFixed(0) + ' MiB free',
                'Storage is getting low and may not fit a future update package.', 'Manage logs', '#/logs');
        }
        return check('storage', 'Storage', 'ready', freeMiB.toFixed(0) + ' MiB free',
            'Enough persistent space is currently available for normal logging.');
    }

    function temperatureCheck(status) {
        var temperature = Number(status.CPUTemp);
        if (!Number.isFinite(temperature) || temperature <= 0) {
            return check('temperature', 'Temperature', 'warning', 'Unavailable',
                'NX Guardian could not read the processor temperature.');
        }
        if (temperature >= 82) {
            return check('temperature', 'Temperature', 'critical', temperature.toFixed(1) + ' °C',
                'The receiver is unusually hot. Improve airflow and keep it out of direct sun.');
        }
        if (temperature >= 75) {
            return check('temperature', 'Temperature', 'warning', temperature.toFixed(1) + ' °C',
                'The receiver is warm. Check airflow before extended use.');
        }
        return check('temperature', 'Temperature', 'ready', temperature.toFixed(1) + ' °C',
            'Processor temperature is in the normal operating range.');
    }

    function clientsCheck(status) {
        var clients = Number(status.Connected_Users || 0);
        if (clients < 1) {
            return check('clients', 'Recent EFB responses', 'warning', 'No recent response',
                'Open the EFB and confirm it is configured to receive Stratux data.');
        }
        return check('clients', 'Recent EFB responses', 'ready', clients + ' observed',
            'Responses were observed recently. Multiple connections from one device may be counted separately.');
    }

    function updateCheck(update) {
        if (update.download_error || update.check_error) {
            return check('update', 'Software update', 'warning', 'Check needed',
                update.download_error || update.check_error, 'Open update settings', '#/settings');
        }
        if (update.staged) {
            return check('update', 'Software update', 'warning', 'Restart pending',
                'An update package is staged and will be applied on restart.', 'Open update settings', '#/settings');
        }
        if (update.available) {
            return check('update', 'Software update', 'info', 'Update available',
                'Install it while parked when convenient.', 'Open update settings', '#/settings');
        }
        if (!update.last_check || Date.parse(update.last_check) < Date.now() - 365 * 86400000) {
            return check('update', 'Software update', 'info', 'Not checked yet',
                'Internet is not required for reception. Update availability will be checked when a connection is available.', 'Open update settings', '#/settings');
        }
        return check('update', 'Software update', 'ready', update.current_version || 'Current',
            'No pending update action was detected.');
    }

    function summarize(checks, status) {
        var critical = checks.filter(function(item) { return item.state === 'critical'; }).length;
        var warnings = checks.filter(function(item) { return item.state === 'warning'; }).length;
        var unverified = checks.filter(function(item) { return item.state === 'info'; }).length;
        if (Array.isArray(status.Errors) && status.Errors.length) {
            critical += status.Errors.length;
            checks.unshift(check('errors', 'System errors', 'critical', status.Errors.length + ' reported',
                status.Errors.join(' · '), 'Open logs', '#/logs'));
        }
        if (critical) {
            return {
                state: 'critical',
                title: 'System needs attention',
                message: critical + ' item' + (critical === 1 ? '' : 's') + ' may limit the receiver. Review the actions below before relying on its data.'
            };
        }
        if (warnings) {
            return {
                state: 'warning',
                title: 'Ready with limitations',
                message: warnings + ' item' + (warnings === 1 ? '' : 's') + ' should be understood. Core reception may still be available.'
            };
        }
        if (unverified) {
            return {
                state: 'warning',
                title: 'Ready with limitations',
                message: unverified + ' item' + (unverified === 1 ? ' could not be' : 's could not be') + ' fully verified. Review the context below.'
            };
        }
        return {
            state: 'ready',
            title: 'System ready',
            message: 'Configured receivers and supporting services are responding normally.'
        };
    }

    $scope.refreshGuardian = function() {
        if ($scope.guardian.loading && $scope.guardian.checkedAt) return;
        $scope.guardian.loading = true;
        $q.all([
            $http.get('/getStatus', {cache: false}),
            $http.get('/getSettings', {cache: false}),
            $http.get('/update/status', {cache: false}),
            $http.get('/flightLog', {cache: false})
        ]).then(function(responses) {
            var status = responses[0].data || {};
            var settings = responses[1].data || {};
            var update = responses[2].data || {};
            var flightLog = responses[3].data || {};
            var checks = [
                receiverCheck(status, settings),
                gpsCheck(status, settings),
                storageCheck(status, flightLog),
                temperatureCheck(status),
                clientsCheck(status),
                updateCheck(update)
            ];
            var summary = summarize(checks, status);
            $scope.guardian.state = summary.state;
            $scope.guardian.title = summary.title;
            $scope.guardian.message = summary.message;
            $scope.guardian.checks = checks;
            $scope.guardian.checkedAt = new Date();
        }, function() {
            $scope.guardian.state = 'critical';
            $scope.guardian.title = 'Status unavailable';
            $scope.guardian.message = 'NX Guardian could not read the receiver. Reconnect to the Stratux NX Wi-Fi and try again.';
            $scope.guardian.checks = [];
        }).finally(function() {
            $scope.guardian.loading = false;
        });
    };

    $scope.guardianIcon = function(state) {
        if (state === 'ready') return 'fa-check';
        if (state === 'critical') return 'fa-times';
        if (state === 'warning') return 'fa-exclamation';
        if (state === 'info') return 'fa-info';
        return 'fa-circle-o-notch fa-spin';
    };

    $scope.refreshGuardian();
    var guardianPoller = $interval($scope.refreshGuardian, 15000);
    $scope.$on('$destroy', function() {
        $interval.cancel(guardianPoller);
    });
});
