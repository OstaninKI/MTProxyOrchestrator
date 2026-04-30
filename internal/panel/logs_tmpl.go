package panel

import (
	"html/template"
	"io"
)

var logsTmpl = template.Must(template.New("logs").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Logs</title>
<style>
*{box-sizing:border-box}
body{font-family:sans-serif;margin:0;background:#1a1a1a;color:#ccc;display:flex;flex-direction:column;height:100vh}
.toolbar{background:#222;padding:.6rem 1rem;display:flex;gap:.5rem;align-items:center;flex-wrap:wrap;border-bottom:1px solid #333}
.toolbar label{font-size:.8rem;color:#aaa;margin-right:.25rem}
select,input[type=text]{background:#333;color:#ccc;border:1px solid #444;border-radius:3px;padding:.3rem .5rem;font-size:.85rem}
button{padding:.3rem .7rem;border:none;border-radius:3px;cursor:pointer;font-size:.85rem}
.btn-pause{background:#555;color:#fff}
.btn-pause.paused{background:#b45309;color:#fff}
.btn-clear{background:#374151;color:#fff}
.btn-download{background:#1d4ed8;color:#fff;text-decoration:none;display:inline-block}
a.btn-download{line-height:1.6}
.spacer{flex:1}
#log-container{flex:1;overflow-y:auto;padding:.5rem 1rem;font-family:'Courier New',monospace;font-size:.8rem;line-height:1.5}
.log-line{white-space:pre-wrap;word-break:break-all;margin:.1rem 0}
.log-line.error{color:#f87171}
.log-line.warn{color:#fbbf24}
.log-line.info{color:#f5f5f5}
.log-line.debug{color:#6b7280}
.status{font-size:.75rem;color:#6b7280;padding:.3rem 1rem;background:#111;border-top:1px solid #333}
</style>
</head>
<body>
<div class="toolbar">
  <label>Component</label>
  <select id="component">
    <option value="panel">panel</option>
    <option value="teleproxy">teleproxy</option>
    <option value="sing-box">sing-box</option>
    <option value="nginx">nginx</option>
  </select>
  <label>Level</label>
  <select id="level">
    <option value="debug">debug</option>
    <option value="info" selected>info</option>
    <option value="warn">warn</option>
    <option value="error">error</option>
  </select>
  <label>Search</label>
  <input type="text" id="search" placeholder="filter text..." style="width:180px">
  <button class="btn-pause" id="btnPause">Pause</button>
  <button class="btn-clear" id="btnClear">Clear</button>
  <span class="spacer"></span>
  <a class="btn-download" id="btnDownload" href="#">Download (500 lines)</a>
  <a href="{{.PanelPath}}/" style="color:#6b7280;font-size:.8rem;margin-left:.5rem">&#8592; Back</a>
</div>
<div id="log-container"></div>
<div class="status" id="status">Connecting&#8230;</div>

<script>
(function(){
  var BASE = {{.PanelPath}};
  var container = document.getElementById('log-container');
  var statusEl  = document.getElementById('status');
  var btnPause  = document.getElementById('btnPause');
  var btnClear  = document.getElementById('btnClear');
  var btnDl     = document.getElementById('btnDownload');
  var selComp   = document.getElementById('component');
  var selLevel  = document.getElementById('level');
  var inpSearch = document.getElementById('search');

  var paused    = false;
  var autoScroll = true;
  var ws        = null;

  function buildWsURL() {
    var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    var q = '?component=' + encodeURIComponent(selComp.value)
          + '&level='     + encodeURIComponent(selLevel.value)
          + '&q='         + encodeURIComponent(inpSearch.value);
    return proto + '//' + location.host + BASE + '/logs/stream' + q;
  }

  function buildDownloadURL() {
    return BASE + '/logs/download?component=' + encodeURIComponent(selComp.value);
  }

  function levelClass(lvl) {
    switch(lvl) {
      case 'error': return 'error';
      case 'warn':  return 'warn';
      case 'debug': return 'debug';
      default:      return 'info';
    }
  }

  function appendEntry(entry) {
    if (paused) return;
    var d = document.createElement('div');
    d.className = 'log-line ' + levelClass(entry.level);
    var ts = entry.time ? new Date(entry.time).toISOString().replace('T',' ').replace('Z','') : '';
    d.textContent = ts + ' [' + (entry.level||'info').toUpperCase() + '] ' + (entry.message||'');
    container.appendChild(d);
    // Trim to last 2000 lines to avoid memory growth.
    while (container.children.length > 2000) {
      container.removeChild(container.firstChild);
    }
    if (autoScroll) {
      container.scrollTop = container.scrollHeight;
    }
  }

  function connect() {
    if (ws) { ws.close(); ws = null; }
    statusEl.textContent = 'Connecting…';
    ws = new WebSocket(buildWsURL());

    ws.onopen = function() {
      statusEl.textContent = 'Connected — ' + selComp.value + ' / ' + selLevel.value;
    };
    ws.onmessage = function(evt) {
      try {
        appendEntry(JSON.parse(evt.data));
      } catch(e) {}
    };
    ws.onerror = function() {
      statusEl.textContent = 'Connection error';
    };
    ws.onclose = function(evt) {
      statusEl.textContent = 'Disconnected (code ' + evt.code + '). Reconnecting in 5s…';
      setTimeout(connect, 5000);
    };
  }

  // Detect manual scroll to disable auto-scroll.
  container.addEventListener('scroll', function() {
    var threshold = 40;
    autoScroll = container.scrollTop + container.clientHeight >= container.scrollHeight - threshold;
  });

  btnPause.addEventListener('click', function() {
    paused = !paused;
    btnPause.textContent = paused ? 'Resume' : 'Pause';
    btnPause.classList.toggle('paused', paused);
  });

  btnClear.addEventListener('click', function() {
    while (container.firstChild) {
      container.removeChild(container.firstChild);
    }
  });

  function reapply() {
    btnDl.href = buildDownloadURL();
    connect();
  }

  selComp.addEventListener('change', reapply);
  selLevel.addEventListener('change', reapply);

  var searchTimer;
  inpSearch.addEventListener('input', function() {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(reapply, 400);
  });

  btnDl.href = buildDownloadURL();
  connect();
})();
</script>
</body>
</html>
`))

func logsPage(w io.Writer, panelPath string) {
	logsTmpl.Execute(w, map[string]string{ //nolint:errcheck
		"PanelPath": panelPath,
	})
}
