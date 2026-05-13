package main

import "html/template"

var setupTpl = template.Must(template.New("setup").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>face_auth agent · setup</title>
<style>
  :root { --bg:#f6f8fa; --card:#fff; --text:#1f2328; --muted:#656d76; --border:#d8dee4; --accent:#0969da; --accent-soft:rgba(9,105,218,0.10); --ok:#1a7f37; --ok-soft:rgba(26,127,55,0.12); --err:#cf222e; --err-soft:rgba(207,34,46,0.10); }
  *{box-sizing:border-box} html,body{margin:0;background:var(--bg);color:var(--text);font:14px/1.45 -apple-system,BlinkMacSystemFont,Segoe UI,sans-serif}
  .wrap{max-width:640px;margin:32px auto;padding:0 16px}
  h1{font-size:22px;margin:0 0 4px}
  .sub{color:var(--muted);font-size:13px;margin-bottom:24px}
  .card{background:var(--card);border:1px solid var(--border);border-radius:10px;padding:20px;margin-bottom:16px;box-shadow:0 1px 3px rgba(0,0,0,0.04)}
  h2{margin:0 0 14px;font-size:13px;text-transform:uppercase;letter-spacing:0.4px;color:var(--muted);font-weight:600}
  label{display:block;font-size:12px;color:var(--muted);font-weight:500;margin:8px 0 4px;text-transform:uppercase;letter-spacing:0.4px}
  input{width:100%;background:#fff;border:1px solid var(--border);color:var(--text);padding:9px 11px;border-radius:7px;font:inherit;font-size:13.5px}
  input:focus{outline:none;border-color:var(--accent);box-shadow:0 0 0 3px var(--accent-soft)}
  .row{display:flex;gap:10px}
  .row > div{flex:1}
  .hint{color:var(--muted);font-size:11.5px;margin-top:3px}
  .btns{display:flex;gap:10px;margin-top:14px;flex-wrap:wrap}
  button{font:inherit;font-size:13.5px;font-weight:500;padding:9px 16px;border-radius:7px;cursor:pointer;border:1px solid transparent}
  .primary{background:var(--accent);color:#fff;border-color:var(--accent)}
  .ghost{background:#fff;color:var(--text);border-color:var(--border)}
  .ghost:hover{border-color:var(--muted)}
  .msg{padding:10px 12px;border-radius:7px;font-size:13px;margin-top:10px;display:none}
  .msg.ok{background:var(--ok-soft);color:var(--ok);border:1px solid rgba(26,127,55,0.3);display:block}
  .msg.err{background:var(--err-soft);color:var(--err);border:1px solid rgba(207,34,46,0.3);display:block}
  .step{display:none}
  .step.active{display:block}
  pre{background:#0d1117;color:#e6edf3;padding:12px;border-radius:7px;overflow:auto;font-size:11.5px;line-height:1.5;margin:8px 0}
  code{background:#f0f3f7;padding:1px 6px;border-radius:4px;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12px}
  .badge{display:inline-block;padding:2px 8px;border-radius:999px;font-size:11px;background:var(--accent-soft);color:var(--accent);margin-left:4px}
  .conf-path{color:var(--muted);font-size:11.5px;margin-top:8px;font-family:ui-monospace,Menlo,Consolas,monospace}
</style>
</head>
<body>
<div class="wrap">
  <h1>face_auth agent setup <span class="badge">{{.OS}}</span></h1>
  <div class="sub">First-time configuration. Settings are saved to your machine and the agent runs automatically afterwards.</div>

  <!-- Step 1: config -->
  <div class="step active" id="step-config">
    <div class="card">
      <h2>Cloud connection</h2>
      <label>Cloud URL</label>
      <input id="cloud_url" value="{{.Current.CloudURL}}" placeholder="https://face_auth.your-domain.com">
      <div class="hint">The public URL of your face_auth server. Use <code>https://</code> for production, <code>http://</code> for local testing.</div>

      <div class="row" style="margin-top:10px">
        <div>
          <label>Agent ID</label>
          <input id="agent_id" value="{{.Current.AgentID}}" placeholder="office-front">
        </div>
        <div>
          <label>Agent token</label>
          <input id="agent_token" value="{{.Current.AgentToken}}" placeholder="from the admin UI">
        </div>
      </div>
      <div class="hint">Generate both in the admin UI's Agents tab → "Add agent".</div>

      <label style="margin-top:10px">Display name</label>
      <input id="agent_name" value="{{.Current.AgentName}}" placeholder="Office front door — {{.Hostname}}">
    </div>

    <div class="card">
      <h2>USB QR scanner (optional)</h2>
      <label>Strip prefix</label>
      <input id="qr_strip_prefix" value="{{.Current.QRStripPrefix}}" placeholder="in#">
      <div class="hint">If your scanner is programmed to add a prefix (most are), enter it here. Leave empty if it emits the QR payload directly.</div>

      <label style="margin-top:10px">Local HTTP listen port</label>
      <input id="qr_listen_port" value="{{.Current.QRListenPort}}" placeholder="7771">
      <div class="hint">The companion AHK / HID helper POSTs scans to <code>http://127.0.0.1:&lt;port&gt;/scan</code>. Default 7771.</div>

      {{if eq .OS "linux"}}
      <label style="margin-top:10px">HID device path (Linux only)</label>
      <input id="qr_device" value="{{.Current.QRDevice}}" placeholder="/dev/input/by-id/usb-Scanner-event-kbd">
      <div class="hint">If set, the agent reads keystrokes from this device natively. Leave empty if using the AHK helper or HTTP endpoint.</div>
      {{end}}
    </div>

    <div class="btns">
      <button class="ghost" id="btn-test">Test connection</button>
      <button class="primary" id="btn-save">Save &amp; continue →</button>
    </div>
    <div class="msg" id="msg-config"></div>
    <div class="conf-path">Config will be written to: {{.ConfPath}}</div>
  </div>

  <!-- Step 2: install -->
  <div class="step" id="step-install">
    <div class="card">
      <h2>Run as a system service</h2>
      <p style="margin:0 0 12px;font-size:13.5px">
        The agent should run automatically every time this machine starts. Click <strong>Install service</strong> to set it up — this will require admin/sudo privileges briefly.
      </p>
      {{if eq .OS "windows"}}
      <p style="font-size:12.5px;color:var(--muted);margin:0 0 8px">
        On Windows, a UAC prompt will appear. The service runs as <code>Local System</code>, starts on boot, and restarts automatically on failure.
      </p>
      {{else if eq .OS "linux"}}
      <p style="font-size:12.5px;color:var(--muted);margin:0 0 8px">
        On Linux, a systemd unit will be written to <code>/etc/systemd/system/face_auth-agent.service</code>. You'll be prompted for your sudo password.
      </p>
      {{else if eq .OS "darwin"}}
      <p style="font-size:12.5px;color:var(--muted);margin:0 0 8px">
        On macOS, a launchd plist will be written to <code>~/Library/LaunchAgents/</code>. No admin password needed for user agents.
      </p>
      {{end}}
    </div>
    <div class="btns">
      <button class="ghost" id="btn-skip">Skip — I'll run it manually</button>
      <button class="primary" id="btn-install">Install service</button>
    </div>
    <div class="msg" id="msg-install"></div>
  </div>

  <!-- Step 3: done -->
  <div class="step" id="step-done">
    <div class="card">
      <h2 style="color:var(--ok)">Agent is configured</h2>
      <p style="margin:0;font-size:13.5px" id="done-text"></p>
    </div>
    <div class="btns">
      <button class="primary" id="btn-close">Close this tab</button>
    </div>
  </div>
</div>

<script>
const $ = (id) => document.getElementById(id);
const show = (which) => {
  document.querySelectorAll('.step').forEach(s => s.classList.remove('active'));
  $('step-' + which).classList.add('active');
};
const setMsg = (id, ok, text) => {
  const el = $('msg-' + id);
  el.className = 'msg ' + (ok ? 'ok' : 'err');
  el.textContent = text;
};
const getCfg = () => ({
  cloud_url:      $('cloud_url').value.trim(),
  agent_id:       $('agent_id').value.trim(),
  agent_token:    $('agent_token').value.trim(),
  agent_name:     $('agent_name').value.trim(),
  qr_strip_prefix:$('qr_strip_prefix').value,
  qr_listen_port: $('qr_listen_port').value.trim(),
  qr_device:      $('qr_device') ? $('qr_device').value.trim() : '',
});

$('btn-test').onclick = async () => {
  setMsg('config', true, 'Testing…');
  try {
    const r = await fetch('/api/test', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify(getCfg())}).then(r => r.json());
    setMsg('config', r.ok, r.ok ? 'Connected to cloud successfully.' : ('Failed: ' + r.error));
  } catch (e) { setMsg('config', false, String(e)); }
};

$('btn-save').onclick = async () => {
  try {
    const r = await fetch('/api/save', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify(getCfg())}).then(r => r.json());
    if (!r.ok) return setMsg('config', false, r.error || 'save failed');
    show('install');
  } catch (e) { setMsg('config', false, String(e)); }
};

$('btn-install').onclick = async () => {
  setMsg('install', true, 'Installing…');
  try {
    const r = await fetch('/api/install', {method:'POST'}).then(r => r.json());
    if (!r.ok) return setMsg('install', false, r.error + (r.output ? '\n\n' + r.output : ''));
    $('done-text').textContent = 'Service installed and started. You can close this tab — the agent now runs in the background.';
    show('done');
  } catch (e) { setMsg('install', false, String(e)); }
};

$('btn-skip').onclick = async () => {
  await fetch('/api/skip-install', {method:'POST'});
  $('done-text').textContent = 'Config saved. To start the agent manually, run the executable again (or close and reopen).';
  show('done');
};

$('btn-close').onclick = () => window.close();
</script>
</body>
</html>`))
