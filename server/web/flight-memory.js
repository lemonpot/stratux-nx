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
  var resetViewButton = document.getElementById('replay-reset-view');
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
  var routeSphere = null;
  var following = false;
  var landingData = null;
  var ghostLandings = [];
  var contextSnapshots = [];
  var trafficMarker = null;
  var showNearbyTraffic = false;
  var routeMinimumAltitudeFt = 0;
  var verticalExaggeration = 8;

  function visualAltitudeMeters(altitudeFt) {
    return 30 + Math.max(0, number(altitudeFt) - routeMinimumAltitudeFt) * 0.3048 * verticalExaggeration;
  }

  function visualPosition(longitude, latitude, altitudeFt) {
    return Cesium.Cartesian3.fromDegrees(longitude, latitude, visualAltitudeMeters(altitudeFt));
  }

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

    // Stratux NX tracks carry no GPS vertical speed; derive it from altitude over about 10 s.
    if (!normalized.some(function (point) { return point.verticalSpeedFps !== 0; })) {
      normalized.forEach(function (point, index) {
        var before = normalized[Math.max(0, index - 5)];
        var after = normalized[Math.min(normalized.length - 1, index + 5)];
        var seconds = (after.time - before.time) / 1000;
        point.verticalSpeedFps = seconds > 0 && seconds <= 30 ? (after.altitudeFt - before.altitudeFt) / seconds : 0;
      });
    }

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

  function landingWindow(segment) {
    return (segment || []).filter(function (point) {
      var seconds = number(point.SecondsToTouchdown);
      return seconds >= -180 && seconds <= 75;
    });
  }

  function drawLandingMarker(ctx, x, top, bottom, label, labelY, color, size) {
    ctx.beginPath();
    ctx.moveTo(x, top);
    ctx.lineTo(x, bottom);
    ctx.strokeStyle = color;
    ctx.lineWidth = size.scale;
    ctx.setLineDash([4 * size.scale, 4 * size.scale]);
    ctx.stroke();
    ctx.setLineDash([]);
    var labelWidth = ctx.measureText(label).width;
    ctx.fillStyle = color;
    ctx.fillText(label, Math.max(0, Math.min(size.width - labelWidth, x - labelWidth / 2)), labelY);
  }

  function drawLandingChart() {
    if (!landingCanvas || !landingData || !landingData.available) return;
    var segment = landingWindow(landingData.segment);
    if (segment.length < 2) return;
    var size = fitCanvas(landingCanvas);
    var ctx = landingCanvas.getContext('2d');
    var scale = size.scale;
    var padding = {left: 54 * scale, right: 48 * scale, top: 44 * scale, bottom: 30 * scale};
    var width = size.width - padding.left - padding.right;
    var height = size.height - padding.top - padding.bottom;
    var minSeconds = number(segment[0].SecondsToTouchdown);
    var maxSeconds = number(segment[segment.length - 1].SecondsToTouchdown);
    var maximumHeight = Math.max(400, Math.ceil(Math.max.apply(null, segment.map(function (point) {
      return Math.max(0, number(point.HeightAboveRunwayFt));
    })) / 400) * 400);
    var maximumSpeed = Math.max(40, Math.ceil(Math.max.apply(null, segment.map(function (point) {
      return Math.max(0, number(point.GroundSpeedKt));
    })) / 20) * 20);
    var secondsRange = Math.max(1, maxSeconds - minSeconds);
    var xFor = function (seconds) { return padding.left + (seconds - minSeconds) / secondsRange * width; };
    var heightY = function (point) {
      return padding.top + height * (1 - Math.max(0, number(point.HeightAboveRunwayFt)) / maximumHeight);
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
      ctx.fillText(Math.round(maximumSpeed * (1 - line / 4)) + ' kt', size.width - padding.right + 6 * scale, y + 4 * scale);
    }

    var muted = cssColor('--muted', '#697386');
    var markerTop = padding.top - 4 * scale;
    var markerBottom = padding.top + height;
    var linedUp = landingData.approach && landingData.approach.lined_up_seconds;
    if (linedUp !== null && linedUp !== undefined && linedUp >= minSeconds) {
      drawLandingMarker(ctx, xFor(linedUp), markerTop, markerBottom, 'LINED UP', padding.top - 8 * scale, muted, size);
    }
    var taxi = landingData.rollout && landingData.rollout.seconds;
    if (taxi !== null && taxi !== undefined && taxi <= maxSeconds) {
      drawLandingMarker(ctx, xFor(taxi), markerTop, markerBottom, 'TAXI SPEED', padding.top - 8 * scale, muted, size);
    }
    drawLandingMarker(ctx, xFor(0), markerTop, markerBottom, 'TOUCHDOWN', size.height - 7 * scale, cssColor('--danger', '#b4234d'), size);

    ctx.globalAlpha = 0.7;
    drawLandingLine(ctx, segment, xFor, speedY, cssColor('--accent', '#00a4ef'), 1.5 * scale);
    ctx.globalAlpha = 1;
    drawLandingLine(ctx, segment, xFor, heightY, cssColor('--primary', '#635bff'), 3 * scale);

    ctx.fillStyle = cssColor('--primary', '#635bff');
    var legend = 'HEIGHT ABOVE RUNWAY';
    ctx.fillText(legend, padding.left, 14 * scale);
    ctx.fillStyle = cssColor('--accent', '#00a4ef');
    ctx.fillText('GROUNDSPEED', padding.left + ctx.measureText(legend).width + 18 * scale, 14 * scale);
    ctx.fillStyle = muted;
    ctx.fillText(Math.round(Math.abs(minSeconds) / 60) + ' min before', padding.left, size.height - 7 * scale);
  }

  function drawGhostChart() {
    if (!ghostCanvas || !landingData || !landingData.available || !ghostLandings.length) return;
    var current = landingWindow(landingData.segment).filter(function (point) {
      return number(point.SecondsToTouchdown) <= 15;
    });
    var histories = ghostLandings.map(function (ghost) {
      return landingWindow(ghost.segment).filter(function (point) {
        return number(point.SecondsToTouchdown) <= 15;
      });
    }).filter(function (segment) { return segment.length > 1; });
    if (current.length < 2 || !histories.length) return;

    var size = fitCanvas(ghostCanvas);
    var ctx = ghostCanvas.getContext('2d');
    var scale = size.scale;
    var padding = {left: 52 * scale, right: 16 * scale, top: 28 * scale, bottom: 28 * scale};
    var width = size.width - padding.left - padding.right;
    var height = size.height - padding.top - padding.bottom;
    var allPoints = current.concat.apply(current, histories);
    var maximumHeight = Math.max(600, Math.ceil(Math.max.apply(null, allPoints.map(function (point) {
      return Math.max(0, number(point.HeightAboveRunwayFt));
    })) / 300) * 300);
    var xFor = function (seconds) { return padding.left + (seconds + 180) / 195 * width; };
    var yFor = function (point) {
      return padding.top + height * (1 - Math.max(0, number(point.HeightAboveRunwayFt)) / maximumHeight);
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
    ctx.fillText('THIS LANDING', padding.left, 14 * scale);
    ctx.fillStyle = cssColor('--muted', '#697386');
    ctx.fillText('PREVIOUS LANDINGS', padding.left + 110 * scale, 14 * scale);
    ctx.fillText('3 min before', padding.left, size.height - 7 * scale);
    var touchdownLabel = 'TOUCHDOWN';
    var touchdownWidth = ctx.measureText(touchdownLabel).width;
    ctx.fillText(touchdownLabel, size.width - padding.right - touchdownWidth, size.height - 7 * scale);
  }

  function landingCheckRow(check) {
    var row = document.createElement('div');
    row.className = 'landing-check is-' + check.verdict;
    var icon = document.createElement('span');
    icon.className = 'landing-check-icon';
    icon.textContent = check.verdict === 'good' ? '✓' : (check.verdict === 'check' ? '!' : 'i');
    var text = document.createElement('div');
    var head = document.createElement('div');
    head.className = 'landing-check-head';
    var label = document.createElement('strong');
    label.textContent = check.label;
    var value = document.createElement('span');
    value.textContent = check.value;
    var detail = document.createElement('p');
    detail.textContent = check.detail;
    head.appendChild(label);
    head.appendChild(value);
    text.appendChild(head);
    text.appendChild(detail);
    row.appendChild(icon);
    row.appendChild(text);
    return row;
  }

  function ghostTableRow(title, landing, isCurrent) {
    var approach = landing.approach || {};
    var rollout = landing.rollout || {};
    var speed = approach.speed_min_kt === null || approach.speed_min_kt === undefined ? '—' :
      approach.speed_min_kt + '–' + approach.speed_max_kt + ' kt' + (approach.speed_gain_kt >= 8 ? ' (+' + approach.speed_gain_kt + ')' : '');
    var cells = [
      title,
      approach.lined_up_height_ft === null || approach.lined_up_height_ft === undefined ? '—' : approach.lined_up_height_ft.toLocaleString() + ' ft',
      approach.path_angle_deg === null || approach.path_angle_deg === undefined ? '—' : approach.path_angle_deg.toFixed(1) + '°',
      speed,
      Math.round(number(landing.touchdown && landing.touchdown.groundspeed_kt)) + ' kt',
      rollout.seconds === null || rollout.seconds === undefined ? '—' : rollout.seconds + ' s · ' + number(rollout.distance_ft).toLocaleString() + ' ft'
    ];
    var labels = ['Landing', 'Lined up', 'Slope', 'Short final', 'Touchdown', 'Rollout'];
    var row = document.createElement('tr');
    if (isCurrent) row.className = 'is-current';
    cells.forEach(function (value, index) {
      var cell = document.createElement('td');
      cell.setAttribute('data-label', labels[index]);
      if (index === 0) {
        var strong = document.createElement('strong');
        strong.textContent = value;
        cell.appendChild(strong);
      } else {
        cell.textContent = value;
      }
      row.appendChild(cell);
    });
    return row;
  }

  var landingConfidenceLabels = {high: 'Good GPS data', medium: 'Fair GPS data', low: 'Limited GPS data'};

  function renderLanding(analysis, ghosts) {
    landingData = analysis || {available: false};
    ghostLandings = Array.isArray(ghosts) ? ghosts : [];
    landingSection.hidden = false;
    var confidence = document.getElementById('landing-confidence');
    var title = document.getElementById('landing-summary-title');
    var story = document.getElementById('landing-summary-story');
    var next = document.getElementById('landing-summary-next');
    var tips = document.getElementById('landing-summary-tips');
    var note = document.getElementById('landing-summary-note');
    var checks = document.getElementById('landing-checks');

    if (!landingData.available) {
      confidence.textContent = 'Not enough data';
      confidence.className = 'landing-confidence is-low';
      title.textContent = 'This landing could not be reviewed';
      story.textContent = landingData.reason || 'The GPS track does not cover the landing.';
      next.hidden = true;
      note.textContent = '';
      checks.hidden = true;
      document.querySelector('.landing-chart-wrap').hidden = true;
      return;
    }

    confidence.textContent = landingConfidenceLabels[landingData.confidence] || 'GPS data';
    confidence.className = 'landing-confidence is-' + landingData.confidence;
    var summary = landingData.summary || {};
    title.textContent = summary.title;
    story.textContent = summary.story;
    tips.textContent = '';
    (summary.tips || []).forEach(function (text) {
      var item = document.createElement('li');
      item.textContent = text;
      tips.appendChild(item);
    });
    if (!tips.children.length) {
      var keep = document.createElement('li');
      keep.textContent = 'Nothing to change. Keep flying it this way.';
      tips.appendChild(keep);
    }
    next.hidden = false;
    note.textContent = summary.note || '';
    checks.textContent = '';
    (landingData.checks || []).forEach(function (check) {
      checks.appendChild(landingCheckRow(check));
    });
    drawLandingChart();

    ghostSection.hidden = false;
    var hasGhosts = ghostLandings.length > 0;
    document.getElementById('ghost-empty').hidden = hasGhosts;
    document.getElementById('ghost-content').hidden = !hasGhosts;
    if (hasGhosts) {
      var comparisons = document.getElementById('ghost-comparisons');
      comparisons.textContent = '';
      comparisons.appendChild(ghostTableRow('This landing', landingData, true));
      ghostLandings.forEach(function (ghost) {
        comparisons.appendChild(ghostTableRow(new Date(ghost.date_utc).toLocaleDateString(), ghost, false));
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
      trafficMarker.position = visualPosition(
        number(target.Longitude),
        number(target.Latitude),
        number(target.AltitudeFt)
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
      if (following) {
        viewer.camera.lookAt(
          routePositions[currentIndex],
          new Cesium.HeadingPitchRange(
            Cesium.Math.toRadians(point.courseDeg + 180),
            Cesium.Math.toRadians(-18),
            8500
          )
        );
      }
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

    routeMinimumAltitudeFt = Math.min.apply(null, points.map(function (point) {
      return point.altitudeFt;
    }));
    routePositions = points.map(function (point) {
      return visualPosition(point.longitude, point.latitude, point.altitudeFt);
    });
    var groundPositions = points.map(function (point) {
      return Cesium.Cartesian3.fromDegrees(point.longitude, point.latitude, 30);
    });

    viewer.entities.add({
      polyline: {
        positions: groundPositions,
        width: 2,
        material: new Cesium.PolylineDashMaterialProperty({
          color: Cesium.Color.fromCssColorString('#8b97a8').withAlpha(0.75),
          dashLength: 14
        }),
        arcType: Cesium.ArcType.NONE
      }
    });
    viewer.entities.add({
      wall: {
        positions: groundPositions,
        minimumHeights: points.map(function () { return 30; }),
        maximumHeights: points.map(function (point) { return visualAltitudeMeters(point.altitudeFt); }),
        material: Cesium.Color.fromCssColorString('#635bff').withAlpha(0.13)
      }
    });
    viewer.entities.add({
      polyline: {
        positions: routePositions,
        width: 4,
        material: Cesium.Color.fromCssColorString('#8d85ff').withAlpha(0.9),
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
    viewer.entities.add({
      position: groundPositions[0],
      point: {
        pixelSize: 10,
        color: Cesium.Color.fromCssColorString('#16a36a'),
        outlineColor: Cesium.Color.WHITE,
        outlineWidth: 2
      },
      label: {
        text: 'START',
        font: '600 12px -apple-system, BlinkMacSystemFont, sans-serif',
        fillColor: Cesium.Color.WHITE,
        showBackground: true,
        backgroundColor: Cesium.Color.fromCssColorString('#101828').withAlpha(0.84),
        pixelOffset: new Cesium.Cartesian2(0, -24)
      }
    });
    viewer.entities.add({
      position: groundPositions[groundPositions.length - 1],
      point: {
        pixelSize: 10,
        color: Cesium.Color.fromCssColorString('#cd3d64'),
        outlineColor: Cesium.Color.WHITE,
        outlineWidth: 2
      },
      label: {
        text: 'FINISH',
        font: '600 12px -apple-system, BlinkMacSystemFont, sans-serif',
        fillColor: Cesium.Color.WHITE,
        showBackground: true,
        backgroundColor: Cesium.Color.fromCssColorString('#101828').withAlpha(0.84),
        pixelOffset: new Cesium.Cartesian2(0, -24)
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

    routeSphere = Cesium.BoundingSphere.fromPoints(routePositions);
    resetOverviewCamera();
    viewer.scene.requestRender();
  }

  function resetOverviewCamera() {
    if (!viewer || !routeSphere) return;
    following = false;
    viewer.trackedEntity = undefined;
    followButton.textContent = 'Follow aircraft';
    followButton.classList.remove('is-active');
    viewer.camera.lookAtTransform(Cesium.Matrix4.IDENTITY);
    viewer.camera.viewBoundingSphere(
      routeSphere,
      new Cesium.HeadingPitchRange(
        Cesium.Math.toRadians(28),
        Cesium.Math.toRadians(-20),
        Math.max(routeSphere.radius * 1.8, 7000)
      )
    );
    viewer.camera.lookAtTransform(Cesium.Matrix4.IDENTITY);
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
      followButton.textContent = following ? 'Stop following' : 'Follow aircraft';
      followButton.classList.toggle('is-active', following);
      if (following) {
        updateReplay(flight);
      } else {
        resetOverviewCamera();
      }
    });
    resetViewButton.addEventListener('click', resetOverviewCamera);
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
