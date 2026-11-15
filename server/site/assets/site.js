(function () {
  var REPO = 'lemonpot/stratux-nx';

  var nav = document.getElementById('nav');
  var toggle = document.getElementById('nav-toggle');
  var menu = document.getElementById('mobile-menu');
  window.addEventListener('scroll', function () {
    nav.classList.toggle('scrolled', window.scrollY > 8);
  }, { passive: true });
  toggle.addEventListener('click', function () {
    var open = menu.classList.toggle('open');
    toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
  });
  menu.addEventListener('click', function (event) {
    if (event.target.closest('a')) menu.classList.remove('open');
  });

  document.getElementById('year').textContent = new Date().getFullYear();

  var revealed = document.querySelectorAll('.reveal');
  if ('IntersectionObserver' in window) {
    var observer = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (entry.isIntersecting) {
          entry.target.classList.add('visible');
          observer.unobserve(entry.target);
        }
      });
    }, { rootMargin: '0px 0px -8% 0px' });
    revealed.forEach(function (el) { observer.observe(el); });
  } else {
    revealed.forEach(function (el) { el.classList.add('visible'); });
  }

  document.querySelectorAll('.card').forEach(function (card) {
    card.addEventListener('pointermove', function (event) {
      var box = card.getBoundingClientRect();
      card.style.setProperty('--mx', (event.clientX - box.left) + 'px');
      card.style.setProperty('--my', (event.clientY - box.top) + 'px');
    });
  });

  function setAll(name, apply) {
    document.querySelectorAll('[data-rel="' + name + '"]').forEach(apply);
  }

  function formatSize(bytes) {
    return bytes >= 1e9 ? (bytes / 1e9).toFixed(1) + ' GB' : Math.round(bytes / 1e6) + ' MB';
  }

  function imageAsset(release) {
    return (release.assets || []).filter(function (a) { return /\.(zip|img\.xz|img\.gz|img)$/i.test(a.name); })[0];
  }

  fetch('https://api.github.com/repos/' + REPO + '/releases?per_page=30', { headers: { Accept: 'application/vnd.github+json' } })
    .then(function (response) { return response.ok ? response.json() : []; })
    .then(function (releases) {
      var release = (releases || [])
        .filter(function (r) { return !r.draft && imageAsset(r); })
        .sort(function (a, b) { return new Date(b.published_at) - new Date(a.published_at); })[0];
      if (!release) return;
      var image = imageAsset(release);
      var version = release.tag_name.replace(/^nx-/, '');
      var date = new Date(release.published_at).toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' });

      setAll('page', function (el) { el.href = release.html_url; });
      setAll('pill', function (el) { el.textContent = 'Latest release: ' + version.split('.').slice(0, 2).join('.') + ' · ' + date; });
      setAll('tag', function (el) { el.textContent = release.name || ('Stratux NX ' + version); });
      setAll('tagshort', function (el) { el.textContent = 'Release ' + version; });
      setAll('meta', function (el) {
        el.textContent = 'Published ' + date + (release.prerelease ? ' · beta' : '') + '. Full SD card image, online update package and complete source code.';
      });
      if (image) {
        setAll('image', function (el) { el.href = image.browser_download_url; el.removeAttribute('target'); });
        setAll('imagelabel', function (el) { el.textContent = 'Download SD card image · ' + formatSize(image.size); });
      }
    })
    .catch(function () {});
})();
