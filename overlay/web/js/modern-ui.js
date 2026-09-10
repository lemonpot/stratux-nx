(function(window, document){
  'use strict';

  var pageMeta={
    '#/':{title:'System status',subtitle:'Receiver health, radio activity, GPS, storage and runtime at a glance.',eyebrow:'STRATUX'},
    '#/weather':{title:'Weather',subtitle:'Live weather products received by Stratux.',eyebrow:'FLIGHT DATA'},
    '#/traffic':{title:'Traffic',subtitle:'Nearby aircraft and vessel traffic currently seen by Stratux.',eyebrow:'TRAFFIC'},
    '#/gps':{title:'GPS / AHRS',subtitle:'Position, satellites, attitude and sensor status.',eyebrow:'NAVIGATION'},
    '#/towers':{title:'Towers',subtitle:'Ground stations and reception details.',eyebrow:'RECEPTION'},
    '#/logs':{title:'Logs',subtitle:'System log files and diagnostic downloads.',eyebrow:'DIAGNOSTICS'},
    '#/settings':{title:'Settings',subtitle:'Configure radios, WiFi, ownship, sensors and system behavior.',eyebrow:'CONFIGURATION'},
    '#/radar':{title:'Radar',subtitle:'Traffic displayed in a cockpit-style radar view.',eyebrow:'TRAFFIC'},
    '#/map':{title:'Map',subtitle:'Traffic and position on the moving map.',eyebrow:'NAVIGATION'},
    '#/flightlog':{title:'Flight Log',subtitle:'Automatic GPS flight history, airport detection, route, Zulu time and local time.',eyebrow:'FLIGHT HISTORY'},
    '#/developer':{title:'Developer',subtitle:'Advanced diagnostics and developer controls.',eyebrow:'ADVANCED'}
  };

  function ensureToastStack(){
    var el=document.getElementById('sx-toast-stack');
    if(!el){
      el=document.createElement('div');
      el.id='sx-toast-stack';
      el.className='sx-toast-stack';
      document.body.appendChild(el);
    }
    return el;
  }

  function iconFor(type){
    switch(type){
      case 'success': return 'fa-check-circle';
      case 'error': return 'fa-exclamation-circle';
      case 'warning': return 'fa-exclamation-triangle';
      default: return 'fa-info-circle';
    }
  }

  function titleFor(type){
    switch(type){
      case 'success': return 'Saved';
      case 'error': return 'Something went wrong';
      case 'warning': return 'Attention';
      default: return 'Stratux';
    }
  }

  function toast(message,type,duration,title){
    type=type||'info';
    duration=duration||3200;
    var stack=ensureToastStack();
    var item=document.createElement('div');
    item.className='sx-toast '+type;
    item.innerHTML='<i class="fa '+iconFor(type)+'"></i><div><strong></strong><span></span></div>';
    item.querySelector('strong').textContent=title||titleFor(type);
    item.querySelector('span').textContent=String(message||'');
    stack.appendChild(item);
    window.setTimeout(function(){
      item.style.opacity='0';
      item.style.transform='translateY(-5px)';
      item.style.transition='opacity .18s, transform .18s';
      window.setTimeout(function(){ if(item.parentNode){ item.parentNode.removeChild(item); } },190);
    },duration);
    return item;
  }

  function ensureBusy(){
    var el=document.getElementById('sx-global-busy');
    if(!el){
      el=document.createElement('div');
      el.id='sx-global-busy';
      el.className='sx-global-busy';
      el.innerHTML='<div class="sx-global-busy-card"><div class="sx-spinner"></div><strong>Working...</strong><span>Please wait.</span></div>';
      document.body.appendChild(el);
    }
    return el;
  }

  function showBusy(title,message){
    var el=ensureBusy();
    el.querySelector('strong').textContent=title||'Working...';
    el.querySelector('span').textContent=message||'Please wait.';
    el.classList.add('is-visible');
  }

  function hideBusy(){
    var el=document.getElementById('sx-global-busy');
    if(el){ el.classList.remove('is-visible'); }
  }

  function currentRoute(){
    var hash=window.location.hash||'#/';
    var clean=hash.split('?')[0];
    return clean||'#/';
  }

  function setActiveNav(){
    var hash=currentRoute();
    var links=document.querySelectorAll('.sidebar-left .list-group-item[href^="#/"]');
    for(var i=0;i<links.length;i++){
      var href=links[i].getAttribute('href')||'';
      var active=(href==='#/' ? hash==='#/' : hash.indexOf(href)===0);
      if(active){ links[i].classList.add('sx-active'); }
      else{ links[i].classList.remove('sx-active'); }
    }
  }

  function directPageHero(view){
    if(!view) return null;
    for(var i=0;i<view.children.length;i++){
      var child=view.children[i];
      if((' '+child.className+' ').indexOf(' sx-page-hero ')>=0) return child;
    }
    return null;
  }

  function renderPageHeader(){
    var route=currentRoute();
    if(route==='#/datausage' || route==='#/flightlog') return;
    var view=document.querySelector('div[ui-view]');
    if(!view) return;
    var meta=pageMeta[route];
    if(!meta) return;
    var old=directPageHero(view);
    if(old && old.getAttribute('data-route')===route) return;
    if(old && old.parentNode) old.parentNode.removeChild(old);
    var hero=document.createElement('div');
    hero.className='sx-page-hero';
    hero.setAttribute('data-route',route);
    hero.innerHTML='<div><div class="sx-page-eyebrow"></div><h2></h2><p></p></div>';
    hero.querySelector('.sx-page-eyebrow').textContent=meta.eyebrow;
    hero.querySelector('h2').textContent=meta.title;
    hero.querySelector('p').textContent=meta.subtitle;
    view.insertBefore(hero,view.firstChild);
  }

  function setDarkClass(){
    var theme=document.getElementById('themeStylesheet');
    var dark=theme && /dark-mode\.css/.test(theme.getAttribute('href')||'');
    document.body.classList.toggle('stratux-dark',!!dark);
  }

  function watchTheme(){
    var theme=document.getElementById('themeStylesheet');
    if(!theme || !window.MutationObserver){ setDarkClass(); return; }
    setDarkClass();
    var observer=new MutationObserver(setDarkClass);
    observer.observe(theme,{attributes:true,attributeFilter:['href']});
  }

  function watchRouteContent(){
    var view=document.querySelector('div[ui-view]');
    if(!view || !window.MutationObserver) return;
    var observer=new MutationObserver(function(){
      window.setTimeout(renderPageHeader,0);
    });
    observer.observe(view,{childList:true});
  }

  function refreshChrome(){
    setActiveNav();
    window.setTimeout(renderPageHeader,0);
  }

  window.StratuxUI={toast:toast,showBusy:showBusy,hideBusy:hideBusy,setActiveNav:setActiveNav,setDarkClass:setDarkClass,renderPageHeader:renderPageHeader};

  var nativeAlert=window.alert;
  window.alert=function(message){
    var text=String(message||'');
    var type=/error|failed|invalid/i.test(text)?'error':(/success|complete|installed/i.test(text)?'success':'info');
    try{ toast(text,type,4500); }
    catch(e){ nativeAlert(message); }
  };

  document.addEventListener('DOMContentLoaded',function(){
    ensureToastStack();
    ensureBusy();
    watchTheme();
    watchRouteContent();
    refreshChrome();
  });
  window.addEventListener('hashchange',function(){ window.setTimeout(refreshChrome,10); });
})(window,document);
