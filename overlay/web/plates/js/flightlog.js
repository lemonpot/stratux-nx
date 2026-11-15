appControllers.controller('FlightLogCtrl', function($scope, $http, $interval) {
    $scope.data = {
        Phase: 'PARKED',
        GPS: {Valid:false, TimeUTC:'', GroundSpeedKt:0, AltitudeFt:0, VerticalSpeedFps:0},
        Current: null,
        CurrentAirport: null,
        Flights: [],
        Settings: {Timezone:'UTC', AutoDetect:true, AutoTimezone:true},
        AirportDatabaseReady: false,
        AirportCount: 0,
        StorageError: '',
        StopCountdownSeconds: 45,
        Sync: {Enabled:false, InstallationID:'', Online:false, Pending:0, LastError:'', Linked:false, AccountName:'', AccountEmail:''},
        LocalTimezone: 'UTC',
        TimezoneSource: 'fallback',
        TimezoneDatabaseReady: false,
        StoragePath: ''
    };
    $scope.settings = {Timezone:'UTC', AutoDetect:true, AutoTimezone:true};
    $scope.settingsDraft = null;
    $scope.errorMessage = '';
    $scope.savingSettings = false;
    $scope.selectedFlight = null;
    $scope.syncing = false;
    $scope.finishing = false;
    $scope.flightLimit = 20;
    $scope.browserTimezone = '';
    var settingsLoaded = false;
    var selectedFlightMap = null;

    function responseMessage(response, fallback) {
        var detail = response && response.data;
        if (typeof detail !== 'string' || /^\s*</.test(detail)) {
            return fallback;
        }
        return detail;
    }

    function toast(message, type, ms) {
        if (window.StratuxUI) window.StratuxUI.toast(message, type, ms);
    }

    try {
        if (window.Intl && Intl.DateTimeFormat) {
            $scope.browserTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone || '';
        }
    } catch (e) {}

    // String.padStart is missing on older iPad Safari versions.
    function pad2(value) {
        value = String(value);
        return value.length < 2 ? '0' + value : value;
    }

    function validDate(value) {
        if (!value) return null;
        var d = new Date(value);
        return isNaN(d.getTime()) ? null : d;
    }

    function timezone() {
        if ($scope.settings && $scope.settings.AutoTimezone && $scope.data && $scope.data.LocalTimezone) {
            return $scope.data.LocalTimezone;
        }
        return ($scope.settings && $scope.settings.Timezone) || ($scope.data && $scope.data.LocalTimezone) || 'UTC';
    }

    $scope.localTimezoneLabel = function() {
        return timezone();
    };

    function localFormat(value, opts) {
        var d = validDate(value);
        if (!d) return null;
        opts.timeZone = timezone();
        try {
            return new Intl.DateTimeFormat('en-CA', opts).format(d);
        } catch (e) {
            return opts.year ? d.toLocaleDateString() : d.toLocaleTimeString();
        }
    }

    $scope.formatLocalShort = function(value) {
        return localFormat(value, {hour:'2-digit', minute:'2-digit', hour12:false}) || '--:--';
    };

    $scope.formatZuluShort = function(value) {
        var d = validDate(value);
        if (!d) return '--:--Z';
        return pad2(d.getUTCHours()) + ':' + pad2(d.getUTCMinutes()) + 'Z';
    };

    $scope.formatLocalDate = function(value) {
        return localFormat(value, {year:'numeric', month:'short', day:'2-digit'}) || '--';
    };

    $scope.formatDuration = function(seconds) {
        seconds = Math.max(0, Number(seconds || 0));
        var h = Math.floor(seconds / 3600);
        var m = Math.floor((seconds % 3600) / 60);
        var s = Math.floor(seconds % 60);
        if (h > 0) return h + 'h ' + pad2(m) + 'm';
        if (m > 0) return m + 'm ' + pad2(s) + 's';
        return s + 's';
    };

    $scope.formatNM = function(value) { return Number(value || 0).toFixed(1) + ' NM'; };
    $scope.formatKt = function(value) { return Math.round(Number(value || 0)) + ' kt'; };
    $scope.formatFt = function(value) { return Math.round(Number(value || 0)).toLocaleString() + ' ft'; };

    $scope.formatVerticalSpeed = function(fps) {
        var fpm = Number(fps || 0) * 60;
        if (!isFinite(fpm)) return '--';
        var rounded = Math.round(fpm / 10) * 10;
        if (Math.abs(rounded) < 10) rounded = 0;
        return (rounded > 0 ? '+' : '') + rounded.toLocaleString() + ' fpm';
    };

    $scope.airportCode = function(a) { return a && a.Code ? a.Code : '---'; };

    $scope.phaseLabel = function(phase) {
        switch (phase) {
            case 'TAXI_OUT': return 'Taxi out';
            case 'AIRBORNE': return 'Airborne';
            case 'TAXI_IN': return 'Taxi in';
            default: return 'Parked';
        }
    };

    $scope.phaseClass = function(phase) {
        if (phase === 'AIRBORNE') return 'airborne';
        if (phase === 'TAXI_OUT' || phase === 'TAXI_IN') return 'taxi';
        return 'parked';
    };

    $scope.statusHint = function() {
        var d = $scope.data;
        if (d.Current) return 'Flight in progress';
        if (!d.GPS.Valid) return 'Waiting for GPS';
        if (!$scope.settings.AutoDetect) return 'Automatic detection is off';
        if (d.CurrentAirport) return 'At ' + d.CurrentAirport.Code + (d.CurrentAirport.Name ? ' · ' + d.CurrentAirport.Name : '');
        return 'A flight starts by itself when you taxi';
    };

    $scope.syncSummary = function() {
        var sync = $scope.data.Sync || {};
        var pending = Number(sync.Pending || 0);
        if (!pending) return 'All flights uploaded';
        var text = pending + (pending === 1 ? ' flight' : ' flights') + ' waiting to upload';
        return sync.Online ? text : text + ' · uploads when Internet is available';
    };

    $scope.syncStatusLabel = function(status) {
        switch (status) {
            case 'uploaded': return 'Cloud';
            case 'uploading': return 'Uploading';
            case 'failed': return 'Retry needed';
            default: return 'Pending';
        }
    };

    $scope.syncStatusClass = function(status) {
        if (status === 'uploaded') return 'ok';
        if (status === 'failed') return 'bad';
        return 'muted';
    };

    function destroySelectedFlightMap() {
        if (selectedFlightMap) {
            selectedFlightMap.setTarget(null);
            selectedFlightMap = null;
        }
    }

    function renderSelectedFlightMap() {
        destroySelectedFlightMap();
        var target = document.getElementById('flightlog-selected-map');
        var points = ($scope.selectedFlight && $scope.selectedFlight.Track) || [];
        if (!target || typeof ol === 'undefined' || points.length < 2) return;

        var coordinates = [];
        angular.forEach(points, function(point) {
            var lat = Number(point.Latitude);
            var lon = Number(point.Longitude);
            if (isFinite(lat) && isFinite(lon)) coordinates.push(ol.proj.fromLonLat([lon, lat]));
        });
        if (coordinates.length < 2) return;

        var route = new ol.Feature({geometry: new ol.geom.LineString(coordinates)});
        route.setStyle(new ol.style.Style({
            stroke: new ol.style.Stroke({color:'#635bff', width:4})
        }));
        var start = new ol.Feature({geometry:new ol.geom.Point(coordinates[0])});
        var finish = new ol.Feature({geometry:new ol.geom.Point(coordinates[coordinates.length - 1])});
        start.setStyle(new ol.style.Style({
            image:new ol.style.Circle({radius:7, fill:new ol.style.Fill({color:'#16a36a'}), stroke:new ol.style.Stroke({color:'#fff', width:2})})
        }));
        finish.setStyle(new ol.style.Style({
            image:new ol.style.Circle({radius:7, fill:new ol.style.Fill({color:'#cd3d64'}), stroke:new ol.style.Stroke({color:'#fff', width:2})})
        }));

        selectedFlightMap = new ol.Map({
            target: target,
            layers: [
                new ol.layer.Tile({
                    source:new ol.source.XYZ({
                        url:'https://tile.openstreetmap.org/{z}/{x}/{y}.png',
                        attributions:'&copy; OpenStreetMap contributors',
                        crossOrigin:'anonymous',
                        maxZoom:19
                    })
                }),
                new ol.layer.Vector({source:new ol.source.Vector({features:[route, start, finish]})})
            ],
            view:new ol.View({center:coordinates[0], zoom:9}),
            controls:ol.control.defaults({attributionOptions:{collapsible:true}})
        });
        selectedFlightMap.getView().fit(route.getGeometry().getExtent(), {
            padding:[36, 36, 36, 36],
            maxZoom:14,
            duration:250
        });
    }

    $scope.refresh = function() {
        $http.get('/flightLog', {cache:false}).then(function(response) {
            var d = response.data || {};
            d.GPS = d.GPS || {Valid:false};
            d.Flights = d.Flights || [];
            d.Sync = d.Sync || {};
            $scope.data = d;
            if (!settingsLoaded && d.Settings) {
                $scope.settings = angular.copy(d.Settings);
                settingsLoaded = true;
            }
            $scope.errorMessage = '';
        }, function(response) {
            $scope.errorMessage = 'Unable to read the flight logger: ' + responseMessage(response, 'service unavailable');
        });
    };

    $scope.openSettings = function() {
        $scope.settingsDraft = angular.copy($scope.settings);
        if ($scope.settingsDraft.AutoTimezone === undefined) $scope.settingsDraft.AutoTimezone = true;
    };

    $scope.closeSettings = function() {
        $scope.settingsDraft = null;
    };

    $scope.saveSettings = function() {
        var draft = $scope.settingsDraft;
        if (!draft || $scope.savingSettings) return;
        var payload = {
            Timezone: (draft.Timezone || 'UTC').trim(),
            AutoDetect: !!draft.AutoDetect,
            AutoTimezone: draft.AutoTimezone !== false
        };
        $scope.savingSettings = true;
        $http.post('/flightLog/settings', payload).then(function() {
            $scope.savingSettings = false;
            $scope.settings = payload;
            $scope.settingsDraft = null;
            toast('Flight Log settings saved', 'success', 1800);
            $scope.refresh();
        }, function(response) {
            $scope.savingSettings = false;
            toast('Could not save settings: ' + responseMessage(response, 'unknown error'), 'error', 4200);
        });
    };

    $scope.syncAction = function(action) {
        if ($scope.syncing) return;
        $scope.syncing = true;
        $http.post('/flightLog/sync', {action:action}).then(function(response) {
            $scope.syncing = false;
            $scope.data.Sync = response.data || $scope.data.Sync;
            var message = $scope.data.Sync.Online ? 'Uploading every flight not yet in the cloud' : 'No Internet now. Flights stay on this device and upload automatically later.';
            toast(message, 'success', 4200);
            $scope.refresh();
        }, function(response) {
            $scope.syncing = false;
            toast('Could not sync: ' + responseMessage(response, 'service unavailable'), 'error', 4200);
        });
    };

    $scope.selectFlight = function(flight) {
        if (!flight || !flight.ID) return;
        $http.get('/flightLog/flight?id=' + encodeURIComponent(flight.ID), {cache:false}).then(function(response) {
            $scope.selectedFlight = response.data || null;
            window.setTimeout(renderSelectedFlightMap, 50);
        }, function(response) {
            toast('Could not open this flight: ' + responseMessage(response, 'unknown error'), 'error', 4200);
        });
    };

    $scope.closeSelected = function() {
        destroySelectedFlightMap();
        $scope.selectedFlight = null;
    };

    $scope.finishFlight = function() {
        if ($scope.finishing) return;
        if (!window.confirm('End the current flight now? The current GPS time is saved as the end of the flight.')) return;
        $scope.finishing = true;
        $http.post('/flightLog/action', {action:'finish'}).then(function() {
            $scope.finishing = false;
            toast('Flight saved', 'success', 1800);
            $scope.refresh();
        }, function(response) {
            $scope.finishing = false;
            toast('Could not end the flight: ' + responseMessage(response, 'unknown error'), 'error', 5000);
        });
    };

    $scope.discardFlight = function() {
        if (!window.confirm('Discard the current flight? It will not be saved or uploaded.')) return;
        $http.post('/flightLog/action', {action:'discard'}).then(function() {
            toast('Flight discarded', 'info', 1800);
            $scope.refresh();
        }, function(response) {
            toast('Could not discard the flight: ' + responseMessage(response, 'unknown error'), 'error', 4200);
        });
    };

    $scope.refresh();
    var timer = $interval($scope.refresh, 1000);
    $scope.$on('$destroy', function() {
        $interval.cancel(timer);
        destroySelectedFlightMap();
        // Mobile Angular UI keeps yielded modal content after the page is left.
        angular.forEach(document.querySelectorAll('.nxf-modal'), function(el) {
            var modal = el.closest('.modal');
            if (modal) angular.element(modal).remove();
        });
    });
});
