appControllers.controller('FlightLogCtrl', function($scope, $http, $interval) {
    $scope.data = {
        Phase: 'PARKED',
        GPS: {
            Valid:false,
            TimeUTC:'',
            Latitude:0,
            Longitude:0,
            GroundSpeedKt:0,
            AltitudeFt:0,
            CourseDeg:0,
            VerticalSpeedFps:0,
            HorizontalAccuracyM:0
        },
        Current: null,
        CurrentTrack: [],
        CurrentAirport: null,
        Flights: [],
        Settings: {Timezone:'UTC', AutoDetect:true},
        AirportDatabaseReady: false,
        AirportCount: 0,
        StoragePath: ''
    };
    $scope.settings = {Timezone:'UTC', AutoDetect:true};
    $scope.errorMessage = '';
    $scope.savingSettings = false;
    $scope.selectedFlight = null;
    $scope.currentTrackPath = '';
    $scope.selectedTrackPath = '';
    $scope.browserTimezone = '';
    var settingsLoaded = false;

    try {
        if (window.Intl && Intl.DateTimeFormat) {
            $scope.browserTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone || '';
        }
    } catch (e) {}

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
        return ($scope.settings && $scope.settings.Timezone) || 'UTC';
    }

    function localParts(value, withDate) {
        var d = validDate(value);
        if (!d) return '--';
        try {
            var opts = withDate ? {
                timeZone: timezone(), year:'numeric', month:'short', day:'2-digit',
                hour:'2-digit', minute:'2-digit', second:'2-digit', hour12:false
            } : {
                timeZone: timezone(), hour:'2-digit', minute:'2-digit', second:'2-digit', hour12:false
            };
            return new Intl.DateTimeFormat('en-CA', opts).format(d);
        } catch (e) {
            return withDate ? d.toLocaleString() : d.toLocaleTimeString();
        }
    }

    $scope.formatZuluClock = function(value) {
        var d = validDate(value);
        if (!d) return '--:--:--Z';
        return pad2(d.getUTCHours()) + ':' + pad2(d.getUTCMinutes()) + ':' + pad2(d.getUTCSeconds()) + 'Z';
    };

    $scope.formatZuluDate = function(value) {
        var d = validDate(value);
        if (!d) return 'Waiting for GPS time';
        return d.getUTCFullYear() + '-' + pad2(d.getUTCMonth()+1) + '-' + pad2(d.getUTCDate());
    };

    $scope.formatZulu = function(value) {
        var d = validDate(value);
        if (!d) return '--';
        return $scope.formatZuluDate(value) + ' ' + $scope.formatZuluClock(value);
    };

    $scope.formatLocalClock = function(value) { return localParts(value, false); };
    $scope.formatLocal = function(value) { return value ? localParts(value, true) : '--'; };
    $scope.formatLocalDate = function(value) {
        var d = validDate(value);
        if (!d) return '--';
        try {
            return new Intl.DateTimeFormat('en-CA', {timeZone:timezone(), year:'numeric', month:'short', day:'2-digit'}).format(d);
        } catch (e) { return d.toLocaleDateString(); }
    };

    $scope.dualTime = function(value) {
        if (!value) return '--';
        return $scope.formatZulu(value) + ' · ' + $scope.formatLocal(value);
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
    $scope.formatNumber = function(value, decimals) {
        var n = Number(value || 0);
        return isFinite(n) ? n.toFixed(decimals || 0) : '--';
    };
    $scope.formatInteger = function(value) {
        var n = Number(value || 0);
        return isFinite(n) ? Math.round(n).toLocaleString() : '--';
    };

    $scope.formatVerticalSpeed = function(fps) {
        var fpm = Number(fps || 0) * 60;
        if (!isFinite(fpm)) return '--';
        var rounded = Math.round(fpm / 10) * 10;
        if (Math.abs(rounded) < 10) rounded = 0;
        return (rounded > 0 ? '+' : '') + rounded.toLocaleString() + ' fpm';
    };

    $scope.verticalSpeedClass = function(fps) {
        var fpm = Number(fps || 0) * 60;
        if (fpm > 100) return 'climb';
        if (fpm < -100) return 'descent';
        return 'level';
    };

    $scope.verticalSpeedHint = function(fps) {
        var fpm = Number(fps || 0) * 60;
        if (fpm > 100) return 'Climbing';
        if (fpm < -100) return 'Descending';
        return 'Level / stable';
    };

    $scope.formatCourse = function(value) {
        var deg = Number(value || 0);
        if (!isFinite(deg)) return '---°';
        deg = ((deg % 360) + 360) % 360;
        var rounded = Math.round(deg);
        if (rounded === 0) rounded = 360;
        var text = String(rounded);
        while (text.length < 3) text = '0' + text;
        return text + '°T';
    };

    $scope.cardinalCourse = function(value) {
        var deg = Number(value || 0);
        if (!isFinite(deg)) return 'Waiting for track';
        deg = ((deg % 360) + 360) % 360;
        var names = ['N','NE','E','SE','S','SW','W','NW'];
        return names[Math.round(deg / 45) % 8] + ' · GPS true course';
    };

    $scope.formatAccuracy = function(value) {
        var m = Number(value || 0);
        if (!isFinite(m) || m <= 0) return '--';
        if (m < 10) return m.toFixed(1) + ' m';
        return Math.round(m) + ' m';
    };

    $scope.accuracyClass = function(value) {
        var m = Number(value || 0);
        if (!isFinite(m) || m <= 0) return 'accuracy-unknown';
        if (m <= 15) return 'accuracy-good';
        if (m <= 50) return 'accuracy-warn';
        return 'accuracy-bad';
    };

    $scope.accuracyHint = function(value) {
        var m = Number(value || 0);
        if (!isFinite(m) || m <= 0) return 'Accuracy unavailable';
        if (m <= 15) return 'Strong position solution';
        if (m <= 50) return 'Usable position solution';
        return 'Low precision';
    };

    $scope.formatCoordinate = function(value, axis) {
        var n = Number(value);
        if (!isFinite(n)) return '--';
        var suffix;
        if (axis === 'lat') suffix = n >= 0 ? 'N' : 'S';
        else suffix = n >= 0 ? 'E' : 'W';
        return Math.abs(n).toFixed(6) + '° ' + suffix;
    };

    $scope.airportCode = function(a) { return a && a.Code ? a.Code : '---'; };
    $scope.airportName = function(a) {
        if (!a) return '';
        var parts = [];
        if (a.Name) parts.push(a.Name);
        if (a.Municipality) parts.push(a.Municipality);
        return parts.join(' · ');
    };

    $scope.airportDistance = function(a) {
        if (!a) return 'No nearby airport resolved';
        var d = Number(a.DistanceNM || 0);
        if (d > 0) return d.toFixed(1) + ' NM · ' + (a.Name || 'airport');
        return a.Name || 'Airport resolved';
    };

    $scope.phaseLabel = function(phase) {
        switch (phase) {
            case 'TAXI_OUT': return 'Taxi out';
            case 'AIRBORNE': return 'Airborne';
            case 'TAXI_IN': return 'Taxi in';
            default: return 'Parked';
        }
    };

    $scope.phaseHint = function(phase) {
        switch (phase) {
            case 'TAXI_OUT': return 'Flight session started; waiting for takeoff';
            case 'AIRBORNE': return 'Air time is running';
            case 'TAXI_IN': return 'Landing detected; waiting for parking';
            default: return 'Waiting for sustained movement';
        }
    };

    $scope.phaseClass = function(phase) {
        if (phase === 'AIRBORNE') return 'airborne';
        if (phase === 'TAXI_OUT' || phase === 'TAXI_IN') return 'taxi';
        return 'parked';
    };

    function buildTrack(points) {
        points = points || [];
        if (points.length < 2) return {path:'', start:null, end:null};
        var minLat=Infinity,maxLat=-Infinity,minLon=Infinity,maxLon=-Infinity;
        angular.forEach(points, function(p){
            var lat=Number(p.Latitude), lon=Number(p.Longitude);
            if (!isFinite(lat) || !isFinite(lon)) return;
            minLat=Math.min(minLat,lat); maxLat=Math.max(maxLat,lat);
            minLon=Math.min(minLon,lon); maxLon=Math.max(maxLon,lon);
        });
        var latSpan=Math.max(0.001,maxLat-minLat), lonSpan=Math.max(0.001,maxLon-minLon);
        var pad=35, w=1000-pad*2, h=360-pad*2;
        var coords=[];
        angular.forEach(points,function(p){
            var x=pad+((Number(p.Longitude)-minLon)/lonSpan)*w;
            var y=pad+(1-((Number(p.Latitude)-minLat)/latSpan))*h;
            coords.push({x:x,y:y});
        });
        var str=coords.map(function(c){return c.x.toFixed(1)+','+c.y.toFixed(1);}).join(' ');
        return {path:str,start:coords[0],end:coords[coords.length-1]};
    }

    function updateCurrentTrack() {
        var t=buildTrack($scope.data.CurrentTrack || []);
        $scope.currentTrackPath=t.path;
        $scope.currentTrackStart=t.start;
        $scope.currentTrackEnd=t.end;
    }

    function updateSelectedTrack() {
        var t=buildTrack(($scope.selectedFlight && $scope.selectedFlight.Track) || []);
        $scope.selectedTrackPath=t.path;
        $scope.selectedTrackStart=t.start;
        $scope.selectedTrackEnd=t.end;
    }

    $scope.refresh = function() {
        $http.get('/flightLog', {cache:false}).then(function(response) {
            var d=response.data || {};
            d.GPS=d.GPS || {Valid:false};
            d.Flights=d.Flights || [];
            d.CurrentTrack=d.CurrentTrack || [];
            $scope.data=d;
            if (!settingsLoaded && d.Settings) {
                $scope.settings=angular.copy(d.Settings);
                settingsLoaded=true;
            }
            updateCurrentTrack();
            $scope.errorMessage='';
        }, function(response) {
            $scope.errorMessage='Unable to read the flight logger: ' + ((response && response.data) || 'service unavailable');
        });
    };

    $scope.useBrowserTimezone = function() {
        if ($scope.browserTimezone) $scope.settings.Timezone=$scope.browserTimezone;
    };

    $scope.saveSettings = function() {
        $scope.savingSettings=true;
        $http.post('/flightLog/settings', {
            Timezone: ($scope.settings.Timezone || 'UTC').trim(),
            AutoDetect: !!$scope.settings.AutoDetect
        }).then(function() {
            $scope.savingSettings=false;
            $scope.errorMessage='';
            if (window.StratuxUI) window.StratuxUI.toast('Flight Log settings saved','success',1800);
            $scope.refresh();
        }, function(response) {
            $scope.savingSettings=false;
            $scope.errorMessage='Could not save Flight Log settings: ' + ((response && response.data) || 'unknown error');
        });
    };

    $scope.selectFlight = function(flight) {
        if (!flight || !flight.ID) return;
        $http.get('/flightLog/flight?id=' + encodeURIComponent(flight.ID), {cache:false}).then(function(response) {
            $scope.selectedFlight=response.data || null;
            updateSelectedTrack();
            window.setTimeout(function(){
                var el=document.querySelector('.flightlog-panel:last-of-type');
                if (el && el.scrollIntoView) el.scrollIntoView({behavior:'smooth',block:'start'});
            },50);
        }, function(response) {
            $scope.errorMessage='Could not open this flight: ' + ((response && response.data) || 'unknown error');
        });
    };

    $scope.closeSelected = function() {
        $scope.selectedFlight=null;
        $scope.selectedTrackPath='';
    };

    $scope.finishFlight = function() {
        if (!window.confirm('Finish the current flight now? Stratux will use the current GPS time as the end of the flight.')) return;
        $http.post('/flightLog/action', {action:'finish'}).then(function(){
            if (window.StratuxUI) window.StratuxUI.toast('Flight saved','success',1800);
            $scope.refresh();
        }, function(response){
            $scope.errorMessage='Could not finish the flight: ' + ((response && response.data) || 'unknown error');
        });
    };

    $scope.discardFlight = function() {
        if (!window.confirm('Discard the current flight? This removes the unsaved current track.')) return;
        $http.post('/flightLog/action', {action:'discard'}).then(function(){
            $scope.refresh();
        }, function(response){
            $scope.errorMessage='Could not discard the flight: ' + ((response && response.data) || 'unknown error');
        });
    };

    $scope.refresh();
    var timer=$interval($scope.refresh,1000);
    $scope.$on('$destroy',function(){ $interval.cancel(timer); });
});
