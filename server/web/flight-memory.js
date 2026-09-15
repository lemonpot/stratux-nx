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

    window.addEventListener('resize', function () {
      drawProfile();
      drawFallback();
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
