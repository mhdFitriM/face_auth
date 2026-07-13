package main

import (
	"encoding/json"
	"fmt"
)

func qrDebugPage() string {
	scanLogMu.Lock()
	snap := make([]ScanEntry, len(scanLog))
	copy(snap, scanLog)
	scanLogMu.Unlock()
	rowsJSON, _ := json.Marshal(snap)
	return fmt.Sprintf(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>QR Debug · face_auth agent</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:system-ui,sans-serif;background:#0f172a;color:#e2e8f0;padding:24px}
h1{font-size:1.2rem;font-weight:600;margin-bottom:16px;color:#f1f5f9}
.sub{font-size:.8rem;color:#64748b;margin-bottom:20px}
table{width:100%%;border-collapse:collapse;font-size:.85rem}
th{text-align:left;padding:8px 10px;border-bottom:1px solid #1e293b;color:#94a3b8;font-weight:500;white-space:nowrap}
td{padding:8px 10px;border-bottom:1px solid #1e293b;vertical-align:top}
.ok{color:#4ade80;font-weight:600}
.fail{color:#f87171;font-weight:600}
.qr{font-family:monospace;color:#cbd5e1;max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.member{display:block;font-family:system-ui,sans-serif;color:#38bdf8;font-weight:600;margin-top:3px}
.route{min-width:180px;color:#e2e8f0;font-weight:600}
.meta{display:block;font-family:monospace;font-size:.68rem;color:#64748b;font-weight:400;margin-top:3px;max-width:240px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.target{white-space:nowrap;color:#cbd5e1}
.door{display:block;color:#38bdf8;font-weight:600;margin-top:3px}
.body{font-family:monospace;font-size:.78rem;color:#64748b;max-width:320px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.time{color:#475569;white-space:nowrap}
.empty{color:#475569;padding:20px 0}
</style>
</head>
<body>
<h1>QR Debug</h1>
<p class="sub">Last %d scans · auto-refreshes every 2 s · <a href="/qrdebug?json=1" style="color:#38bdf8">JSON</a></p>
<table id="tbl">
<thead><tr><th>Time</th><th>QR value</th><th>Scanner / station</th><th>Target device / door</th><th>Status</th><th>Opened</th><th>Response</th></tr></thead>
<tbody id="body"></tbody>
</table>
<script>
const seed = %s;
function render(rows){
  const b=document.getElementById('body');
  if(!rows||!rows.length){b.innerHTML='<tr><td colspan="7" class="empty">No scans yet. Scan a QR code to see results here.</td></tr>';return}
  b.innerHTML=rows.map(r=>'<tr>'+
    '<td class="time">'+r.time+'</td>'+
    '<td class="qr" title="'+esc(r.qr)+'">'+esc(r.qr)+(r.member?'<span class="member">'+esc(r.member)+'</span>':'')+'</td>'+
    '<td class="route">'+esc(r.scanner||'Unassigned scanner')+
      '<span class="meta">Local port :'+esc(r.localPort||'—')+'</span>'+
      (r.hardwareId?'<span class="meta" title="'+esc(r.hardwareId)+'">USB: '+esc(shortID(r.hardwareId))+'</span>':'<span class="meta">USB device not bound</span>')+'</td>'+
    '<td class="target">'+esc(r.deviceId||'Default device')+
      '<span class="door">Door '+esc(r.doorNo||1)+(r.direction?' · '+esc(r.direction.toUpperCase()):'')+'</span></td>'+
    '<td>'+(r.status===200?'<span class="ok">200 OK</span>':'<span class="fail">'+r.status+'</span>')+'</td>'+
    '<td>'+(r.opened?'<span class="ok">✓ opened</span>':'<span class="fail">✗ no</span>')+'</td>'+
    '<td class="body" title="'+esc(r.body)+'">'+esc(r.body)+'</td>'+
  '</tr>').join('');
}
function shortID(s){s=String(s||'');return s.length>42?'…'+s.slice(-42):s}
function esc(s){return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/"/g,'&quot;')}
render(seed);
async function poll(){try{const r=await fetch('/qrdebug?json=1');render(await r.json())}catch(e){}}
setInterval(poll,2000);
</script>
</body>
</html>`, len(snap), string(rowsJSON))
}
