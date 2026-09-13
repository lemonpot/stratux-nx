/* ============================================================
   STRATUX NX -- Map Enhancements
   Better tile sources, smoother animations, custom controls.
   Layered on top of upstream map.js without replacing it.
   ============================================================ */
(function(window) {
  'use strict';

  var NX_TILES = {
    cartoDark: {
      label: 'Dark (Night VFR)',
      url: 'https://{a-d}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}@2x.png',
      attr: '&copy; <a href="https://www.openstreetmap.org/">OSM</a> &copy; <a href="https://carto.com/">CARTO</a>'
    },
    cartoLight: {
      label: 'Light (Day VFR)',
      url: 'https://{a-d}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}@2x.png',
      attr: '&copy; <a href="https://www.openstreetmap.org/">OSM</a> &copy; <a href="https://carto.com/">CARTO</a>'
    },
    cartoVoyager: {
      label: 'Voyager',
      url: 'https://{a-d}.basemaps.cartocdn.com/rastertiles/voyager/{z}/{x}/{y}@2x.png',
      attr: '&copy; <a href="https://www.openstreetmap.org/">OSM</a> &copy; <a href="https://carto.com/">CARTO</a>'
    }
  };

  function isDarkMode() {
    return document.body.classList.contains('stratux-dark');
  }

  function patchMapIfReady() {
    if (typeof ol === 'undefined') return;

    var mapEl = document.getElementById('map_display');
    if (!mapEl) return;

    var maps = mapEl.querySelectorAll('.ol-viewport');
    if (!maps.length) return;

    var olMap = null;
    if (window._stratuxMap) {
      olMap = window._stratuxMap;
    }

    if (!olMap) return;

    var existingLayers = olMap.getLayers().getArray();
    var hasNXBase = false;
    for (var i = 0; i < existingLayers.length; i++) {
      if (existingLayers[i].get('nx-base')) {
        hasNXBase = true;
        break;
      }
    }

    if (hasNXBase) return;

    var tileKey = isDarkMode() ? 'cartoDark' : 'cartoLight';
    var tileConf = NX_TILES[tileKey];

    try {
      var nxBaseLayer = new ol.layer.Tile({
        source: new ol.source.XYZ({
          url: tileConf.url,
          attributions: tileConf.attr,
          crossOrigin: 'anonymous',
          tilePixelRatio: 2,
          maxZoom: 19
        }),
        zIndex: -1
      });
      nxBaseLayer.set('nx-base', true);
      nxBaseLayer.set('title', tileConf.label);

      olMap.getLayers().insertAt(0, nxBaseLayer);
    } catch (e) {
      /* Fail silently - upstream OSM layer remains */
    }
  }

  /* Expose for manual triggering */
  window.StratuxMapNX = {
    tiles: NX_TILES,
    patchMap: patchMapIfReady
  };

  /* Attempt to patch once the map page loads */
  window.addEventListener('hashchange', function() {
    if (window.location.hash === '#/map') {
      window.setTimeout(patchMapIfReady, 500);
      window.setTimeout(patchMapIfReady, 1500);
    }
  });

  /* Initial check */
  if (window.location.hash === '#/map') {
    window.setTimeout(patchMapIfReady, 500);
  }

})(window);
