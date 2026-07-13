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
  .myt-clock{font-size:12px;font-variant-numeric:tabular-nums;color:var(--muted);letter-spacing:0.02em;font-family:ui-monospace,Menlo,Consolas,monospace;flex-shrink:0}
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
      <span class="platform">v{{.Version}} · {{.OS}}</span>
    </div>
    <div class="header-actions">
      <span id="myt-clock" class="myt-clock" title="Malaysia Time (MYT = UTC+8)"></span>
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
        <div class="kv"><span>Scanner capture</span><span id="kv-qr-running">—</span></div>
        <div class="kv"><span>Capture method</span><span id="kv-qr-startup">—</span></div>
        <div class="kv"><span>Legacy helper</span><span id="kv-qr-ahk">—</span></div>
        <div class="kv"><span id="kv-port-label">Listen port</span><span id="kv-port">…</span></div>
        <div class="kv"><span>Strip prefix</span><span id="kv-prefix">…</span></div>
        <div class="btn-row">
          <button class="btn primary" onclick="showTab('qr')">Manage QR watcher</button>
        </div>
      </section>

      <section class="card wide">
        <div class="card-head">
          <h2>Third-party QR scanner stations</h2>
          <button class="btn ghost" onclick="loadStations()">Refresh</button>
        </div>
        <p class="hint">All configured USB/HID scanner routes on this agent. Each local port is isolated and opens only its assigned Hikvision door.</p>
        <div id="overview-station-list"><p class="hint">Loading scanner stations…</p></div>
        <div class="btn-row">
          <button class="btn primary" onclick="showTab('qr')">Manage scanner stations</button>
        </div>
      </section>
    </div>
  </div>

  <!-- ─── QR scanner ─── -->
  <div class="panel" data-panel="qr">

    <!-- Scanner stations card (always shown) -->
    <section class="card" style="margin-bottom:14px">
      <div class="card-head">
        <h2>Third-party QR scanner stations</h2>
        <button class="btn ghost" onclick="loadStations()">Refresh</button>
      </div>
      <p class="hint"><strong>External USB/HID QR scanners only.</strong> Each station maps one third-party scanner to a door. Do not add the Hikvision device's built-in QR/card scanner here; configure that separately in Admin → Access points using Hikvision mode.</p>
      <div id="station-list"></div>
      <div style="margin-top:14px;border-top:1px solid var(--border);padding-top:14px">
        <h2 id="station-form-title" style="margin-bottom:8px">Add station <span class="hint">(maximum 4 per agent)</span></h2>
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:10px">
          <div>
            <label style="margin-top:0">Station name</label>
            <input id="st-name" placeholder="e.g. Main entrance">
          </div>
          <div>
            <label style="margin-top:0">Hikvision device — SN (Name)</label>
            <select id="st-device">
              <option value="">Loading outlet devices…</option>
            </select>
          </div>
          <div>
            <label style="margin-top:0">Relay / door ID</label>
            <input id="st-door" type="number" value="1" min="1">
            <span class="hint">Use 1 for a normal single-relay Hikvision terminal. IN/OUT is selected separately.</span>
          </div>
          <div>
            <label style="margin-top:0">Local HTTP port</label>
            <input id="st-port" type="number" value="7771" min="1" max="65535">
            <span class="hint">Scanner 1: 7771, Scanner 2: 7772</span>
          </div>
          <div>
            <label style="margin-top:0">Direction</label>
            <select id="st-dir">
              <option value="in">In</option>
              <option value="out">Out</option>
              <option value="">Any</option>
            </select>
          </div>
        </div>
        <div class="btn-row" style="margin-top:10px">
          <button id="btn-add-station" class="btn primary" onclick="addStation()">Add station</button>
          <button id="btn-cancel-station" class="btn" style="display:none" onclick="cancelEditStation()">Cancel edit</button>
        </div>
      </div>
    </section>

    <!-- Physical scanner identification and assignment (Windows only) -->
    <section class="card" id="native-scanner-card" style="display:none;margin-bottom:14px">
      <div class="card-head"><h2>Bind Windows USB port to station</h2><button class="btn ghost" onclick="loadScanners()">Refresh devices</button></div>
      <p class="hint">Each detected Windows USB/HID device path is isolated. Identify the scanner, then bind that USB interface to a station and its local HTTP port.</p>
      <div class="btn-row"><button class="btn primary" onclick="identifyScanner()">Identify scanner</button><span id="identify-state" class="hint" style="margin-left:8px">Connect a scanner, click Identify, then scan any test QR.</span></div>
      <div id="scanner-list" style="margin-top:12px"></div>
      <div id="scanner-bindings" style="margin-top:12px"></div>
    </section>
    <section class="card">
      <h2 id="qr-setup-title">QR scanner — bundled setup</h2>
      <p class="hint" id="qr-setup-intro">Checking which scanner capture method this agent is using…</p>

      <div id="native-setup" style="display:none">
        <div class="step done">
          <div class="step-num"><span>1</span></div>
          <div class="step-body">
            <h3>Native scanner capture</h3>
            <p>The FaceAuth agent reads each bound Windows USB scanner directly using HID POS or Raw Input and routes it only to its assigned station and door.</p>
            <div id="native-capture-state" class="step-state">Checking…</div>
          </div>
        </div>
        <div class="step done">
          <div class="step-num"><span>2</span></div>
          <div class="step-body">
            <h3>What you need to download</h3>
            <p><strong>FaceAuth Windows agent v{{.Version}} only.</strong> Native scanner capture is already bundled inside the agent. Do not install AutoHotkey or the legacy QR watcher for bound scanner stations. Windows HID scanners normally need no separate driver.</p>
            <div class="step-state ok">No separate watcher download required</div>
          </div>
        </div>
        <div class="step">
          <div class="step-num"><span>3</span></div>
          <div class="step-body">
            <h3>Scanner assignment</h3>
            <p id="native-bound-count">Checking bound scanners…</p>
            <div class="btn-row">
              <button class="btn primary" onclick="loadScanners()">Refresh USB scanners</button>
              <button class="btn" onclick="showTab('logs')">Open native-reader logs</button>
            </div>
          </div>
        </div>
      </div>

      <div id="qr-platform-note" style="display:none">
        <p class="hint" style="color:var(--warn)">Auto-install is Windows-only. On Linux, point the agent at your HID device path under <a href="#" onclick="showTab('settings');return false" style="color:var(--accent)">Settings → QR device</a>.</p>
      </div>

      <div id="legacy-setup">
       <div id="qr-steps">
        <div class="step" id="step-ahk">
          <div class="step-num"><span>1</span></div>
          <div class="step-body">
            <h3>{{if eq .OS "darwin"}}macOS keyboard access{{else}}AutoHotkey v2{{end}}</h3>
            <p id="step-ahk-desc">{{if eq .OS "darwin"}}The native watcher requires Accessibility permission to read USB scanner keystrokes.{{else}}Required to run the watcher script. Free, ~3 MB, takes 30 seconds to install.{{end}}</p>
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
      </div>
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
      <p id="station-routing-note" class="hint"></p>
      <label>Strip prefix (your scanner adds this before each code)</label>
      <input id="e-prefix" placeholder="e.g. in#">
      <div id="legacy-route-settings">
        <label>Legacy single-scanner listen port</label>
        <input id="e-port" placeholder="7771">
        <label style="margin-top:14px;display:flex;align-items:center;gap:8px;text-transform:none;font-size:13px;color:var(--text)">
          <input id="e-direct-open" type="checkbox" style="width:auto">
          Legacy single-scanner QR-only direct opening
        </label>
        <label>Legacy target device ID</label>
        <input id="e-target-device" placeholder="used only when no scanner stations exist">
      </div>
      <p class="hint">In station mode, each bound scanner uses its own local port, Hikvision device, door, and direction.</p>
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
const agentOS = '{{.OS}}';
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
      updateOverviewPortSummary();
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
  $('e-target-device').value = cfg.qr_target_device || '';
  $('e-direct-open').checked = Boolean(cfg.qr_direct_open);
}

async function saveConfig() {
  const body = {
    cloud_url: $('e-url').value.trim(),
    agent_id: $('e-id').value.trim(),
    agent_token: $('e-token').value.trim(),
    agent_name: $('e-name').value.trim(),
    qr_strip_prefix: $('e-prefix').value,
    qr_listen_port: $('e-port').value.trim(),
    qr_target_device: $('e-target-device').value.trim(),
    qr_direct_open: $('e-direct-open').checked,
  };
  if (body.qr_direct_open && !body.qr_target_device) {
    toast('Target device ID is required for direct opening', 'err');
    return;
  }
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
  if (agentOS === 'windows') { loadStations(); loadScanners(); } else loadStations();
  try {
    const r = await fetch('/api/qr-watcher/status').then(r => r.json());
    lastQR = r;
    $('ahk-link').href = r.autohotkey_url || 'https://www.autohotkey.com/';
    const nativeCapture = r.capture_mode === 'native_usb';
    const captureActive = typeof r.capture_active === 'boolean' ? r.capture_active : !!r.running;

    $('native-setup').style.display = nativeCapture ? '' : 'none';
    $('legacy-setup').style.display = nativeCapture ? 'none' : '';
    if (nativeCapture) {
      $('qr-setup-title').textContent = 'QR scanner — native bundled setup';
      $('qr-setup-intro').textContent = 'Current method: native Windows USB HID / Raw Input. The FaceAuth agent itself is the scanner watcher.';
      $('native-capture-state').className = 'step-state ' + (captureActive ? 'ok' : 'err');
      $('native-capture-state').textContent = captureActive ? 'Active' : 'Offline — reconnect the USB scanner and refresh devices';
      const count = Number(r.native_bound_scanners || 0);
      $('native-bound-count').textContent = count
        ? count + ' USB scanner' + (count === 1 ? ' is' : 's are') + ' bound to native routing.'
        : 'No USB scanner is bound yet. Identify and bind a scanner above.';
    } else {
      $('qr-setup-title').textContent = 'QR watcher — legacy bundled setup';
      $('qr-setup-intro').textContent = 'Legacy single-scanner mode uses the keyboard watcher below. AutoHotkey is required on Windows only for this mode.';
    }

    if (!r.supported) {
      $('qr-platform-note').style.display = 'block';
      $('qr-steps').style.display = 'none';
      return;
    }
    $('qr-platform-note').style.display = 'none';
    $('qr-steps').style.display = '';

    // Step 1 — AutoHotkey
    $('step-ahk').classList.toggle('done', r.autohotkey_installed);
    $('step-ahk-desc').textContent = agentOS === 'darwin'
      ? (r.running ? 'Native watcher has keyboard access and is running.' : 'Allow face_auth-qr-watcher under System Settings → Privacy & Security → Accessibility.')
      : (r.autohotkey_installed ? ('Detected at ' + (r.autohotkey_path || '')) : 'Required to run the watcher script. Free, ~3 MB, takes 30 seconds to install.');

    // Step 2 — Install + run state
    const installed = !!r.startup_installed;
    const running = !!r.running;
    $('step-install').classList.toggle('done', installed && running);
    $('btn-qr-install').style.display = installed ? 'none' : '';
    $('btn-qr-start').style.display   = (installed && !running) ? '' : 'none';
    $('btn-qr-stop').style.display    = running ? '' : 'none';
    $('btn-qr-remove').style.display  = installed ? '' : 'none';

    // Overview reports the selected capture path. A bound Windows scanner uses
    // the agent's native HID/Raw Input reader, so a stopped AutoHotkey helper is
    // expected and must not make the whole scanner look offline.
    $('kv-qr-running').innerHTML = captureActive
      ? '<span class="ok">active</span>'
      : '<span class="err">offline</span>';
    $('kv-qr-startup').innerHTML = nativeCapture
      ? '<span class="ok">Native USB HID / Raw Input</span>'
      : '<span class="ok">Legacy keyboard watcher</span>';
    $('kv-qr-ahk').innerHTML = nativeCapture
      ? '<span class="muted">not used</span>'
      : (running ? '<span class="ok">running</span>' : (installed ? '<span class="warn">stopped</span>' : '<span class="err">not installed</span>'));
    $('kv-qr-path').textContent   = r.startup_path || '—';
    $('kv-qr-log').textContent    = r.log_path || '—';
  } catch (e) {}
}

function scannerLabel(d) {
  const serial = d.serialNumber ? (' · serial ' + d.serialNumber) : ' · no serial (USB port may matter)';
  const mode = d.readerMode === 'hid_pos' ? ' · HID POS direct (preferred)' : ' · Raw Input fallback';
  return (d.name || 'HID scanner') + ' · ' + (d.vendorId || '----') + ':' + (d.productId || '----') + mode + serial;
}

// ─── Scanner stations ────────────────────────────────────────────
let _stations = [];
let _stationDevices = [];
let _editingStationID = '';

async function loadStationDevices() {
  try {
    const r = await fetch('/api/station-devices').then(x => x.json());
    if (r.error) throw new Error(r.error);
    _stationDevices = r.devices || [];
    renderStationDeviceOptions();
    renderOverviewStations();
  } catch (e) {
    $('st-device').innerHTML = '<option value="">Unable to load outlet devices — check agent log</option>';
  }
}

function renderStationDeviceOptions() {
  const select = $('st-device');
  const selected = select.value;
  const assigned = new Set(_stations.map(s => String(s.device_id || '').toLowerCase()));
  const available = _stationDevices.filter(d => !assigned.has(String(d.deviceId).toLowerCase()) || d.deviceId === selected);
  select.innerHTML = '<option value="">— Select unpaired Hikvision device —</option>' + available.map(d =>
    '<option value="'+esc(d.deviceId)+'">'+esc(d.deviceId+' ('+(d.name||'Unnamed device')+')'+(d.online?' · online':' · offline'))+'</option>'
  ).join('');
  if (available.some(d => d.deviceId === selected)) select.value = selected;
  if (!_stationDevices.length) select.innerHTML = '<option value="">No devices assigned to this outlet</option>';
  else if (!available.length) select.innerHTML = '<option value="">All outlet devices already have a scanner</option>';
}

async function loadStations() {
  try {
    const r = await fetch('/api/stations').then(x => x.json());
    _stations = r.stations || [];
    renderStations();
    renderOverviewStations();
    updateOverviewPortSummary();
    updateRoutingMode();
    if (_stationDevices.length) renderStationDeviceOptions();
    const addButton = $('btn-add-station');
    if (addButton) {
      addButton.disabled = _stations.length >= 4;
      addButton.textContent = _stations.length >= 4 ? 'Maximum 4 stations reached' : 'Add station';
    }
    const portInput = $('st-port');
    if (portInput && document.activeElement !== portInput) {
      const used = new Set(_stations.map(s => Number(s.port)));
      let next = 7771; while (used.has(next) || next === 7780) next++;
      portInput.value = String(next);
    }
  } catch (e) {}
}

function updateOverviewPortSummary() {
  const label = $('kv-port-label');
  const value = $('kv-port');
  if (!label || !value) return;
  if (!_stations.length) {
    label.textContent = 'Listen port';
    return;
  }
  const ports = _stations.map(s => Number(s.port)).filter(Boolean).sort((a, b) => a - b);
  label.textContent = 'Station ports';
  value.textContent = ports.map(p => ':' + p).join(', ');
}

function stationDeviceLabel(deviceId) {
  const device = _stationDevices.find(d => String(d.deviceId).toLowerCase() === String(deviceId).toLowerCase());
  if (!device) return deviceId || 'Not assigned';
  return device.deviceId + ' (' + (device.name || 'Unnamed device') + ')';
}

function renderOverviewStations() {
  const el = $('overview-station-list');
  if (!el) return;
  if (!_stations.length) {
    el.innerHTML = '<p class="hint">No third-party QR scanner stations configured on this agent.</p>';
    return;
  }
  el.innerHTML = _stations
    .slice()
    .sort((a, b) => Number(a.port) - Number(b.port))
    .map(s => {
      const bound = s.hardware_id
        ? '<span class="ok">USB bound</span>'
        : '<span class="warn">USB not bound</span>';
      const direction = s.direction ? String(s.direction).toUpperCase() : 'ANY';
      const debugURL = 'http://127.0.0.1:' + Number(s.port) + '/qrdebug';
      return '<div style="padding:11px 0;border-bottom:1px solid var(--border)">' +
        '<div style="display:flex;justify-content:space-between;align-items:flex-start;gap:10px;flex-wrap:wrap">' +
          '<div style="min-width:0;flex:1">' +
            '<strong>' + esc(s.name || s.id) + '</strong> · ' + bound +
            '<div style="font-size:12px;color:var(--muted);margin-top:3px">' +
              'Port <strong style="color:var(--text)">:' + Number(s.port) + '</strong>' +
              ' → ' + esc(stationDeviceLabel(s.device_id)) +
              ' → Door ' + Number(s.door_no || 1) + ' · ' + esc(direction) +
            '</div>' +
            (s.hardware_id ? '<div style="font-size:9px;color:var(--muted);word-break:break-all;margin-top:3px">USB/HID: ' + esc(s.hardware_id) + '</div>' : '') +
          '</div>' +
          '<a class="btn ghost" href="' + debugURL + '" target="_blank" rel="noopener">Port ' + Number(s.port) + ' debug</a>' +
        '</div>' +
      '</div>';
    }).join('');
}

function updateRoutingMode() {
  const stationMode = _stations.length > 0;
  const legacy = $('legacy-route-settings');
  const note = $('station-routing-note');
  if (!legacy || !note) return;
  legacy.style.display = stationMode ? 'none' : '';
  note.innerHTML = stationMode
    ? '<strong class="ok">Per-station routing active.</strong> The global one-port route is disabled. Each scanner must be USB-bound to its station.'
    : '<strong class="warn">Legacy single-scanner mode.</strong> Add stations for separate scanner-to-gate routing.';
}

function renderStations() {
  const el = $('station-list');
  if (!_stations.length) {
    el.innerHTML = '<p class="hint">No stations configured yet. Add one below.</p>';
    return;
  }
  el.innerHTML = _stations.map(s => {
    const hw = s.hardware_id ? '<span class="ok" style="font-size:11px">USB port bound</span>' : '<span class="warn" style="font-size:11px">No USB port bound yet</span>';
    return '<div class="kv" style="display:flex;align-items:flex-start;gap:10px;padding:10px 0;border-bottom:1px solid var(--border)">' +
      '<div style="flex:1;min-width:0">' +
        '<strong>'+esc(s.name||s.id)+'</strong> '+hw+'<br>' +
        '<span style="font-size:11px;color:var(--muted)">Device: '+esc(s.device_id)+' · Door '+s.door_no+(s.direction?' · '+s.direction:'')+' · Port :'+s.port+'</span>' +
        (s.hardware_id?'<div style="font-size:9px;color:var(--muted);word-break:break-all">USB/HID: '+esc(s.hardware_id)+'</div>':'') +
      '</div>' +
      '<div class="btn-row" style="margin:0;flex-shrink:0">' +
        '<button class="btn" onclick="editStation(\''+esc(s.id)+'\')">Edit</button>' +
        '<button class="btn danger" onclick="deleteStation(\''+esc(s.id)+'\')">Delete</button>' +
      '</div>' +
    '</div>';
  }).join('');
}

async function addStation() {
  if (!_editingStationID && _stations.length >= 4) { toast('Maximum 4 scanner stations per agent', 'err'); return; }
  const name = $('st-name').value.trim();
  const device = $('st-device').value;
  const door = parseInt($('st-door').value) || 1;
  const port = parseInt($('st-port').value) || 0;
  const dir = $('st-dir').value;
  if (!device) { toast('Select a Hikvision device', 'err'); return; }
  if (door > 1 && !confirm('Door ID ' + door + ' is only valid for a multi-door Hikvision controller. Normal face terminals use Door 1. Save Door ' + door + ' anyway?')) return;
  const existing = _stations.find(s => s.id === _editingStationID);
  const r = await fetch('/api/stations', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({id: _editingStationID, name: name||device, scanner_source:'third_party_usb', hardware_id: existing ? existing.hardware_id : '', device_id: device, door_no: door, direction: dir, port}),
  }).then(x => x.json());
  if (r.ok) {
    toast(_editingStationID ? 'Station updated ✓' : 'Station added ✓');
    cancelEditStation();
    await loadStations();
    if (agentOS === 'windows') loadScanners();
  } else {
    toast('Failed: ' + r.error, 'err');
  }
}

function editStation(id) {
  const s = _stations.find(x => x.id === id);
  if (!s) return;
  _editingStationID = s.id;
  $('st-name').value = s.name || '';
  $('st-device').value = s.device_id || '';
  $('st-door').value = String(s.door_no || 1);
  $('st-port').value = String(s.port || 7771);
  $('st-dir').value = s.direction || '';
  $('station-form-title').innerHTML = 'Edit station <span class="hint">· USB binding will be preserved</span>';
  $('btn-add-station').textContent = 'Save station';
  $('btn-add-station').disabled = false;
  $('btn-cancel-station').style.display = '';
  renderStationDeviceOptions();
  $('st-device').value = s.device_id || '';
  $('station-form-title').scrollIntoView({behavior:'smooth', block:'center'});
}

function cancelEditStation() {
  _editingStationID = '';
  $('st-name').value = '';
  $('st-device').value = '';
  $('st-door').value = '1';
  $('st-dir').value = 'in';
  $('station-form-title').innerHTML = 'Add station <span class="hint">(maximum 4 per agent)</span>';
  $('btn-add-station').textContent = _stations.length >= 4 ? 'Maximum 4 stations reached' : 'Add station';
  $('btn-add-station').disabled = _stations.length >= 4;
  $('btn-cancel-station').style.display = 'none';
  renderStationDeviceOptions();
}

async function deleteStation(id) {
  if (!confirm('Delete this station?')) return;
  const r = await fetch('/api/stations/delete', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({id}),
  }).then(x => x.json());
  toast(r.ok ? 'Station deleted' : ('Failed: ' + r.error), r.ok ? 'ok' : 'err');
  if (r.ok) { await loadStations(); if (agentOS === 'windows') loadScanners(); }
}

async function loadScanners() {
  if (agentOS !== 'windows') return;
  $('native-scanner-card').style.display = '';
  try {
    const r = await fetch('/api/scanners').then(x => x.json());
    const devices = r.devices || [], bindings = r.bindings || [];
    const boundHardware = new Set(bindings.map(b => (b.hardware_id || '').toLowerCase()));
    // Also mark stations that already have a hardware_id
    const stationBound = new Set(_stations.filter(s => s.hardware_id).map(s => s.hardware_id.toLowerCase()));
    const allBound = new Set([...boundHardware, ...stationBound]);
    const stationOptions = _stations.map(s =>
      '<option value="'+esc(s.id)+'">'+esc((s.name||s.id)+' (door '+s.door_no+')')+'</option>'
    ).join('');
    $('scanner-list').innerHTML = devices.length ? devices.map((d, i) => {
      const bound = allBound.has((d.hardwareId || '').toLowerCase());
      return '<div class="kv" style="display:block;padding:10px"><div><strong>'+esc(scannerLabel(d))+'</strong></div>' +
        '<div style="font-size:10px;color:var(--muted);word-break:break-all">'+esc(d.devicePath||d.hardwareId)+'</div>' +
        (bound
          ? '<span class="ok" style="font-size:11px">Already assigned</span>'
          : (_stations.length
              ? '<div class="btn-row"><select id="scanner-target-'+i+'"><option value="">— Select station / local HTTP port —</option>'+stationOptions+'</select><button class="btn primary" onclick="bindScannerToStation('+i+')">Bind USB port</button></div>'
              : '<span class="warn" style="font-size:11px">Add a station first</span>'
            )
        ) + '</div>';
    }).join('') : '<p class="hint" style="color:var(--warn)">No external HID scanner detected. Connect it, then click Refresh.</p>';
    window._scannerDevices = devices;
    $('scanner-bindings').innerHTML = '';
    if (r.lastHardwareId) {
      const d = devices.find(x => x.hardwareId === r.lastHardwareId);
      $('identify-state').textContent = 'Last scan from: ' + (d ? scannerLabel(d) : r.lastHardwareId);
    }
  } catch (e) { $('scanner-list').textContent = 'Scanner discovery failed: '+e; }
}

async function bindScannerToStation(i) {
  const d = (window._scannerDevices || [])[i];
  const stationId = $('scanner-target-'+i) && $('scanner-target-'+i).value;
  if (!d || !stationId) return toast('Select a station first', 'err');
  const r = await fetch('/api/stations/assign', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({station_id: stationId, hardware_id: d.hardwareId}),
  }).then(x => x.json());
  toast(r.ok ? 'Scanner assigned to station ✓' : ('Failed: ' + r.error), r.ok ? 'ok' : 'err');
  if (r.ok) { await loadStations(); setTimeout(loadScanners, 600); }
}

async function identifyScanner() {
  await fetch('/api/scanners/identify', {method:'POST'});
  $('identify-state').textContent = 'Waiting for a scan…';
  let tries = 0;
  const timer = setInterval(async () => { await loadScanners(); if (++tries >= 20 || !$('identify-state').textContent.includes('Waiting')) clearInterval(timer); }, 500);
}

function esc(v) { const d=document.createElement('div'); d.textContent=String(v==null?'':v); return d.innerHTML; }

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

// ─── MYT World Clock ──────────────────────────────────────
function tickMYT() {
  const el = $('myt-clock');
  if (!el) return;
  const now = new Date();
  const time = now.toLocaleTimeString('en-MY', {
    timeZone: 'Asia/Kuala_Lumpur',
    hour12: false,
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
  const date = now.toLocaleDateString('en-MY', {
    timeZone: 'Asia/Kuala_Lumpur',
    day: '2-digit',
    month: 'short',
  });
  el.textContent = 'MYT ' + date + ' ' + time;
}
tickMYT();
setInterval(tickMYT, 1000);

// PWA — register the tiny service worker so the install button appears
if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js').catch(()=>{});

refresh();
refreshQR();
loadStations();
loadStationDevices();
setInterval(refresh, 2000);
setInterval(refreshQR, 5000);
</script>
</body>
</html>`))
