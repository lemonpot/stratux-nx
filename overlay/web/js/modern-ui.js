(function(window, document) {
  'use strict';

  /* ================================================================
     STRATUX NX -- Global UX Layer
     Toast notifications, busy overlay, navigation chrome, page heroes,
     dark mode detection, sidebar grouping.
     ================================================================ */

  var pageMeta = {
    '#/':          { title: 'Status',        subtitle: 'Receiver health, radio activity, GPS and system overview.', eyebrow: 'DASHBOARD', group: 'main', icon: 'fa-tachometer' },
    '#/weather':   { title: 'Weather',       subtitle: 'Live weather products received via ADS-B.',                 eyebrow: 'FLIGHT DATA', group: 'flight', icon: 'fa-cloud' },
    '#/traffic':   { title: 'Traffic',       subtitle: 'Nearby aircraft and vessel traffic.',                       eyebrow: 'TRAFFIC', group: 'flight', icon: 'fa-plane' },
    '#/gps':       { title: 'GPS / AHRS',    subtitle: 'Position, satellites, attitude and sensor data.',           eyebrow: 'SENSORS', group: 'flight', icon: 'fa-location-arrow' },
    '#/towers':    { title: 'Towers',        subtitle: 'Ground stations and ADS-B reception quality.',              eyebrow: 'RECEPTION', group: 'flight', icon: 'fa-signal' },
    '#/radar':     { title: 'Radar',         subtitle: 'Traffic in a cockpit-style radar view.',                    eyebrow: 'TRAFFIC', group: 'flight', icon: 'fa-bullseye' },
    '#/map':       { title: 'Map',           subtitle: 'Traffic and position on the moving map.',                   eyebrow: 'NAVIGATION', group: 'flight', icon: 'fa-map' },
    '#/flightlog': { title: 'Flight Log',    subtitle: 'Automatic GPS flight history and route recording.',         eyebrow: 'HISTORY', group: 'tools', icon: 'fa-book' },
    '#/datausage': { title: 'Internet Data', subtitle: 'Monitor and control internet data usage.',                  eyebrow: 'CONNECTIVITY', group: 'tools', icon: 'fa-bar-chart' },
    '#/logs':      { title: 'Logs',          subtitle: 'System log files and diagnostic downloads.',                eyebrow: 'DIAGNOSTICS', group: 'system', icon: 'fa-file-text-o' },
    '#/settings':  { title: 'Settings',      subtitle: 'Configure radios, WiFi, ownship, sensors and behavior.',   eyebrow: 'CONFIGURATION', group: 'system', icon: 'fa-cog' },
    '#/about':     { title: 'About',         subtitle: 'Project history, attribution and Stratux NX information.', eyebrow: 'STRATUX NX', group: 'system', icon: 'fa-info-circle' },
    // Update is embedded in Settings -- no hero needed
    '#/developer': { title: 'Developer',     subtitle: 'Advanced diagnostics and developer controls.',              eyebrow: 'ADVANCED', group: 'system', icon: 'fa-code' }
  };


  /* --- Toast Notification System ---------------------------------- */

  function ensureToastStack() {
    var el = document.getElementById('sx-toast-stack');
    if (!el) {
      el = document.createElement('div');
      el.id = 'sx-toast-stack';
      el.className = 'sx-toast-stack';
      document.body.appendChild(el);
    }
    return el;
  }

  function iconFor(type) {
    switch (type) {
      case 'success': return 'fa-check-circle';
      case 'error':   return 'fa-exclamation-circle';
      case 'warning': return 'fa-exclamation-triangle';
      default:        return 'fa-info-circle';
    }
  }

  function titleFor(type) {
    switch (type) {
      case 'success': return 'Done';
      case 'error':   return 'Error';
      case 'warning': return 'Warning';
      default:        return 'Stratux NX';
    }
  }

  function toast(message, type, duration, title) {
    type = type || 'info';
    duration = duration || 3200;
    var stack = ensureToastStack();
    var item = document.createElement('div');
    item.className = 'sx-toast ' + type;
    item.innerHTML = '<i class="fa ' + iconFor(type) + '"></i><div><strong></strong><span></span></div>';
    item.querySelector('strong').textContent = title || titleFor(type);
    item.querySelector('span').textContent = String(message || '');
    stack.appendChild(item);
    window.setTimeout(function() {
      item.style.opacity = '0';
      item.style.transform = 'translateY(-6px) scale(.96)';
      item.style.transition = 'opacity .2s, transform .2s';
      window.setTimeout(function() {
        if (item.parentNode) item.parentNode.removeChild(item);
      }, 220);
    }, duration);
    return item;
  }


  /* --- Global Busy Overlay ---------------------------------------- */

  function ensureBusy() {
    var el = document.getElementById('sx-global-busy');
    if (!el) {
      el = document.createElement('div');
      el.id = 'sx-global-busy';
      el.className = 'sx-global-busy';
      el.innerHTML =
        '<div class="sx-global-busy-card">' +
          '<div class="sx-spinner"></div>' +
          '<strong>Working...</strong>' +
          '<span>Please wait.</span>' +
        '</div>';
      document.body.appendChild(el);
    }
    return el;
  }

  function showBusy(title, message) {
    var el = ensureBusy();
    el.querySelector('strong').textContent = title || 'Working...';
    el.querySelector('span').textContent = message || 'Please wait.';
    el.classList.add('is-visible');
  }

  function hideBusy() {
    var el = document.getElementById('sx-global-busy');
    if (el) el.classList.remove('is-visible');
  }


  /* --- Navigation Chrome ------------------------------------------ */

  function currentRoute() {
    var hash = window.location.hash || '#/';
    return hash.split('?')[0] || '#/';
  }

  function setActiveNav() {
    var hash = currentRoute();
    var links = document.querySelectorAll('.sidebar-left .list-group-item[href^="#/"]');
    for (var i = 0; i < links.length; i++) {
      var href = links[i].getAttribute('href') || '';
      var active = (href === '#/' ? hash === '#/' : hash.indexOf(href) === 0);
      if (active) links[i].classList.add('sx-active');
      else links[i].classList.remove('sx-active');
    }
  }

  function setBrandNX() {
    var brand = document.querySelector('.navbar-brand.navbar-brand-center');
    if (!brand) return;
    var link = brand.querySelector('a[href="#/"]');
    if (link && !link.querySelector('.sx-navbar-name')) {
      link.textContent = '';
      var name = document.createElement('span');
      name.className = 'sx-navbar-name';
      name.textContent = 'Stratux NX';
      link.appendChild(name);
    }
    if (!brand.querySelector('.sx-version-badge')) {
      initVersionBadge(brand);
    }
  }


  /* --- Version Badge & Update Dot --------------------------------- */

  function initVersionBadge(brandEl) {
    var xhr = new XMLHttpRequest();
    xhr.open('GET', '/getStatus', true);
    xhr.timeout = 4000;
    xhr.onload = function() {
      if (xhr.status !== 200) return;
      try {
        var data = JSON.parse(xhr.responseText);
        var ver = data.Version || '';
        if (!ver) return;
        var badge = document.createElement('span');
        badge.className = 'sx-version-badge';
        badge.textContent = ver;
        brandEl.appendChild(badge);
      } catch (e) { /* ignore parse errors */ }
    };
    xhr.send();

    checkForUpdate();
  }

  function checkForUpdate() {
    var xhr = new XMLHttpRequest();
    xhr.open('GET', '/update/status', true);
    xhr.timeout = 4000;
    xhr.onload = function() {
      if (xhr.status !== 200) return;
      try {
        var data = JSON.parse(xhr.responseText);
        if (data && data.available && data.available.update_available) {
          showUpdateDot();
        }
      } catch (e) { /* ignore */ }
    };
    xhr.send();
  }

  function showUpdateDot() {
    var brand = document.querySelector('.navbar-brand.navbar-brand-center');
    if (!brand || brand.querySelector('.sx-update-dot')) return;
    var dot = document.createElement('span');
    dot.className = 'sx-update-dot';
    dot.title = 'Update available';
    brand.appendChild(dot);
  }


  /* --- Page Hero Headers ------------------------------------------ */

  function directPageHero(view) {
    if (!view) return null;
    for (var i = 0; i < view.children.length; i++) {
      var child = view.children[i];
      if ((' ' + child.className + ' ').indexOf(' sx-page-hero ') >= 0) return child;
    }
    return null;
  }

  function renderPageHeader() {
    var route = currentRoute();
    var view = document.querySelector('div[ui-view]');
    if (view) {
      view.classList.toggle('sx-page-full', route === '#/map');
      view.setAttribute('data-sx-route', route === '#/' ? 'status' : route.replace('#/', ''));
    }
    if (route === '#/map') return;

    var meta = pageMeta[route];
    if (!view || !meta) return;

    var existing = view.querySelector('.sx-page-header');
    if (existing && existing.getAttribute('data-route') === route) return;
    if (existing && existing.parentNode) existing.parentNode.removeChild(existing);

    var header = document.createElement('header');
    header.className = 'sx-page-header';
    header.setAttribute('data-route', route);
    header.innerHTML = '<div><h1></h1><p></p></div>';
    header.querySelector('h1').textContent = meta.title;
    header.querySelector('p').textContent = meta.subtitle;
    view.insertBefore(header, view.firstChild);
  }


  /* --- Dark Mode Detection ---------------------------------------- */

  function setDarkClass() {
    var theme = document.getElementById('themeStylesheet');
    var dark = theme && /dark-mode\.css/.test(theme.getAttribute('href') || '');
    document.body.classList.toggle('stratux-dark', !!dark);
  }

  function watchTheme() {
    var theme = document.getElementById('themeStylesheet');
    if (!theme || !window.MutationObserver) { setDarkClass(); return; }
    setDarkClass();
    var observer = new MutationObserver(setDarkClass);
    observer.observe(theme, { attributes: true, attributeFilter: ['href'] });
  }


  /* --- Route Content Watcher -------------------------------------- */

  function watchRouteContent() {
    var root = document.querySelector('.app-content') || document.body;
    if (!root || !window.MutationObserver) return;
    var observer = new MutationObserver(function() {
      window.setTimeout(renderPageHeader, 0);
    });
    observer.observe(root, { childList: true, subtree: true });
  }


  /* --- Chrome Refresh --------------------------------------------- */

  function refreshChrome() {
    setActiveNav();
    setBrandNX();
    window.setTimeout(renderPageHeader, 0);
  }


  /* --- Public API ------------------------------------------------- */

  window.StratuxUI = {
    toast: toast,
    showBusy: showBusy,
    hideBusy: hideBusy,
    setActiveNav: setActiveNav,
    setDarkClass: setDarkClass,
    renderPageHeader: renderPageHeader,
    pageMeta: pageMeta
  };

  /* Override native alert with toast */
  var nativeAlert = window.alert;
  window.alert = function(message) {
    var text = String(message || '');
    var type = /error|failed|invalid/i.test(text) ? 'error'
             : /success|complete|installed/i.test(text) ? 'success'
             : 'info';
    try { toast(text, type, 4500); }
    catch (e) { nativeAlert(message); }
  };


  /* --- Init ------------------------------------------------------- */

  document.addEventListener('DOMContentLoaded', function() {
    ensureToastStack();
    ensureBusy();
    watchTheme();
    watchRouteContent();
    refreshChrome();
    // Angular may attach ui-view after DOMContentLoaded on slower receivers.
    window.setTimeout(renderPageHeader, 250);
    window.setTimeout(renderPageHeader, 1000);
  });

  window.addEventListener('hashchange', function() {
    window.setTimeout(refreshChrome, 10);
  });

})(window, document);
