package main

import (
	"html/template"
	"runtime"
)

var runtimeGOOS = runtime.GOOS

const manifestJSON = `{
  "name": "face_auth agent",
  "short_name": "face_auth",
  "description": "Local-network bridge for face_auth cloud",
  "start_url": "/",
  "display": "standalone",
  "background_color": "#0a0c10",
  "theme_color": "#4f8cff",
  "icons": [
    {
      "src": "data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 192 192'><rect width='192' height='192' rx='32' fill='%234f8cff'/><circle cx='96' cy='96' r='40' fill='white'/></svg>",
      "sizes": "192x192",
      "type": "image/svg+xml",
      "purpose": "any maskable"
    }
  ]
}`

const serviceWorkerJS = `
// Minimal SW for installability. We don't cache anything since the dashboard
// must always reflect live state.
self.addEventListener('install', e => self.skipWaiting());
self.addEventListener('activate', e => self.clients.claim());
self.addEventListener('fetch', e => e.respondWith(fetch(e.request)));
`

var dashboardTpl = template.Must(template.New("dash").Parse(`<!doctype html>
<html lang="en" data-theme="dark">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<title>face_auth agent</title>
<link rel="manifest" href="/manifest.json">
<meta name="theme-color" content="#4f8cff">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
<meta name="apple-mobile-web-app-title" content="face_auth agent">
<style>
  :root[data-theme="dark"] {
    --bg:#0a0c10; --card:#11151b; --soft:#161b22; --hover:#1c222a;
    --text:#e6edf3; --muted:#7d8590; --border:#23292f; --border-strong:#30363d;
    --accent:#4f8cff; --accent-soft:rgba(79,140,255,0.14); --accent-hover:#6ea2ff;
    --ok:#3fb950; --ok-soft:rgba(63,185,80,0.14);
    --warn:#d29922; --warn-soft:rgba(210,153,34,0.14);
    --err:#f85149; --err-soft:rgba(248,81,73,0.12);
    --code-bg:#0d1117;
    --shadow:0 10px 30px rgba(0,0,0,0.4);
    --backdrop:rgba(0,0,0,0.6);
  }
  :root[data-theme="light"] {
    --bg:#f6f8fa; --card:#ffffff; --soft:#f0f3f6; --hover:#e7ebef;
    --text:#1f2328; --muted:#59636e; --border:#d1d9e0; --border-strong:#b7bec6;
    --accent:#0969da; --accent-soft:rgba(9,105,218,0.12); --accent-hover:#0860c6;
    --ok:#1a7f37; --ok-soft:rgba(26,127,55,0.12);
    --warn:#9a6700; --warn-soft:rgba(154,103,0,0.12);
    --err:#cf222e; --err-soft:rgba(207,34,46,0.10);
    --code-bg:#f6f8fa;
    --shadow:0 8px 24px rgba(31,35,40,0.14);
    --backdrop:rgba(31,35,40,0.45);
  }
  *{box-sizing:border-box}
  html,body{margin:0;background:var(--bg);color:var(--text);font:14px/1.45 -apple-system,BlinkMacSystemFont,"Segoe UI",Inter,sans-serif;transition:background-color .15s, color .15s}
  body{min-height:100vh;padding-bottom:env(safe-area-inset-bottom)}
  .wrap{max-width:920px;margin:0 auto;padding:18px 16px 32px}

  /* Header */
  header{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-bottom:14px}
  .brand{display:flex;align-items:center;gap:10px;min-width:0}
  .brand-dot{width:10px;height:10px;border-radius:50%;background:var(--accent);box-shadow:0 0 0 4px var(--accent-soft);flex-shrink:0}
  h1{margin:0;font-size:17px;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .platform{font-size:10.5px;color:var(--muted);text-transform:uppercase;letter-spacing:0.6px;padding:2px 8px;background:var(--soft);border:1px solid var(--border);border-radius:999px;flex-shrink:0}
  .status-pill{display:flex;align-items:center;gap:7px;padding:5px 12px;border-radius:999px;font-size:12px;font-weight:500;border:1px solid var(--border);background:var(--soft);color:var(--muted);flex-shrink:0}
  .status-pill .dot{width:7px;height:7px;border-radius:50%;background:currentColor}
  .status-pill.ok{color:var(--ok);border-color:rgba(63,185,80,0.35);background:var(--ok-soft)}
  .status-pill.ok .dot{box-shadow:0 0 0 3px rgba(63,185,80,0.2);animation:pulse 2s infinite}
  .status-pill.err{color:var(--err);border-color:rgba(248,81,73,0.35);background:var(--err-soft)}
  @keyframes pulse{0%{box-shadow:0 0 0 0 rgba(63,185,80,0.4)}70%{box-shadow:0 0 0 8px rgba(63,185,80,0)}100%{box-shadow:0 0 0 0 rgba(63,185,80,0)}}
  .header-actions{margin-left:auto;display:flex;align-items:center;gap:8px;flex-shrink:0}
  .theme-toggle{width:34px;height:34px;border-radius:50%;border:1px solid var(--border);background:var(--soft);color:var(--text);cursor:pointer;display:flex;align-items:center;justify-content:center;padding:0}
  .theme-toggle:hover{background:var(--hover)}
  .theme-toggle svg{width:16px;height:16px}
  :root[data-theme="dark"] .theme-toggle .sun{display:block}
  :root[data-theme="dark"] .theme-toggle .moon{display:none}
  :root[data-theme="light"] .theme-toggle .sun{display:none}
  :root[data-theme="light"] .theme-toggle .moon{display:block}

  /* Tabs */
  nav.tabs{display:flex;gap:2px;margin-bottom:14px;background:var(--soft);border:1px solid var(--border);border-radius:9px;padding:3px;overflow-x:auto;-webkit-overflow-scrolling:touch;scrollbar-width:none}
  nav.tabs::-webkit-scrollbar{display:none}
  nav.tabs button{flex:1;min-width:max-content;padding:8px 14px;border:0;background:transparent;color:var(--muted);font:inherit;font-size:13px;font-weight:500;cursor:pointer;border-radius:6px;transition:background .12s, color .12s;white-space:nowrap}
  nav.tabs button:hover{color:var(--text)}
  nav.tabs button.active{background:var(--card);color:var(--text);box-shadow:0 1px 2px rgba(0,0,0,0.15)}
  nav.tabs button .badge{display:inline-block;margin-left:6px;min-width:18px;padding:1px 6px;background:var(--accent-soft);color:var(--accent);border-radius:999px;font-size:10.5px;font-weight:600}

  /* Tab panels */
  .panel{display:none}
  .panel.active{display:block;animation:fadeIn .2s ease}
  @keyframes fadeIn{from{opacity:0;transform:translateY(2px)}to{opacity:1;transform:none}}

  /* Cards */
  .grid{display:grid;gap:14px;grid-template-columns:1fr 1fr}
  @media (max-width:760px){.grid{grid-template-columns:1fr}}
  .card{background:var(--card);border:1px solid var(--border);border-radius:10px;padding:16px 18px}
  .card.wide{grid-column:1/-1}
  .card-head{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:12px}
  .card-head h2{margin:0}
  h2{margin:0 0 12px;font-size:11.5px;font-weight:600;letter-spacing:0.6px;text-transform:uppercase;color:var(--muted)}
  .hint{margin:0 0 12px;font-size:12.5px;color:var(--muted);line-height:1.55}

  /* Key-value rows */
  .kv{display:flex;justify-content:space-between;gap:10px;padding:6px 0;border-bottom:1px solid var(--border);font-size:13px;align-items:flex-start}
  .kv:last-child{border-bottom:none}
  .kv > span:first-child{color:var(--muted);font-size:11px;text-transform:uppercase;letter-spacing:0.4px;flex-shrink:0;padding-top:1px}
  .kv > span:last-child{font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12.5px;word-break:break-all;text-align:right;max-width:60%}
  .kv .ok{color:var(--ok)}
  .kv .err{color:var(--err)}
  .kv .warn{color:var(--warn)}

  /* Buttons */
  .btn{font:inherit;font-size:12.5px;font-weight:500;padding:8px 14px;border-radius:6px;cursor:pointer;border:1px solid var(--border);background:var(--soft);color:var(--text);display:inline-flex;align-items:center;gap:6px;transition:background .12s, border-color .12s}
  .btn:hover{background:var(--hover);border-color:var(--border-strong)}
  .btn.primary{background:var(--accent);border-color:var(--accent);color:#fff}
  .btn.primary:hover{background:var(--accent-hover);border-color:var(--accent-hover)}
  .btn.warn{color:var(--warn);border-color:rgba(210,153,34,0.4)}
  .btn.danger{color:var(--err);border-color:rgba(248,81,73,0.4)}
  .btn.ghost{background:transparent}
  .btn:disabled{opacity:0.5;cursor:not-allowed}
  .btn-row{display:flex;gap:8px;margin-top:12px;flex-wrap:wrap}
  .btn-row.end{justify-content:flex-end}

  /* Log box */
  pre{background:var(--code-bg);border:1px solid var(--border);border-radius:7px;padding:10px;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:11.5px;line-height:1.55;color:var(--text);max-height:480px;overflow:auto;margin:0}
  @media (max-width:600px){pre{max-height:55vh;font-size:11px}}
  .log-line{display:flex;gap:10px;padding:1px 0}
  .log-line time{color:var(--muted);font-size:11px;flex-shrink:0}

  /* Forms */
  label{display:block;font-size:11px;color:var(--muted);text-transform:uppercase;letter-spacing:0.4px;margin:14px 0 4px;font-weight:600}
  input,select{width:100%;background:var(--code-bg);border:1px solid var(--border);color:var(--text);padding:9px 11px;border-radius:6px;font:inherit;font-size:13px}
  input:focus,select:focus{outline:none;border-color:var(--accent);box-shadow:0 0 0 3px var(--accent-soft)}

  /* Toast */
  .toast{position:fixed;bottom:24px;left:50%;transform:translateX(-50%) translateY(20px);background:var(--card);border:1px solid var(--border);color:var(--text);padding:11px 16px;border-radius:8px;font-size:13px;box-shadow:var(--shadow);transition:opacity .2s, transform .2s;opacity:0;pointer-events:none;max-width:90vw;text-align:center}
  .toast.show{opacity:1;transform:translateX(-50%) translateY(0)}
  .toast.ok{border-color:rgba(63,185,80,0.45)}
  .toast.err{border-color:rgba(248,81,73,0.45)}

  /* QR scanner panel */
  .step{display:flex;gap:14px;padding:14px 0;border-bottom:1px solid var(--border)}
  .step:last-child{border-bottom:none}
  .step-num{flex-shrink:0;width:28px;height:28px;border-radius:50%;background:var(--soft);border:1px solid var(--border);color:var(--muted);display:flex;align-items:center;justify-content:center;font-weight:600;font-size:13px}
  .step.done .step-num{background:var(--ok-soft);border-color:var(--ok);color:var(--ok)}
  .step.done .step-num::before{content:"✓"}
  .step.done .step-num > *{display:none}
  .step-body{flex:1;min-width:0}
  .step-body h3{margin:2px 0 4px;font-size:13.5px;font-weight:600}
  .step-body p{margin:0 0 8px;font-size:12.5px;color:var(--muted);line-height:1.5}
  .step-state{font-size:11.5px;font-weight:600;text-transform:uppercase;letter-spacing:0.4px}
  .step-state.ok{color:var(--ok)}
  .step-state.warn{color:var(--warn)}
  .step-state.err{color:var(--err)}

  /* Small phone tweaks */
  @media (max-width:480px){
    .wrap{padding:14px 12px 28px}
    h1{font-size:15.5px}
    .platform{display:none}
    .card{padding:14px 14px}
    .btn{padding:9px 12px}
    .header-actions{gap:6px}
    .kv > span:last-child{max-width:52%}
  }
</style>
</head>
<body>
<div class="wrap">
  <header>
    <div class="brand">
      <span class="brand-dot"></span>
      <h1>face_auth agent</h1>
      <span class="platform">{{.OS}}</span>
    </div>
    <div class="header-actions">
      <span id="pill" class="status-pill"><span class="dot"></span><span id="pill-text">connecting…</span></span>
      <button class="theme-toggle" onclick="toggleTheme()" title="Toggle light / dark">
        <svg class="sun" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41"/></svg>
        <svg class="moon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>
      </button>
    </div>
  </header>

  <nav class="tabs" role="tablist">
    <button data-tab="overview" class="active" onclick="showTab('overview')">Overview</button>
    <button data-tab="qr" onclick="showTab('qr')">QR scanner</button>
    <button data-tab="logs" onclick="showTab('logs')">Logs</button>
    <button data-tab="settings" onclick="showTab('settings')">Settings</button>
  </nav>

  <!-- ─── Overview ─── -->
  <div class="panel active" data-panel="overview">
    <div class="grid">
      <section class="card">
        <h2>Cloud connection</h2>
        <div class="kv"><span>Cloud URL</span><span id="kv-url">…</span></div>
        <div class="kv"><span>Agent ID</span><span id="kv-id">…</span></div>
        <div class="kv"><span>Agent name</span><span id="kv-name">…</span></div>
        <div class="kv"><span>Connected at</span><span id="kv-since">…</span></div>
        <div class="kv"><span>Last error</span><span id="kv-err">—</span></div>
        <div class="btn-row">
          <button class="btn" onclick="testConn()">Test connection</button>
          <button class="btn" onclick="showTab('settings')">Edit config</button>
        </div>
      </section>

      <section class="card">
        <h2>QR scanner</h2>
        <div class="kv"><span>Watcher</span><span id="kv-qr-running">—</span></div>
        <div class="kv"><span>Auto-start</span><span id="kv-qr-startup">—</span></div>
        <div class="kv"><span>AutoHotkey</span><span id="kv-qr-ahk">—</span></div>
        <div class="kv"><span>Listen port</span><span id="kv-port">…</span></div>
        <div class="kv"><span>Strip prefix</span><span id="kv-prefix">…</span></div>
        <div class="btn-row">
          <button class="btn primary" onclick="showTab('qr')">Manage QR watcher</button>
        </div>
      </section>
    </div>
  </div>

  <!-- ─── QR scanner ─── -->
  <div class="panel" data-panel="qr">
    <section class="card">
      <h2>QR watcher — bundled setup</h2>
      <p class="hint">The watcher captures keystrokes from your USB QR scanner and forwards each scan to the agent. Everything ships inside this app — there's nothing to download separately, except AutoHotkey itself if you don't already have it.</p>

      <div id="qr-platform-note" style="display:none">
        <p class="hint" style="color:var(--warn)">Auto-install is Windows-only. On Linux, point the agent at your HID device path under <a href="#" onclick="showTab('settings');return false" style="color:var(--accent)">Settings → QR device</a>.</p>
      </div>

      <div id="qr-steps">
        <div class="step" id="step-ahk">
          <div class="step-num"><span>1</span></div>
          <div class="step-body">
            <h3>AutoHotkey v2</h3>
            <p id="step-ahk-desc">Required to run the watcher script. Free, ~3 MB, takes 30 seconds to install.</p>
            <div class="btn-row">
              <a id="ahk-link" class="btn primary" href="#" target="_blank" rel="noopener">Download AutoHotkey v2</a>
              <button class="btn ghost" onclick="refreshQR()">I've installed it — recheck</button>
            </div>
          </div>
        </div>

        <div class="step" id="step-install">
          <div class="step-num"><span>2</span></div>
          <div class="step-body">
            <h3>Install QR watcher</h3>
            <p>Drops the bundled <code>face_auth-qr-watcher.ahk</code> into your <strong>Startup</strong> folder so it auto-runs on every boot, and starts it right now.</p>
            <div class="btn-row">
              <button id="btn-qr-install" class="btn primary" onclick="qrInstall()">Install &amp; start</button>
              <button id="btn-qr-stop" class="btn" onclick="qrStop()" style="display:none">Stop watcher</button>
              <button id="btn-qr-start" class="btn" onclick="qrStart()" style="display:none">Start watcher</button>
              <button id="btn-qr-remove" class="btn danger" onclick="qrUninstall()" style="display:none">Remove from auto-start</button>
            </div>
          </div>
        </div>

        <div class="step" id="step-test">
          <div class="step-num"><span>3</span></div>
          <div class="step-body">
            <h3>Test it</h3>
            <p>Scan any QR code into any text field. The watcher reads the scan in the background and POSTs it to <code id="qr-endpoint">http://127.0.0.1:7771/scan</code>. You'll see <code>QR /scan</code> entries appear under the Logs tab.</p>
            <div class="btn-row">
              <button class="btn" onclick="showTab('logs')">Open live logs</button>
              <a id="qr-download" class="btn ghost" href="/qr-watcher.ahk" download>Download .ahk script</a>
            </div>
          </div>
        </div>
      </div>

      <div class="kv" style="margin-top:14px"><span>Script path</span><span id="kv-qr-path">—</span></div>
      <div class="kv"><span>Watcher log</span><span id="kv-qr-log">—</span></div>
    </section>
  </div>

  <!-- ─── Logs ─── -->
  <div class="panel" data-panel="logs">
    <section class="card">
      <div class="card-head">
        <h2>Live agent log <span style="font-weight:400;text-transform:none;letter-spacing:0;color:var(--muted);font-size:11px;margin-left:6px">(streaming)</span></h2>
        <div style="display:flex;gap:6px">
          <button class="btn ghost" onclick="$('log').innerHTML=''" title="Clear">Clear</button>
        </div>
      </div>
      <pre id="log"></pre>
    </section>
  </div>

  <!-- ─── Settings ─── -->
  <div class="panel" data-panel="settings">
    <section class="card">
      <h2>Cloud configuration</h2>
      <p class="hint">These values come from your admin UI under <em>Agents → Add agent</em>. Saving reconnects immediately.</p>
      <label>Cloud URL</label>
      <input id="e-url" placeholder="https://face.example.com or http://localhost:8080">
      <label>Agent ID</label>
      <input id="e-id" placeholder="lan-bridge-01">
      <label>Agent token</label>
      <input id="e-token" placeholder="long random string from admin UI">
      <label>Display name (optional)</label>
      <input id="e-name" placeholder="Front desk PC">
      <div class="btn-row">
        <button class="btn primary" onclick="saveConfig()">Save &amp; reconnect</button>
        <button class="btn" onclick="testConn()">Test connection</button>
      </div>
    </section>

    <section class="card" style="margin-top:14px">
      <h2>QR scanner</h2>
      <label>Strip prefix (your scanner adds this before each code)</label>
      <input id="e-prefix" placeholder="e.g. in#">
      <label>Listen port (the watcher posts scans here)</label>
      <input id="e-port" placeholder="7771">
      <div class="btn-row">
        <button class="btn primary" onclick="saveConfig()">Save</button>
      </div>
    </section>

    <section class="card" style="margin-top:14px">
      <h2>System</h2>
      <p class="hint">Install the agent itself as a system service so it auto-starts on every boot (requires elevation on Windows / sudo on Linux). Reset wipes local config and reopens the setup wizard.</p>
      <div class="kv"><span>Config file</span><span style="font-size:11.5px">{{.ConfPath}}</span></div>
      <div class="btn-row">
        <button class="btn primary" onclick="installSvc()">Install as system service</button>
        <button class="btn warn" onclick="resetCfg()">Reset config &amp; reconfigure</button>
      </div>
    </section>
  </div>
</div>

<div id="toast" class="toast"></div>

<script>
const $ = (id) => document.getElementById(id);
let lastQR = null;

// ─── Theme ─────────────────────────────────────────────────
(function initTheme(){
  const saved = localStorage.getItem('face_auth.theme');
  const prefersLight = window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches;
  const theme = saved || (prefersLight ? 'light' : 'dark');
  document.documentElement.setAttribute('data-theme', theme);
})();
function toggleTheme(){
  const cur = document.documentElement.getAttribute('data-theme');
  const next = cur === 'dark' ? 'light' : 'dark';
  document.documentElement.setAttribute('data-theme', next);
  localStorage.setItem('face_auth.theme', next);
}

// ─── Tabs ──────────────────────────────────────────────────
function showTab(name){
  document.querySelectorAll('nav.tabs button').forEach(b => b.classList.toggle('active', b.dataset.tab === name));
  document.querySelectorAll('.panel').forEach(p => p.classList.toggle('active', p.dataset.panel === name));
  localStorage.setItem('face_auth.tab', name);
  if (name === 'qr') refreshQR();
  if (name === 'settings') populateSettings();
}
(function initTab(){
  const t = localStorage.getItem('face_auth.tab') || 'overview';
  showTab(t);
})();

// ─── Toast ─────────────────────────────────────────────────
function toast(text, kind='ok') {
  const t = $('toast');
  t.textContent = text;
  t.className = 'toast show ' + kind;
  clearTimeout(t._t);
  t._t = setTimeout(() => t.classList.remove('show'), 2800);
}

// ─── Status ────────────────────────────────────────────────
function statusClass(s) {
  if (s === 'connected') return 'ok';
  if (s === 'disconnected') return 'err';
  return '';
}

async function refresh() {
  try {
    const r = await fetch('/api/status').then(r => r.json());
    const s = r.status || {};
    $('pill').className = 'status-pill ' + statusClass(s.state);
    $('pill-text').textContent = s.state;
    $('kv-err').textContent = s.lastError || '—';
    $('kv-since').textContent = s.connectedAt && s.state === 'connected' ? new Date(s.connectedAt).toLocaleTimeString() : '—';
    if (r.config) {
      $('kv-url').textContent  = r.config.cloud_url || '—';
      $('kv-id').textContent   = r.config.agent_id || '—';
      $('kv-name').textContent = r.config.agent_name || '—';
      $('kv-prefix').textContent = r.config.qr_strip_prefix || '(none)';
      $('kv-port').textContent   = r.config.qr_listen_port || '7771';
      $('qr-endpoint').textContent = 'http://127.0.0.1:' + (r.config.qr_listen_port || '7771') + '/scan';
    }
  } catch (e) {}
}

// ─── Logs (SSE) ────────────────────────────────────────────
function fmtTime(d) {
  const dt = new Date(d);
  return dt.toLocaleTimeString([], {hour12:false}) + '.' + String(dt.getMilliseconds()).padStart(3,'0');
}
function appendLog(entry) {
  const el = document.createElement('div');
  el.className = 'log-line';
  el.innerHTML = '<time>' + fmtTime(entry.at) + '</time><span></span>';
  el.lastChild.textContent = entry.line;
  $('log').appendChild(el);
  // Cap to 1000 lines so the DOM doesn't drift
  while ($('log').children.length > 1000) $('log').removeChild($('log').firstChild);
  $('log').scrollTop = $('log').scrollHeight;
}
const es = new EventSource('/api/logs/stream');
es.onmessage = (e) => { try { appendLog(JSON.parse(e.data)) } catch {} };

// ─── Cloud connection ─────────────────────────────────────
async function testConn() {
  const cfg = await fetch('/api/config').then(r => r.json());
  toast('Testing connection…');
  const r = await fetch('/api/test', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify(cfg)}).then(r => r.json());
  toast(r.ok ? 'Cloud is reachable ✓' : ('Failed: ' + r.error), r.ok ? 'ok' : 'err');
}

async function populateSettings() {
  const cfg = await fetch('/api/config').then(r => r.json());
  $('e-url').value = cfg.cloud_url || '';
  $('e-id').value = cfg.agent_id || '';
  $('e-token').value = cfg.agent_token || '';
  $('e-name').value = cfg.agent_name || '';
  $('e-prefix').value = cfg.qr_strip_prefix || '';
  $('e-port').value = cfg.qr_listen_port || '7771';
}

async function saveConfig() {
  const body = {
    cloud_url: $('e-url').value.trim(),
    agent_id: $('e-id').value.trim(),
    agent_token: $('e-token').value.trim(),
    agent_name: $('e-name').value.trim(),
    qr_strip_prefix: $('e-prefix').value,
    qr_listen_port: $('e-port').value.trim(),
  };
  const r = await fetch('/api/config', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify(body)}).then(r => r.json());
  if (r.ok) {
    toast('Saved. Reconnecting…');
    setTimeout(refresh, 1500);
  } else {
    toast('Save failed: ' + (r.error||'unknown'), 'err');
  }
}

async function installSvc() {
  toast('Requesting elevation…');
  const r = await fetch('/api/install', {method:'POST'}).then(r => r.json());
  if (r.ok) toast('Service installed and running ✓');
  else toast('Install failed: ' + (r.error||'unknown'), 'err');
}

async function resetCfg() {
  if (!confirm('Wipe local config and start the setup wizard again? The agent will disconnect.')) return;
  const r = await fetch('/api/reset', {method:'POST'}).then(r => r.json());
  if (r.ok) {
    toast('Config wiped. Reloading…');
    setTimeout(() => location.reload(), 800);
  } else toast('Reset failed: ' + (r.error||'unknown'), 'err');
}

// ─── QR watcher ────────────────────────────────────────────
async function refreshQR() {
  try {
    const r = await fetch('/api/qr-watcher/status').then(r => r.json());
    lastQR = r;
    $('ahk-link').href = r.autohotkey_url || 'https://www.autohotkey.com/';

    if (!r.supported) {
      $('qr-platform-note').style.display = 'block';
      $('qr-steps').style.display = 'none';
      return;
    }
    $('qr-platform-note').style.display = 'none';
    $('qr-steps').style.display = '';

    // Step 1 — AutoHotkey
    $('step-ahk').classList.toggle('done', r.autohotkey_installed);
    $('step-ahk-desc').textContent = r.autohotkey_installed
      ? ('Detected at ' + (r.autohotkey_path || ''))
      : 'Required to run the watcher script. Free, ~3 MB, takes 30 seconds to install.';

    // Step 2 — Install + run state
    const installed = !!r.startup_installed;
    const running = !!r.running;
    $('step-install').classList.toggle('done', installed && running);
    $('btn-qr-install').style.display = installed ? 'none' : '';
    $('btn-qr-start').style.display   = (installed && !running) ? '' : 'none';
    $('btn-qr-stop').style.display    = running ? '' : 'none';
    $('btn-qr-remove').style.display  = installed ? '' : 'none';

    // Overview kv
    $('kv-qr-running').innerHTML  = running ? '<span class="ok">running</span>' : (installed ? '<span class="warn">stopped</span>' : '<span class="err">not installed</span>');
    $('kv-qr-startup').innerHTML  = installed ? '<span class="ok">enabled</span>' : '<span class="muted">disabled</span>';
    $('kv-qr-ahk').innerHTML      = r.autohotkey_installed ? '<span class="ok">installed</span>' : '<span class="err">missing</span>';
    $('kv-qr-path').textContent   = r.startup_path || '—';
    $('kv-qr-log').textContent    = r.log_path || '—';
  } catch (e) {}
}

async function qrInstall() {
  $('btn-qr-install').disabled = true;
  try {
    const r = await fetch('/api/qr-watcher/install', {method:'POST'}).then(r => r.json());
    if (r.ok) {
      const running = r.status && r.status.running;
      const ahk = r.status && r.status.autohotkey_installed;
      if (running) toast('Watcher installed and running ✓');
      else if (!ahk) toast('Installed to startup, but AutoHotkey is missing — install it then click Start.', 'err');
      else toast('Installed to startup. Click Start to run it now.', 'ok');
    } else {
      toast('Install failed: ' + (r.error||'unknown'), 'err');
    }
  } finally {
    $('btn-qr-install').disabled = false;
    refreshQR();
  }
}
async function qrStart() {
  const r = await fetch('/api/qr-watcher/start', {method:'POST'}).then(r => r.json());
  toast(r.ok ? 'Watcher started ✓' : ('Start failed: ' + (r.error||'unknown')), r.ok ? 'ok' : 'err');
  refreshQR();
}
async function qrStop() {
  const r = await fetch('/api/qr-watcher/stop', {method:'POST'}).then(r => r.json());
  toast(r.ok ? 'Watcher stopped' : ('Stop failed: ' + (r.error||'unknown')), r.ok ? 'ok' : 'err');
  refreshQR();
}
async function qrUninstall() {
  if (!confirm('Remove the QR watcher from auto-start and stop it now?')) return;
  const r = await fetch('/api/qr-watcher/uninstall', {method:'POST'}).then(r => r.json());
  toast(r.ok ? 'Removed from auto-start' : ('Remove failed: ' + (r.error||'unknown')), r.ok ? 'ok' : 'err');
  refreshQR();
}

// PWA — register the tiny service worker so the install button appears
if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js').catch(()=>{});

refresh();
refreshQR();
setInterval(refresh, 2000);
setInterval(refreshQR, 5000);
</script>
</body>
</html>`))
