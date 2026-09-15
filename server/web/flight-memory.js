(function () {
  'use strict';

  var root = document.getElementById('flight-memory');
  if (!root) return;

  var endpoint = root.getAttribute('data-flight-endpoint');
  var status = document.getElementById('replay-status');
  var playButton = document.getElementById('replay-play');
  var scrubber = document.getElementById('replay-scrubber');
  var speedSelect = document.getElementById('replay-speed');
  var followButton = document.getElementById('replay-follow');
  var profileCanvas = document.getElementById('flight-profile-canvas');
  var fallbackCanvas = document.getElementById('replay-fallback-canvas');
  var cesiumContainer = document.getElementById('replay-3d');
  var fallbackContainer = document.getElementById('replay-fallback');
  var currentTime = document.getElementById('replay-current-time');
  var currentAltitude = document.getElementById('replay-current-altitude');
  var currentSpeed = document.getElementById('replay-current-speed');
  var currentVerticalSpeed = document.getElementById('replay-current-vs');
  var currentPhase = document.getElementById('replay-current-phase');
  var landingSection = document.getElementById('landing-signature');
  var landingCanvas = document.getElementById('landing-signature-canvas');
  var ghostSection = document.getElementById('ghost-landing');
  var ghostCanvas = document.getElementById('ghost-landing-canvas');
  var trafficButton = document.getElementById('replay-traffic');
  var contextSection = document.getElementById('context-journey');
  var contextCanvas = document.getElementById('context-journey-canvas');
  var weatherSection = document.getElementById('weather-journey');

  var points = [];
  var currentIndex = 0;
  var playing = false;
  var playbackTime = 0;
  var lastFrameAt = 0;
  var viewer = null;
  var marker = null;
  var completedRoute = null;
  var routePositions = [];
  var following = false;
  var landingData = null;
  var ghostLandings = [];
  var contextSnapshots = [];
  var trafficMarker = null;
  var showNearbyTraffic = false;

  function setStatus(message, type) {
    status.textContent = message;
    status.className = 'replay-status is-' + (type || 'info');
  }

  function number(value, fallback) {
    if (value === null || value === undefined || value === '') {
      return arguments.length > 1 ? fallback : 0;
    }
    var parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : (arguments.length > 1 ? fallback : 0);
  }

  function normalizeTrack(track) {
    var normalized = (Array.isArray(track) ? track : []).map(function (point) {
      return {
        time: Date.parse(point.TimeUTC),
        timeText: String(point.TimeUTC || ''),
        latitude: number(point.Latitude, NaN),
        longitude: number(point.Longitude, NaN),
        altitudeFt: number(point.AltitudeFt),
        groundSpeedKt: number(point.GroundSpeedKt),
        courseDeg: number(point.CourseDeg),
        verticalSpeedFps: number(point.VerticalSpeedFps)
      };
    }).filter(function (point) {
      return Number.isFinite(point.time) &&
        point.latitude >= -90 && point.latitude <= 90 &&
        point.longitude >= -180 && point.longitude <= 180;
    }).sort(function (a, b) {
      return a.time - b.time;
    });

    normalized = normalized.filter(function (point, index) {
      return index === 0 || point.time > normalized[index - 1].time;
    });

    if (normalized.length <= 5000) return normalized;
    var thinned = [];
    var step = (normalized.length - 1) / 4999;
    for (var i = 0; i < 5000; i++) {
      thinned.push(normalized[Math.round(i * step)]);
    }
    return thinned;
  }

  function formatTime(timestamp) {
    return new Date(timestamp).toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit'
    });
  }

  function elapsedRatio(index) {
    var duration = points[points.length - 1].time - points[0].time;
    if (duration <= 0) return 0;
    return (points[index].time - points[0].time) / duration;
  }

  function nearestPointIndex(timestamp) {
    var low = 0;
    var high = points.length - 1;
    while (low < high) {
      var middle = Math.floor((low + high) / 2);
      if (points[middle].time < timestamp) low = middle + 1;
      else high = middle;
    }
    if (low > 0 && Math.abs(points[low - 1].time - timestamp) < Math.abs(points[low].time - timestamp)) {
      return low - 1;
    }
    return low;
  }

  function flightPhase(point, flight) {
    var takeoff = Date.parse(flight.TakeoffUTC || '');
    var landing = Date.parse(flight.LandingUTC || '');
    if (Number.isFinite(takeoff) && point.time < takeoff) return 'Taxi out';
    if (Number.isFinite(landing) && point.time >= landing) return 'Taxi in';
    if (Number.isFinite(takeoff)) return 'Airborne';
    return 'Recorded';
  }

  function cssColor(name, fallback) {
    var value = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    return value || fallback;
  }

  function fitCanvas(canvas) {
    var rect = canvas.getBoundingClientRect();
    var scale = Math.min(window.devicePixelRatio || 1, 2);
    var width = Math.max(1, Math.round(rect.width * scale));
    var height = Math.max(1, Math.round(rect.height * scale));
    if (canvas.width !== width || canvas.height !== height) {
      canvas.width = width;
      canvas.height = height;
    }
    return {width: width, height: height, scale: scale};
  }

  function drawProfile() {
    if (!profileCanvas || !points.length) return;
    var size = fitCanvas(profileCanvas);
    var ctx = profileCanvas.getContext('2d');
    var padding = {left: 54 * size.scale, right: 16 * size.scale, top: 16 * size.scale, bottom: 28 * size.scale};
    var plotWidth = size.width - padding.left - padding.right;
    var plotHeight = size.height - padding.top - padding.bottom;
    var altitudes = points.map(function (point) { return point.altitudeFt; });
    var minimum = Math.floor((Math.min.apply(null, altitudes) - 200) / 500) * 500;
    var maximum = Math.ceil((Math.max.apply(null, altitudes) + 200) / 500) * 500;
    if (maximum <= minimum) maximum = minimum + 1000;

    ctx.clearRect(0, 0, size.width, size.height);
    ctx.font = (11 * size.scale) + 'px -apple-system, BlinkMacSystemFont, sans-serif';
    ctx.lineWidth = size.scale;
    ctx.strokeStyle = cssColor('--border', '#dfe3ea');
    ctx.fillStyle = cssColor('--muted', '#697386');

    for (var line = 0; line <= 4; line++) {
      var y = padding.top + plotHeight * line / 4;
      var altitude = Math.round(maximum - (maximum - minimum) * line / 4);
      ctx.beginPath();
      ctx.moveTo(padding.left, y);
      ctx.lineTo(size.width - padding.right, y);
      ctx.stroke();
      ctx.fillText(altitude.toLocaleString() + ' ft', 4 * size.scale, y + 4 * size.scale);
    }

    ctx.beginPath();
    points.forEach(function (point, index) {
      var x = padding.left + plotWidth * elapsedRatio(index);
      var y = padding.top + plotHeight * (maximum - point.altitudeFt) / (maximum - minimum);
      if (index === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    });
    ctx.strokeStyle = cssColor('--primary', '#635bff');
    ctx.lineWidth = 2 * size.scale;
    ctx.stroke();

    var currentX = padding.left + plotWidth * elapsedRatio(currentIndex);
    ctx.beginPath();
    ctx.moveTo(currentX, padding.top);
    ctx.lineTo(currentX, padding.top + plotHeight);
    ctx.strokeStyle = cssColor('--accent', '#00a4ef');
    ctx.lineWidth = 2 * size.scale;
    ctx.stroke();

    ctx.fillStyle = cssColor('--muted', '#697386');
    ctx.fillText(formatTime(points[0].time), padding.left, size.height - 7 * size.scale);
    var endLabel = formatTime(points[points.length - 1].time);
    var endWidth = ctx.measureText(endLabel).width;
    ctx.fillText(endLabel, size.width - padding.right - endWidth, size.height - 7 * size.scale);
  }

  function verticalSpeedText(value) {
    var rounded = Math.round(number(value));
    if (rounded < 0) return Math.abs(rounded).toLocaleString() + ' fpm down';
    if (rounded > 0) return rounded.toLocaleString() + ' fpm up';
    return '0 fpm';
  }

  function drawLandingLine(ctx, segment, xFor, yFor, color, width) {
    ctx.beginPath();
    segment.forEach(function (point, index) {
      var x = xFor(number(point.SecondsToTouchdown));
      var y = yFor(point);
      if (index === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    });
    ctx.strokeStyle = color;
    ctx.lineWidth = width;
    ctx.stroke();
  }

  function drawLandingChart() {
    if (!landingCanvas || !landingData || !landingData.available) return;
    var segment = landingData.segment || [];
    if (segment.length < 2) return;
    var size = fitCanvas(landingCanvas);
    var ctx = landingCanvas.getContext('2d');
    var scale = size.scale;
    var padding = {left: 54 * scale, right: 48 * scale, top: 30 * scale, bottom: 30 * scale};
    var width = size.width - padding.left - padding.right;
    var height = size.height - padding.top - padding.bottom;
    var heightField = landingData.height_reference === 'airport_elevation' ? 'HeightAboveAirportFt' : 'AltitudeFt';
    var minSeconds = Math.min.apply(null, segment.map(function (point) { return number(point.SecondsToTouchdown); }));
    var maxSeconds = Math.max.apply(null, segment.map(function (point) { return number(point.SecondsToTouchdown); }));
    var maximumHeight = Math.max(500, Math.ceil(Math.max.apply(null, segment.map(function (point) {
      return Math.max(0, number(point[heightField]));
    })) / 500) * 500);
    var maximumSpeed = Math.max(40, Math.ceil(Math.max.apply(null, segment.map(function (point) {
      return Math.max(0, number(point.GroundSpeedKt));
    })) / 20) * 20);
    var secondsRange = Math.max(1, maxSeconds - minSeconds);
    var xFor = function (seconds) { return padding.left + (seconds - minSeconds) / secondsRange * width; };
    var altitudeY = function (point) {
      return padding.top + height * (1 - Math.max(0, number(point[heightField])) / maximumHeight);
    };
    var speedY = function (point) {
      return padding.top + height * (1 - Math.max(0, number(point.GroundSpeedKt)) / maximumSpeed);
    };

    ctx.clearRect(0, 0, size.width, size.height);
    ctx.font = (11 * scale) + 'px -apple-system, BlinkMacSystemFont, sans-serif';
    ctx.lineWidth = scale;
    for (var line = 0; line <= 4; line++) {
      var y = padding.top + height * line / 4;
      ctx.beginPath();
      ctx.moveTo(padding.left, y);
      ctx.lineTo(size.width - padding.right, y);
      ctx.strokeStyle = cssColor('--border', '#dfe3ea');
      ctx.stroke();
      ctx.fillStyle = cssColor('--muted', '#697386');
      ctx.fillText(Math.round(maximumHeight * (1 - line / 4)).toLocaleString() + ' ft', 4 * scale, y + 4 * scale);
      var speedLabel = Math.round(maximumSpeed * (1 - line / 4)) + ' kt';
      ctx.fillText(speedLabel, size.width - padding.right + 6 * scale, y + 4 * scale);
    }

    var touchdownX = xFor(0);
    ctx.beginPath();
    ctx.moveTo(touchdownX, padding.top);
    ctx.lineTo(touchdownX, padding.top + height);
    ctx.strokeStyle = cssColor('--danger', '#b4234d');
    ctx.setLineDash([4 * scale, 4 * scale]);
    ctx.stroke();
    ctx.setLineDash([]);

    drawLandingLine(ctx, segment, xFor, altitudeY, cssColor('--primary', '#635bff'), 2.5 * scale);
    drawLandingLine(ctx, segment, xFor, speedY, cssColor('--accent', '#00a4ef'), 2 * scale);

    ctx.fillStyle = cssColor('--primary', '#635bff');
    ctx.fillText(heightField === 'HeightAboveAirportFt' ? 'HEIGHT ABOVE AIRPORT' : 'GPS ALTITUDE MSL', padding.left, 15 * scale);
    ctx.fillStyle = cssColor('--accent', '#00a4ef');
    ctx.fillText('GROUNDSPEED', padding.left + 150 * scale, 15 * scale);
    ctx.fillStyle = cssColor('--muted', '#697386');
    ctx.fillText(Math.round(Math.abs(minSeconds) / 60) + ' min before', padding.left, size.height - 7 * scale);
    var touchdownLabel = 'TOUCHDOWN';
    var touchdownWidth = ctx.measureText(touchdownLabel).width;
    ctx.fillText(touchdownLabel, Math.min(size.width - padding.right - touchdownWidth, touchdownX - touchdownWidth / 2), size.height - 7 * scale);
  }

  function drawGhostChart() {
    if (!ghostCanvas || !landingData || !landingData.available || !ghostLandings.length) return;
    var current = (landingData.segment || []).filter(function (point) {
      return number(point.SecondsToTouchdown) >= -180 && number(point.SecondsToTouchdown) <= 15;
    });
    var histories = ghostLandings.map(function (ghost) {
      return (ghost.segment || []).filter(function (point) {
        return number(point.SecondsToTouchdown) >= -180 && number(point.SecondsToTouchdown) <= 15;
      });
    }).filter(function (segment) { return segment.length > 1; });
    if (current.length < 2 || !histories.length) return;
    var useAirportHeight = landingData.height_reference === 'airport_elevation' &&
      ghostLandings.every(function (ghost) { return ghost.height_reference === 'airport_elevation'; });
    var heightField = useAirportHeight ? 'HeightAboveAirportFt' : 'AltitudeFt';

    var size = fitCanvas(ghostCanvas);
    var ctx = ghostCanvas.getContext('2d');
    var scale = size.scale;
    var padding = {left: 52 * scale, right: 16 * scale, top: 28 * scale, bottom: 28 * scale};
    var width = size.width - padding.left - padding.right;
    var height = size.height - padding.top - padding.bottom;
    var allPoints = current.concat.apply(current, histories);
    var maximumHeight = Math.max(500, Math.ceil(Math.max.apply(null, allPoints.map(function (point) {
      return Math.max(0, number(point[heightField]));
    })) / 500) * 500);
    var xFor = function (seconds) { return padding.left + (seconds + 180) / 195 * width; };
    var yFor = function (point) {
      return padding.top + height * (1 - Math.max(0, number(point[heightField])) / maximumHeight);
    };

    ctx.clearRect(0, 0, size.width, size.height);
    ctx.font = (11 * scale) + 'px -apple-system, BlinkMacSystemFont, sans-serif';
    for (var line = 0; line <= 3; line++) {
      var y = padding.top + height * line / 3;
      ctx.beginPath();
      ctx.moveTo(padding.left, y);
      ctx.lineTo(size.width - padding.right, y);
      ctx.strokeStyle = cssColor('--border', '#dfe3ea');
      ctx.lineWidth = scale;
      ctx.stroke();
      ctx.fillStyle = cssColor('--muted', '#697386');
      ctx.fillText(Math.round(maximumHeight * (1 - line / 3)).toLocaleString() + ' ft', 3 * scale, y + 4 * scale);
    }

    var ghostColors = ['#9aa4b2', '#c0c6d0', '#7d8796'];
    histories.forEach(function (segment, index) {
      drawLandingLine(ctx, segment, xFor, yFor, ghostColors[index % ghostColors.length], 1.5 * scale);
    });
    drawLandingLine(ctx, current, xFor, yFor, cssColor('--primary', '#635bff'), 3 * scale);

    ctx.fillStyle = cssColor('--primary', '#635bff');
    ctx.fillText('CURRENT LANDING', padding.left, 14 * scale);
    ctx.fillStyle = cssColor('--muted', '#697386');
    ctx.fillText('PREVIOUS LANDINGS', padding.left + 130 * scale, 14 * scale);
    ctx.fillText('3 min before', padding.left, size.height - 7 * scale);
    var touchdownLabel = 'TOUCHDOWN';
    var touchdownWidth = ctx.measureText(touchdownLabel).width;
    ctx.fillText(touchdownLabel, size.width - padding.right - touchdownWidth, size.height - 7 * scale);
  }

  function fillList(elementId, values) {
    var list = document.getElementById(elementId);
    list.textContent = '';
    (values || []).forEach(function (value) {
      var item = document.createElement('li');
      item.textContent = value;
      list.appendChild(item);
    });
  }

  function renderLanding(analysis, ghosts) {
    landingData = analysis || {available: false};
    ghostLandings = Array.isArray(ghosts) ? ghosts : [];
    landingSection.hidden = false;
    var confidence = document.getElementById('landing-confidence');

    if (!landingData.available) {
      confidence.textContent = 'Not enough data';
      confidence.className = 'landing-confidence is-low';
      fillList('landing-observations', [landingData.reason || 'This landing could not be reconstructed.']);
      fillList('landing-limitations', ['No landing metrics are estimated when the track is incomplete.']);
      document.querySelector('.landing-metrics').hidden = true;
      document.querySelector('.landing-chart-wrap').hidden = true;
      ghostSection.hidden = false;
      document.getElementById('ghost-empty').hidden = false;
      return;
    }

    confidence.textContent = landingData.confidence.charAt(0).toUpperCase() + landingData.confidence.slice(1) + ' data confidence';
    confidence.className = 'landing-confidence is-' + landingData.confidence;
    document.getElementById('landing-touchdown-time').textContent = formatTime(Date.parse(landingData.touchdown.time_utc));
    document.getElementById('landing-touchdown-vs').textContent = verticalSpeedText(landingData.touchdown.vertical_speed_fpm);
    document.getElementById('landing-touchdown-speed').textContent = Math.round(landingData.touchdown.groundspeed_kt) + ' kt';
    document.getElementById('landing-average-vs').textContent = verticalSpeedText(landingData.final_minute.average_vertical_speed_fpm);
    fillList('landing-observations', landingData.observations);
    fillList('landing-limitations', landingData.limitations);
    drawLandingChart();

    ghostSection.hidden = false;
    var hasGhosts = ghostLandings.length > 0;
    document.getElementById('ghost-empty').hidden = hasGhosts;
    document.getElementById('ghost-content').hidden = !hasGhosts;
    if (hasGhosts) {
      var comparisons = document.getElementById('ghost-comparisons');
      comparisons.textContent = '';
      ghostLandings.forEach(function (ghost) {
        var card = document.createElement('div');
        card.className = 'ghost-comparison';
        var title = document.createElement('strong');
        title.textContent = new Date(ghost.date_utc).toLocaleDateString() + ' · ' + ghost.route;
        var metrics = document.createElement('span');
        metrics.textContent = verticalSpeedText(ghost.touchdown.vertical_speed_fpm) + ' · ' +
          Math.round(ghost.touchdown.groundspeed_kt) + ' kt · ' + ghost.confidence + ' confidence';
        card.appendChild(title);
        card.appendChild(metrics);
        comparisons.appendChild(card);
      });
      drawGhostChart();
    }
  }

  function normalizeContext(context) {
    return (Array.isArray(context) ? context : []).map(function (snapshot) {
      var copy = Object.assign({}, snapshot);
      copy.time = Date.parse(snapshot.TimeUTC || '');
      return copy;
    }).filter(function (snapshot) {
      return Number.isFinite(snapshot.time);
    }).sort(function (a, b) {
      return a.time - b.time;
    });
  }

  function contextAt(timestamp) {
    if (!contextSnapshots.length) return null;
    var nearest = contextSnapshots[0];
    var distance = Math.abs(nearest.time - timestamp);
    contextSnapshots.forEach(function (snapshot) {
      var candidate = Math.abs(snapshot.time - timestamp);
      if (candidate < distance) {
        nearest = snapshot;
        distance = candidate;
      }
    });
    return distance <= 60000 ? nearest : null;
  }

  function updateTrafficContext(timestamp) {
    if (!viewer || !trafficMarker) return;
    var snapshot = contextAt(timestamp);
    var target = snapshot && snapshot.NearestTraffic;
    trafficMarker.show = !!(showNearbyTraffic && target);
    if (trafficMarker.show) {
      trafficMarker.position = Cesium.Cartesian3.fromDegrees(
        number(target.Longitude),
        number(target.Latitude),
        Math.max(0, number(target.AltitudeFt) * 0.3048)
      );
    }
  }

  function drawContextChart() {
    if (!contextCanvas || contextSnapshots.length < 2) return;
    var size = fitCanvas(contextCanvas);
    var ctx = contextCanvas.getContext('2d');
    var scale = size.scale;
    var padding = {left: 48 * scale, right: 42 * scale, top: 28 * scale, bottom: 28 * scale};
    var width = size.width - padding.left - padding.right;
    var height = size.height - padding.top - padding.bottom;
    var start = contextSnapshots[0].time;
    var end = contextSnapshots[contextSnapshots.length - 1].time;
    var duration = Math.max(1, end - start);
    var maxMessages = Math.max(10, Math.max.apply(null, contextSnapshots.map(function (item) {
      return number(item.UATMsgPerMinute) + number(item.ESMsgPerMinute);
    })));
    var maxTraffic = Math.max(1, Math.max.apply(null, contextSnapshots.map(function (item) {
      return number(item.ActiveTraffic);
    })));
    var xFor = function (item) { return padding.left + (item.time - start) / duration * width; };
    var messagesY = function (item) {
      return padding.top + height * (1 - (number(item.UATMsgPerMinute) + number(item.ESMsgPerMinute)) / maxMessages);
    };
    var trafficY = function (item) {
      return padding.top + height * (1 - number(item.ActiveTraffic) / maxTraffic);
    };

    ctx.clearRect(0, 0, size.width, size.height);
    ctx.font = (11 * scale) + 'px -apple-system, BlinkMacSystemFont, sans-serif';
    contextSnapshots.forEach(function (item, index) {
      if (item.GPSValid || index >= contextSnapshots.length - 1) return;
      var next = contextSnapshots[index + 1];
      ctx.fillStyle = 'rgba(232,145,45,.10)';
      ctx.fillRect(xFor(item), padding.top, Math.max(2 * scale, xFor(next) - xFor(item)), height);
    });
    for (var line = 0; line <= 3; line++) {
      var y = padding.top + height * line / 3;
      ctx.beginPath();
      ctx.moveTo(padding.left, y);
      ctx.lineTo(size.width - padding.right, y);
      ctx.strokeStyle = cssColor('--border', '#dfe3ea');
      ctx.lineWidth = scale;
      ctx.stroke();
    }
    ctx.beginPath();
    contextSnapshots.forEach(function (item, index) {
      var x = xFor(item);
      var y = messagesY(item);
      if (index === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    });
    ctx.strokeStyle = cssColor('--primary', '#635bff');
    ctx.lineWidth = 2.5 * scale;
    ctx.stroke();
    ctx.beginPath();
    contextSnapshots.forEach(function (item, index) {
      var x = xFor(item);
      var y = trafficY(item);
      if (index === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    });
    ctx.strokeStyle = cssColor('--accent', '#00a4ef');
    ctx.lineWidth = 2 * scale;
    ctx.stroke();

    ctx.fillStyle = cssColor('--primary', '#635bff');
    ctx.fillText('ADS-B MESSAGES / MIN', padding.left, 14 * scale);
    ctx.fillStyle = cssColor('--accent', '#00a4ef');
    ctx.fillText('NEARBY TRAFFIC', padding.left + 160 * scale, 14 * scale);
    ctx.fillStyle = cssColor('--muted', '#697386');
    ctx.fillText(formatTime(start), padding.left, size.height - 7 * scale);
    var endLabel = formatTime(end);
    ctx.fillText(endLabel, size.width - padding.right - ctx.measureText(endLabel).width, size.height - 7 * scale);
  }

  function renderContext(context, weather) {
    contextSnapshots = normalizeContext(context);
    if (contextSnapshots.length) {
      contextSection.hidden = false;
      var validGPS = contextSnapshots.filter(function (item) { return !!item.GPSValid; }).length;
      var online = contextSnapshots.filter(function (item) { return !!item.WANOnline; }).length;
      var messages = contextSnapshots.map(function (item) {
        return number(item.UATMsgPerMinute) + number(item.ESMsgPerMinute);
      });
      var peakTraffic = Math.max.apply(null, contextSnapshots.map(function (item) { return number(item.ActiveTraffic); }));
      document.getElementById('context-gps').textContent = Math.round(validGPS / contextSnapshots.length * 100) + '% of snapshots';
      document.getElementById('context-reception').textContent = Math.round(messages.reduce(function (sum, value) { return sum + value; }, 0) / messages.length) + ' msg/min avg';
      document.getElementById('context-traffic').textContent = peakTraffic + ' targets';
      document.getElementById('context-internet').textContent = Math.round(online / contextSnapshots.length * 100) + '% of snapshots';
      var hasTraffic = contextSnapshots.some(function (item) { return !!item.NearestTraffic; });
      trafficButton.hidden = !hasTraffic;
      drawContextChart();
    }

    if (weather && number(weather.CaptureVersion) > 0) {
      weatherSection.hidden = false;
      var summary = document.getElementById('weather-summary');
      summary.textContent = '';
      var entries = Object.keys(weather.Summary || {}).sort();
      if (!entries.length) {
        var empty = document.createElement('div');
        empty.className = 'weather-empty';
        empty.textContent = 'No FIS-B weather products were captured during this flight.';
        summary.appendChild(empty);
      } else {
        entries.forEach(function (type) {
          var badge = document.createElement('div');
          var value = document.createElement('strong');
          var label = document.createElement('span');
          value.textContent = number(weather.Summary[type]).toLocaleString();
          label.textContent = type;
          badge.appendChild(value);
          badge.appendChild(label);
          summary.appendChild(badge);
        });
      }
      var reports = document.getElementById('weather-reports');
      reports.textContent = '';
      (weather.TextReports || []).slice(-30).reverse().forEach(function (report) {
        var item = document.createElement('article');
        var head = document.createElement('div');
        var title = document.createElement('strong');
        var time = document.createElement('span');
        var body = document.createElement('p');
        title.textContent = report.Type + ' · ' + report.Location;
        time.textContent = new Date(report.ReceivedUTC).toLocaleTimeString();
        body.textContent = report.Data;
        head.appendChild(title);
        head.appendChild(time);
        item.appendChild(head);
        item.appendChild(body);
        reports.appendChild(item);
      });
    }
  }

  function drawFallback() {
    if (!fallbackCanvas || !points.length) return;
    var size = fitCanvas(fallbackCanvas);
    var ctx = fallbackCanvas.getContext('2d');
    var padding = 24 * size.scale;
    var latitudes = points.map(function (point) { return point.latitude; });
    var longitudes = points.map(function (point) { return point.longitude; });
    var minLat = Math.min.apply(null, latitudes);
    var maxLat = Math.max.apply(null, latitudes);
    var minLon = Math.min.apply(null, longitudes);
    var maxLon = Math.max.apply(null, longitudes);
    var latRange = Math.max(0.001, maxLat - minLat);
    var lonRange = Math.max(0.001, maxLon - minLon);

    function xy(point) {
      return {
        x: padding + (point.longitude - minLon) / lonRange * (size.width - padding * 2),
        y: size.height - padding - (point.latitude - minLat) / latRange * (size.height - padding * 2)
      };
    }

    ctx.clearRect(0, 0, size.width, size.height);
    ctx.fillStyle = cssColor('--map-bg', '#111b2b');
    ctx.fillRect(0, 0, size.width, size.height);
    ctx.beginPath();
    points.forEach(function (point, index) {
      var position = xy(point);
      if (index === 0) ctx.moveTo(position.x, position.y);
      else ctx.lineTo(position.x, position.y);
    });
    ctx.strokeStyle = cssColor('--route', '#48b7ff');
    ctx.lineWidth = 3 * size.scale;
    ctx.stroke();

    var markerPosition = xy(points[currentIndex]);
    ctx.beginPath();
    ctx.arc(markerPosition.x, markerPosition.y, 6 * size.scale, 0, Math.PI * 2);
    ctx.fillStyle = '#ffffff';
    ctx.fill();
    ctx.strokeStyle = cssColor('--route', '#48b7ff');
    ctx.lineWidth = 3 * size.scale;
    ctx.stroke();
  }

  function updateReplay(flight) {
    if (!points.length) return;
    var point = points[currentIndex];
    scrubber.value = String(Math.round((point.time - points[0].time) / 1000));
    currentTime.textContent = formatTime(point.time);
    currentAltitude.textContent = Math.round(point.altitudeFt).toLocaleString() + ' ft';
    currentSpeed.textContent = Math.round(point.groundSpeedKt) + ' kt';
    currentVerticalSpeed.textContent = Math.round(point.verticalSpeedFps * 60) + ' fpm';
    currentPhase.textContent = flightPhase(point, flight);

    if (viewer && marker) {
      marker.position = routePositions[currentIndex];
      completedRoute.polyline.positions = routePositions.slice(0, currentIndex + 1);
      updateTrafficContext(point.time);
      viewer.scene.requestRender();
    }
    drawProfile();
    drawFallback();
  }

  function initCesium() {
    if (!window.Cesium || !window.WebGLRenderingContext) {
      throw new Error('3D rendering is not supported');
    }

    viewer = new Cesium.Viewer(cesiumContainer, {
      baseLayer: false,
      terrainProvider: new Cesium.EllipsoidTerrainProvider(),
      baseLayerPicker: false,
      geocoder: false,
      homeButton: false,
      sceneModePicker: false,
      navigationHelpButton: false,
      animation: false,
      timeline: false,
      fullscreenButton: false,
      infoBox: false,
      selectionIndicator: false,
      requestRenderMode: true,
      maximumRenderTimeChange: Infinity
    });
    viewer.resolutionScale = Math.min(window.devicePixelRatio || 1, 1.25);
    viewer.scene.globe.baseColor = Cesium.Color.fromCssColorString('#152236');

    try {
      viewer.imageryLayers.addImageryProvider(new Cesium.UrlTemplateImageryProvider({
        url: 'https://tile.openstreetmap.org/{z}/{x}/{y}.png',
        maximumLevel: 18,
        credit: new Cesium.Credit('© OpenStreetMap contributors')
      }));
    } catch (imageryError) {
      setStatus('Basemap unavailable. The 3D flight path is still available.', 'warning');
    }

    routePositions = points.map(function (point) {
      return Cesium.Cartesian3.fromDegrees(point.longitude, point.latitude, Math.max(0, point.altitudeFt * 0.3048));
    });

    viewer.entities.add({
      polyline: {
        positions: routePositions,
        width: 3,
        material: Cesium.Color.fromCssColorString('#7180ff').withAlpha(0.4),
        arcType: Cesium.ArcType.NONE
      }
    });
    completedRoute = viewer.entities.add({
      polyline: {
        positions: [routePositions[0]],
        width: 5,
        material: Cesium.Color.fromCssColorString('#27b5ff'),
        arcType: Cesium.ArcType.NONE
      }
    });
    marker = viewer.entities.add({
      position: routePositions[0],
      point: {
        pixelSize: 12,
        color: Cesium.Color.WHITE,
        outlineColor: Cesium.Color.fromCssColorString('#27b5ff'),
        outlineWidth: 4,
        disableDepthTestDistance: Number.POSITIVE_INFINITY
      }
    });
    trafficMarker = viewer.entities.add({
      show: false,
      point: {
        pixelSize: 11,
        color: Cesium.Color.fromCssColorString('#f79009'),
        outlineColor: Cesium.Color.WHITE,
        outlineWidth: 2,
        disableDepthTestDistance: Number.POSITIVE_INFINITY
      }
    });

    var sphere = Cesium.BoundingSphere.fromPoints(routePositions);
    viewer.camera.flyToBoundingSphere(sphere, {
      duration: 0,
      offset: new Cesium.HeadingPitchRange(0, -0.65, Math.max(sphere.radius * 2.2, 5000))
    });
    viewer.scene.requestRender();
  }

  function frame(timestamp, flight) {
    if (!playing) return;
    if (!lastFrameAt) lastFrameAt = timestamp;
    var delta = timestamp - lastFrameAt;
    lastFrameAt = timestamp;
    playbackTime += delta * number(speedSelect.value, 20);

    var targetTime = points[0].time + playbackTime;
    while (currentIndex < points.length - 1 && points[currentIndex + 1].time <= targetTime) {
      currentIndex++;
    }
    updateReplay(flight);
    if (currentIndex >= points.length - 1) {
      playing = false;
      playButton.textContent = 'Replay';
      lastFrameAt = 0;
      return;
    }
    requestAnimationFrame(function (nextTimestamp) { frame(nextTimestamp, flight); });
  }

  function wireControls(flight) {
    scrubber.max = String(Math.max(1, Math.round((points[points.length - 1].time - points[0].time) / 1000)));
    scrubber.value = '0';

    playButton.addEventListener('click', function () {
      if (currentIndex >= points.length - 1) {
        currentIndex = 0;
        playbackTime = 0;
        updateReplay(flight);
      }
      playing = !playing;
      playButton.textContent = playing ? 'Pause' : 'Play';
      lastFrameAt = 0;
      if (playing) requestAnimationFrame(function (timestamp) { frame(timestamp, flight); });
    });

    scrubber.addEventListener('input', function () {
      currentIndex = nearestPointIndex(points[0].time + Number(scrubber.value) * 1000);
      playbackTime = points[currentIndex].time - points[0].time;
      updateReplay(flight);
    });

    followButton.addEventListener('click', function () {
      if (!viewer || !marker) return;
      following = !following;
      viewer.trackedEntity = following ? marker : undefined;
      followButton.textContent = following ? 'Stop following' : 'Follow aircraft';
      followButton.classList.toggle('is-active', following);
    });
    trafficButton.addEventListener('click', function () {
      showNearbyTraffic = !showNearbyTraffic;
      trafficButton.textContent = showNearbyTraffic ? 'Hide nearby traffic' : 'Show nearby traffic';
      trafficButton.classList.toggle('is-active', showNearbyTraffic);
      updateTrafficContext(points[currentIndex].time);
      if (viewer) viewer.scene.requestRender();
    });

    window.addEventListener('resize', function () {
      drawProfile();
      drawFallback();
      drawLandingChart();
      drawGhostChart();
      drawContextChart();
    });
  }

  fetch(endpoint, {
    credentials: 'same-origin',
    cache: 'no-store',
    headers: {'Accept': 'application/json'}
  }).then(function (response) {
    if (!response.ok) throw new Error(response.status === 401 ? 'Sign in again to view this flight.' : 'Flight data could not be loaded.');
    return response.json();
  }).then(function (data) {
    var flight = data.flight || {};
    points = normalizeTrack(flight.Track);
    if (points.length < 2) throw new Error('This flight does not contain enough valid track points.');
    renderLanding(data.landing, data.ghost_landings);
    renderContext(flight.Context, flight.Weather);

    scrubber.disabled = false;
    playButton.disabled = false;
    speedSelect.disabled = false;
    setStatus(points.length.toLocaleString() + ' track points loaded', 'success');

    try {
      initCesium();
      fallbackContainer.hidden = true;
      followButton.disabled = false;
    } catch (error) {
      cesiumContainer.hidden = true;
      fallbackContainer.hidden = false;
      followButton.hidden = true;
      setStatus('3D is unavailable on this device. Showing the lightweight route replay.', 'warning');
    }

    wireControls(flight);
    updateReplay(flight);
  }).catch(function (error) {
    setStatus(error.message || 'Flight replay could not be loaded.', 'error');
    playButton.disabled = true;
    scrubber.disabled = true;
    speedSelect.disabled = true;
  });
}());
