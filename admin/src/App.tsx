import { useEffect, useState, useRef, useMemo } from 'react'
import { api, apiUrl, auth, subscribeEvents, DeviceForm, PersonForm } from './api'

type Tab = 'devices' | 'live' | 'persons' | 'enrol' | 'events' | 'qr-auth' | 'agents' | 'console' | 'test' | 'settings' | 'guide' | 'api-docs' | 'plugin-faceapp' | 'plans' | 'access-log' | 'hq'

export default function App() {
  const [me, setMe] = useState<any>(null)
  const [authReady, setAuthReady] = useState(false)
  const [theme, setTheme] = useState<'light' | 'dark'>(
    (localStorage.getItem('face_auth.theme') as 'light' | 'dark') || 'dark'
  )

  // Apply the theme at the App level so CSS variables are set even on the
  // login screen (before the Dashboard mounts).
  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
    localStorage.setItem('face_auth.theme', theme)
  }, [theme])

  const loadMe = async () => {
    if (!auth.token) { setAuthReady(true); setMe(null); return }
    try { setMe(await api.me()) } catch { auth.clear(); setMe(null) }
    finally { setAuthReady(true) }
  }
  useEffect(() => { loadMe() }, [])
  useEffect(() => {
    const handler = () => { setMe(null); setAuthReady(true) }
    window.addEventListener('face_auth:unauthorized', handler)
    return () => window.removeEventListener('face_auth:unauthorized', handler)
  }, [])

  if (!authReady) return <div className="auth-splash">Loading…</div>
  if (!me?.user) return <LoginScreen onLoggedIn={loadMe} />

  return <Dashboard me={me} onMe={setMe} onLogout={() => { auth.clear(); setMe(null) }} theme={theme} setTheme={setTheme} />
}

function LoginScreen({ onLoggedIn }: { onLoggedIn: () => void }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [backend, setBackend] = useState<'checking' | 'ok' | 'down'>('checking')

  // Probe the backend on mount so the user sees instantly whether the API is
  // reachable. If this fails the login form is pointless until they fix the
  // backend.
  useEffect(() => {
    let cancelled = false
    api.healthz()
      .then(() => { if (!cancelled) setBackend('ok') })
      .catch(() => { if (!cancelled) setBackend('down') })
    return () => { cancelled = true }
  }, [])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true); setErr('')
    try {
      const r = await api.login(email, password)
      auth.token = r.token
      if (r.tenant?.id) auth.activeTenantId = r.tenant.id
      onLoggedIn()
    } catch (e: any) {
      // Strip the "Error: " prefix react adds + the leading status code if
      // present so the user reads a normal sentence.
      setErr(String(e?.message || e).replace(/^Error:\s*/, '').replace(/^\d+:\s*/, ''))
    } finally { setBusy(false) }
  }

  return (
    <div className="login-shell">
      <form className="login-card" onSubmit={submit}>
        <div className="login-brand">
          <span className="logo-dot" /> <strong>face_auth</strong>
        </div>
        <h1>Sign in</h1>
        <p className="login-sub">
          Use your HQ or tenant admin email and password.
        </p>

        <div className={`backend-status ${backend}`}>
          <span className="status-dot" />
          {backend === 'checking' && 'Checking backend…'}
          {backend === 'ok' && 'Backend reachable'}
          {backend === 'down' && (
            <>
              Cannot reach the backend at <code>{apiUrl('/healthz')}</code>. Check that the API container is running and the URL is right.
            </>
          )}
        </div>

        <label className="login-field">
          <span>Email</span>
          <input
            type="email"
            autoFocus
            autoComplete="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
          />
        </label>
        <label className="login-field">
          <span>Password</span>
          <input
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </label>
        {err && <div className="login-error">{err}</div>}
        <button className="btn-primary login-submit" type="submit" disabled={busy || backend === 'down'}>
          {busy ? 'Signing in…' : backend === 'down' ? 'Backend unreachable' : 'Sign in'}
        </button>
      </form>
    </div>
  )
}

function Dashboard({ me, onMe, onLogout, theme, setTheme }: { me: any; onMe: (m: any) => void; onLogout: () => void; theme: 'light' | 'dark'; setTheme: (t: 'light' | 'dark') => void }) {
  const isHQ = me.user?.role === 'hq_admin'
  const [tab, setTab] = useState<Tab>(isHQ ? 'hq' : 'devices')
  // When the user clicks "Enrol face" on a person, jump to the Enrol tab with
  // that person preselected. Consumed (and cleared) by EnrolTab on mount.
  const [enrolPersonId, setEnrolPersonId] = useState('')
  const goEnrol = (personId: string) => { setEnrolPersonId(personId); setTab('enrol') }

  // Hard guard: if the role changed mid-session (e.g. HQ admin logs out and a
  // tenant admin logs in on the same browser), force the active tab back to a
  // role-appropriate default. Without this, a leftover tab='hq' selection
  // would render HQTab for a tenant_admin and trigger the 403 banner.
  useEffect(() => {
    if (!isHQ && tab === 'hq') setTab('devices')
  }, [isHQ, tab])
  const [status, setStatus] = useState<any>(null)
  const [navOpen, setNavOpen] = useState(false)
  const [tenants, setTenants] = useState<any[]>([])
  const [activeTenantId, setActiveTenantId] = useState<string>(auth.activeTenantId)

  useEffect(() => {
    if (isHQ) api.hqListTenants().then((t) => setTenants(t || [])).catch(() => {})
  }, [isHQ])

  useEffect(() => {
    auth.activeTenantId = activeTenantId
    if (isHQ && activeTenantId) {
      // Refresh "me" so the topbar shows the active tenant label
      api.me().then(onMe).catch(() => {})
    }
  }, [activeTenantId, isHQ])

  useEffect(() => {
    const tick = () => api.status().then(setStatus).catch(() => setStatus(null))
    tick()
    const t = setInterval(tick, 5000)
    return () => clearInterval(t)
  }, [activeTenantId])

  const allTabs: { id: Tab; label: string; group?: string; hq?: boolean }[] = [
    { id: 'hq',      label: 'HQ overview', group: 'HQ', hq: true },
    { id: 'devices', label: 'Devices', group: 'Operate' },
    { id: 'live',    label: 'Live cameras', group: 'Operate' },
    { id: 'persons', label: 'Members', group: 'Operate' },
    { id: 'enrol',   label: 'Enrol', group: 'Operate' },
    { id: 'plans',   label: 'Plans & rules', group: 'Operate' },
    { id: 'access-log', label: 'Access log', group: 'Operate' },
    { id: 'events',  label: 'Raw events', group: 'Diagnostics' },
    { id: 'qr-auth', label: 'QR Auth', group: 'Diagnostics' },
    { id: 'agents',  label: 'Agents', group: 'Diagnostics' },
    { id: 'console', label: 'ISAPI console', group: 'Diagnostics' },
    { id: 'test',    label: 'Test face-auth', group: 'Diagnostics' },
    { id: 'settings',label: 'Settings', group: 'Admin' },
    { id: 'plugin-faceapp', label: 'FaceApp plugin', group: 'Admin' },
    { id: 'guide',   label: 'Setup guide', group: 'Help' },
    { id: 'api-docs',label: 'API docs', group: 'Help' },
  ]
  // HQ users see all tabs; tenant users hide HQ-only tabs and need a chosen
  // tenant before operating endpoints work.
  const tabs = allTabs.filter((t) => isHQ || !t.hq)
  const grouped = useMemo(() => {
    const m: Record<string, typeof tabs> = {}
    tabs.forEach((t) => { (m[t.group || 'Other'] = m[t.group || 'Other'] || []).push(t) })
    return m
  }, [tabs])

  // Lock body scroll when mobile sidebar drawer is open
  useEffect(() => {
    if (navOpen) document.body.style.overflow = 'hidden'
    else document.body.style.overflow = ''
    return () => { document.body.style.overflow = '' }
  }, [navOpen])

  return (
    <div className="app">
      <aside className={`sidebar ${navOpen ? 'open' : ''}`} aria-label="primary navigation">
        <div className="sidebar-head">
          <div className="brand">
            <span className="logo-dot" />
            <span className="brand-text">face_auth</span>
          </div>
          <button className="sidebar-close" onClick={() => setNavOpen(false)} aria-label="close menu">×</button>
        </div>
        <nav className="sidebar-nav">
          {Object.entries(grouped).map(([group, items]) => (
            <div key={group} className="sidebar-group">
              <div className="sidebar-group-label">{group}</div>
              {items.map((t) => (
                <button
                  key={t.id}
                  className={`sidebar-item ${tab === t.id ? 'active' : ''}`}
                  onClick={() => { setTab(t.id); setNavOpen(false) }}
                >{t.label}</button>
              ))}
            </div>
          ))}
        </nav>
        <div className="sidebar-footer">
          <div className="muted small" style={{ marginBottom: 6 }}>
            <strong>{me.user?.name || me.user?.email}</strong>
            <div className="muted small">{me.user?.role}{me.tenant?.name ? ` · ${me.tenant.name}` : ''}</div>
          </div>
          <button className="btn-ghost" style={{ width: '100%' }} onClick={async () => { try { await api.logout() } catch {}; onLogout() }}>Sign out</button>
        </div>
      </aside>

      <div className={`sidebar-backdrop ${navOpen ? 'visible' : ''}`} onClick={() => setNavOpen(false)} />

      <div className="main-area">
        <header className="topbar">
          <button className="nav-toggle" onClick={() => setNavOpen(true)} aria-label="open menu">
            <span /><span /><span />
          </button>
          <div className="brand topbar-brand">
            <span className="logo-dot" />
            <span className="brand-text">{tabs.find((t) => t.id === tab)?.label}</span>
          </div>
          <div className="topbar-right">
            {isHQ && (
              <select
                className="tenant-switcher"
                value={activeTenantId}
                onChange={(e) => setActiveTenantId(e.target.value)}
                title="Active tenant for tenant-scoped pages"
              >
                <option value="">— pick a tenant —</option>
                {tenants.map((row: any) => (
                  <option key={row.tenant.id} value={row.tenant.id}>{row.tenant.name}</option>
                ))}
              </select>
            )}
            {status ? (
              <span className={status.devicesOnline > 0 ? 'badge ok' : 'badge'}>
                <span className="status-dot" />
                {status.devicesOnline}/{status.devices}
              </span>
            ) : (
              <span className="badge err"><span className="status-dot" />offline</span>
            )}
            <button
              className="theme-toggle"
              onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
              title={`Switch to ${theme === 'dark' ? 'light' : 'dark'} mode`}
            >{theme === 'dark' ? 'Light' : 'Dark'}</button>
          </div>
        </header>
        <main>
          {tab === 'devices' && <DevicesTab />}
          {tab === 'live' && <LiveTab />}
          {tab === 'persons' && <PersonsTab onEnrol={goEnrol} />}
          {tab === 'enrol' && <EnrolTab initialPersonId={enrolPersonId} onConsumeInitial={() => setEnrolPersonId('')} />}
          {tab === 'events' && <EventsTab />}
          {tab === 'qr-auth' && <QRAuthTab />}
          {tab === 'agents' && <AgentsTab />}
          {tab === 'console' && <ConsoleTab />}
          {tab === 'test' && <TestTab />}
          {tab === 'settings' && <SettingsTab />}
          {tab === 'guide' && <GuideTab />}
          {tab === 'api-docs' && <ApiDocsTab />}
          {tab === 'plugin-faceapp' && <FaceAppTab />}
          {tab === 'hq' && <HQTab />}
          {tab === 'plans' && <PlansTab />}
          {tab === 'access-log' && <AccessLogTab />}
        </main>
      </div>
    </div>
  )
}

// ===================== Devices =====================

function DevicesTab() {
  const [devices, setDevices] = useState<any[]>([])
  const [editing, setEditing] = useState<any | null>(null)
  const [creating, setCreating] = useState(false)
  const [wifiDevice, setWifiDevice] = useState<any | null>(null)
  const [qrDevice, setQrDevice] = useState<any | null>(null)
  const [healthDevice, setHealthDevice] = useState<any | null>(null)
  const [scheduleDevice, setScheduleDevice] = useState<any | null>(null)
  const [liveDevice, setLiveDevice] = useState<any | null>(null)
  const [search, setSearch] = useState('')
  const [err, setErr] = useState('')

  const load = () => api.listDevices().then((d) => setDevices(d || [])).catch((e) => setErr(String(e)))
  useEffect(() => { load(); const t = setInterval(load, 5000); return () => clearInterval(t) }, [])

  const filtered = useMemo(() => {
    if (!search) return devices
    const s = search.toLowerCase()
    return devices.filter((d) =>
      (d.deviceID || '').toLowerCase().includes(s) ||
      (d.name || '').toLowerCase().includes(s) ||
      (d.ip || '').toLowerCase().includes(s) ||
      (d.model || '').toLowerCase().includes(s)
    )
  }, [devices, search])

  const del = async (id: string) => {
    if (!confirm(`Delete device ${id}?`)) return
    await api.deleteDevice(id); load()
  }
  const probe = async (id: string) => {
    try { await api.probeDevice(id) } catch {}
    load()
  }
  const openDoor = async (id: string) => {
    try {
      const r = await api.openDoor(id)
      alert(r.ok ? 'Door opened.' : `Failed: ${r.error}`)
    } catch (e: any) { alert(String(e)) }
  }
  const setupAlarm = async (id: string) => {
    if (!confirm(`Configure ${id} to push events to this server?`)) return
    try {
      const r = await api.setupAlarmHost(id)
      alert(r.ok ? 'Events will now stream live.' : `Failed: ${r.error}`)
    } catch (e: any) { alert(String(e)) }
  }

  return (
    <>
      <div className="page-toolbar">
        <div className="toolbar-left">
          <h1 className="page-title">Devices <span className="muted">· {devices.length}</span></h1>
          <input className="search" placeholder="Search ID, name, IP, model…" value={search} onChange={(e) => setSearch(e.target.value)} />
        </div>
        <div className="toolbar-right">
          <button className="btn-primary" onClick={() => setCreating(true)}>Add device</button>
        </div>
      </div>

      {err && <div className="err">{err}</div>}

      <Card>
        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr>
                <th>Preview</th>
                <th>Device</th>
                <th>Address</th>
                <th>Model</th>
                <th>Status</th>
                <th className="ta-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((d) => (
                <tr key={d.deviceID}>
                  <td data-label="Preview" className="cell-preview">
                    <div className="snap-box small">
                      {d.online
                        ? <SnapshotImg deviceId={d.deviceID} pollMs={6000} />
                        : <div className="snap-placeholder">offline</div>}
                    </div>
                  </td>
                  <td data-label="Device">
                    <div className="cell-stack">
                      <strong>{d.name || d.deviceID}</strong>
                      <span className="muted mono small">{d.deviceID}</span>
                    </div>
                  </td>
                  <td data-label="Address">
                    {d.reach === 'otap'
                      ? <span className="badge mono small">OTAP push</span>
                      : d.ip
                        ? <span className="mono small">{d.useHttps ? 'https' : 'http'}://{d.ip}:{d.port}</span>
                        : <span className="muted">—</span>}
                  </td>
                  <td data-label="Model">
                    <div className="cell-stack">
                      <span>{d.model || '—'}</span>
                      <span className="muted small">{d.firmware || ''}</span>
                    </div>
                  </td>
                  <td data-label="Status">
                    <span className={`badge ${d.online ? 'ok' : ''}`}>
                      <span className="status-dot" />
                      {d.online ? 'online' : 'offline'}
                    </span>
                  </td>
                  <td data-label="Actions" className="ta-right">
                    <div className="cell-actions">
                      <button className="btn-ghost" onClick={() => openDoor(d.deviceID)}>Open door</button>
                      <button className="btn-ghost" onClick={() => setLiveDevice(d)}>Live</button>
                      <button className="btn-ghost" onClick={() => probe(d.deviceID)}>Probe</button>
                      <button className="btn-ghost" onClick={() => setupAlarm(d.deviceID)}>Events</button>
                      <button className="btn-ghost" onClick={() => setWifiDevice(d)}>Wi-Fi</button>
                      <button className="btn-ghost" onClick={() => setQrDevice(d)}>Camera QR</button>
                      <button className="btn-ghost" onClick={() => setHealthDevice(d)}>Health</button>
                      <button className="btn-ghost" onClick={() => setScheduleDevice(d)}>Schedule</button>
                      <button className="btn-ghost" onClick={() => setEditing(d)}>Edit</button>
                      <button className="btn-danger" onClick={() => del(d.deviceID)}>Delete</button>
                    </div>
                  </td>
                </tr>
              ))}
              {filtered.length === 0 && (
                <tr><td colSpan={6}><div className="empty">{search ? 'No devices match the search.' : 'No devices yet. Click "Add device".'}</div></td></tr>
              )}
            </tbody>
          </table>
        </div>
      </Card>

      {creating && <DeviceModal onClose={() => setCreating(false)} onSaved={() => { setCreating(false); load() }} />}
      {editing && <DeviceModal device={editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); load() }} />}
      {wifiDevice && <WifiModal device={wifiDevice} onClose={() => setWifiDevice(null)} />}
      {qrDevice && <QRCameraModal device={qrDevice} onClose={() => setQrDevice(null)} />}
      {healthDevice && <HealthModal device={healthDevice} onClose={() => setHealthDevice(null)} />}
      {scheduleDevice && <ScheduleModal device={scheduleDevice} onClose={() => setScheduleDevice(null)} />}
      {liveDevice && <LiveViewModal device={liveDevice} onClose={() => setLiveDevice(null)} />}
    </>
  )
}

// Live view (MJPEG re-multiplexed snapshots) + snapshot grab + intercom control.
function LiveViewModal({ device, onClose }: { device: any; onClose: () => void }) {
  const id = device.deviceID
  const [supported, setSupported] = useState<boolean | null>(null) // null = checking
  const [status, setStatus] = useState('') // device call status (idle/ring/onCall…)
  const [busy, setBusy] = useState('')
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')

  const refreshStatus = () => api.intercomStatus(id)
    .then((r) => { try { setStatus(JSON.parse(r.raw).CallStatus?.status || '') } catch { setStatus('') } })
    .catch(() => {})

  // Probe intercom support (VideoIntercom call signaling) when the modal opens.
  useEffect(() => {
    api.intercomCapabilities(id)
      .then((r) => { setSupported(!!r.supported); if (r.supported) refreshStatus() })
      .catch(() => setSupported(false))
  }, [id])

  const signal = async (cmd: string, label: string) => {
    setBusy(cmd); setErr(''); setMsg('')
    try {
      await api.intercomSignal(id, cmd)
      setMsg(`${label} sent.`)
      await refreshStatus()
    } catch (e: any) { setErr(String(e)) } finally { setBusy('') }
  }
  // Best-effort hang up if the modal closes mid-call.
  useEffect(() => () => { if (status && status !== 'idle') api.intercomSignal(id, 'hangUp').catch(() => {}) }, [status, id])

  return (
    <Modal title={`Live — ${device.name || id}`} onClose={onClose}>
      <div className="live-frame" style={{ marginBottom: 12 }}>
        <img className="snap-img" alt="live" src={api.mjpegUrl(id, 6)} />
      </div>
      <div className="form-row" style={{ gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
        <a className="btn-ghost" href={`${api.snapshotUrl(id)}${api.snapshotUrl(id).includes('?') ? '&' : '?'}t=${Date.now()}`} target="_blank" rel="noreferrer">Open snapshot</a>
        {supported === false && <span className="badge">Intercom not supported on this device</span>}
        {supported === null && <span className="badge">Checking intercom…</span>}
        {supported === true && <>
          <button type="button" className="btn-primary" disabled={!!busy} onClick={() => signal('request', 'Call')}>{busy === 'request' ? 'Calling…' : 'Call'}</button>
          <button type="button" className="btn-ghost" disabled={!!busy} onClick={() => signal('answer', 'Answer')}>{busy === 'answer' ? 'Answering…' : 'Answer'}</button>
          <button type="button" className="btn-danger" disabled={!!busy} onClick={() => signal('hangUp', 'Hang up')}>{busy === 'hangUp' ? 'Hanging up…' : 'Hang up'}</button>
          {status && <span className="muted small">status: <b>{status}</b></span>}
        </>}
      </div>
      <p className="muted small" style={{ marginTop: 8 }}>
        Live view streams re-multiplexed snapshots (MJPEG). Intercom uses the device's VideoIntercom call
        signaling — Call rings it, Answer picks up an incoming call, Hang up ends it. Two-way audio media
        between the browser and the device is a follow-up.
      </p>
      {msg && <div className="muted small">{msg}</div>}
      {err && <div className="err">{err}</div>}
    </Modal>
  )
}

// Device health — polls AcsWorkStatus.
function HealthModal({ device, onClose }: { device: any; onClose: () => void }) {
  const id = device.deviceID
  const [raw, setRaw] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const load = async () => {
    setBusy(true); setErr('')
    try { const r = await api.workStatus(id); setRaw(r.raw || '') }
    catch (e: any) { setErr(String(e)) } finally { setBusy(false) }
  }
  useEffect(() => { load() }, [])

  // Pull a few well-known fields out of the raw status for a friendly summary.
  let parsed: any = null
  try { parsed = raw ? JSON.parse(raw).AcsWorkStatus || JSON.parse(raw) : null } catch {}

  return (
    <Modal title={`Health — ${device.name || id}`} onClose={onClose}>
      <div className="form-row" style={{ marginBottom: 10 }}>
        <button type="button" className="btn-ghost" disabled={busy} onClick={load}>{busy ? 'Refreshing…' : 'Refresh'}</button>
      </div>
      {parsed && (
        <div>
          {[
            ['Door', parsed.doorLockStatus ?? parsed.doorStatus],
            ['Magnetic', parsed.doorStatus ?? parsed.magneticStatus],
            ['Tamper', parsed.antiSneakStatus ?? parsed.caseStatus ?? parsed.tamperStatus],
            ['Battery', parsed.batteryVoltage ?? parsed.battery],
            ['Capacity used', parsed.usedFaceNum ?? parsed.faceNum],
          ].filter(([, v]) => v !== undefined).map(([label, v]) => (
            <div className="detail-row" key={String(label)}>
              <span className="detail-label">{label}</span>
              <span className="detail-value">{String(v)}</span>
            </div>
          ))}
        </div>
      )}
      {err && <div className="err">{err}</div>}
      {raw && <details style={{ marginTop: 10 }} open={!parsed}><summary className="muted small">Raw work status</summary><pre className="result">{raw}</pre></details>}
    </Modal>
  )
}

const WEEKDAYS = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday']

// Access schedule editor — one allow window per weekday → week plan + template.
function ScheduleModal({ device, onClose }: { device: any; onClose: () => void }) {
  const id = device.deviceID
  const [planNo, setPlanNo] = useState(1)
  const [tplNo, setTplNo] = useState(1)
  const [days, setDays] = useState(() => WEEKDAYS.map((w) => ({ week: w, enable: true, begin: '09:00:00', end: '18:00:00' })))
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<any>(null)
  const [err, setErr] = useState('')

  const setDay = (i: number, patch: Partial<{ enable: boolean; begin: string; end: string }>) =>
    setDays((prev) => prev.map((d, j) => (j === i ? { ...d, ...patch } : d)))

  const save = async () => {
    setBusy(true); setErr(''); setResult(null)
    try {
      const wp = await api.setWeekPlan(id, planNo, days)
      const tpl = await api.setPlanTemplate(id, tplNo, { weekPlanNo: planNo, name: `tpl${tplNo}` })
      setResult({ weekPlan: wp, planTemplate: tpl })
    } catch (e: any) { setErr(String(e)) } finally { setBusy(false) }
  }

  return (
    <Modal title={`Access schedule — ${device.name || id}`} onClose={onClose}>
      <p className="card-sub">
        Set the allow window per weekday. This writes week plan #{planNo} and plan template #{tplNo} to the device.
        Assign template #{tplNo} to a person (their plan template) to enforce it — outside the window the device denies entry.
      </p>
      <div className="form-row" style={{ gap: 8 }}>
        <Field label="Week plan #"><input type="number" min={1} value={planNo} onChange={(e) => setPlanNo(parseInt(e.target.value || '1', 10))} /></Field>
        <Field label="Template #"><input type="number" min={1} value={tplNo} onChange={(e) => setTplNo(parseInt(e.target.value || '1', 10))} /></Field>
      </div>
      <table className="table" style={{ marginTop: 8 }}>
        <thead><tr><th>Day</th><th>Allow</th><th>From</th><th>To</th></tr></thead>
        <tbody>
          {days.map((d, i) => (
            <tr key={d.week}>
              <td>{d.week.slice(0, 3)}</td>
              <td><input type="checkbox" checked={d.enable} onChange={(e) => setDay(i, { enable: e.target.checked })} /></td>
              <td><input type="time" step={1} value={d.begin.slice(0, 5)} disabled={!d.enable} onChange={(e) => setDay(i, { begin: `${e.target.value}:00` })} /></td>
              <td><input type="time" step={1} value={d.end.slice(0, 5)} disabled={!d.enable} onChange={(e) => setDay(i, { end: `${e.target.value}:00` })} /></td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="form-actions" style={{ marginTop: 12 }}>
        <button type="button" className="btn-ghost" onClick={onClose}>Close</button>
        <button type="button" className="btn-primary" disabled={busy} onClick={save}>{busy ? 'Writing…' : 'Write schedule to device'}</button>
      </div>
      {err && <div className="err">{err}</div>}
      {result && <pre className="result">{JSON.stringify(result, null, 2)}</pre>}
    </Modal>
  )
}

// Probe + toggle device-native QR-code scanning (user shows QR to the device camera).
function QRCameraModal({ device, onClose }: { device: any; onClose: () => void }) {
  const id = device.deviceID
  const [busy, setBusy] = useState<'' | 'probe' | 'on' | 'off'>('')
  const [supported, setSupported] = useState<boolean | null>(null)
  const [raw, setRaw] = useState('')
  const [result, setResult] = useState<any>(null)
  const [err, setErr] = useState('')

  const probe = async () => {
    setBusy('probe'); setErr(''); setResult(null)
    try {
      const r = await api.qrCapability(id)
      setSupported(!!r.supported); setRaw(r.raw || '')
    } catch (e: any) { setErr(String(e)) } finally { setBusy('') }
  }

  useEffect(() => { probe() }, [])

  const toggle = async (enable: boolean) => {
    setBusy(enable ? 'on' : 'off'); setErr(''); setResult(null)
    try { setResult(await api.setQrScan(id, enable)) }
    catch (e: any) { setErr(String(e)) } finally { setBusy('') }
  }

  return (
    <Modal title={`Camera QR — ${device.name || id}`} onClose={onClose}>
      <p className="card-sub">
        Let the device's own camera read a user's QR code and authenticate — no separate USB scanner.
        Whether this works depends on the device firmware.
      </p>
      <div className="detail-row">
        <span className="detail-label">Firmware support</span>
        <span className="detail-value">
          {busy === 'probe' ? 'Probing…'
            : supported === null ? '—'
            : supported ? <span className="badge ok">supported</span>
            : <span className="badge">not supported</span>}
        </span>
      </div>
      {supported === false && (
        <div className="err">This device's firmware reports no camera QR support. Users must use the USB scanner or another method.</div>
      )}
      <div className="form-row" style={{ gap: 8, marginTop: 12 }}>
        <button type="button" className="btn-ghost" disabled={!!busy} onClick={probe}>Re-probe</button>
        <button type="button" className="btn-primary" disabled={!!busy || supported === false} onClick={() => toggle(true)}>
          {busy === 'on' ? 'Enabling…' : 'Enable camera QR'}
        </button>
        <button type="button" className="btn-ghost" disabled={!!busy} onClick={() => toggle(false)}>
          {busy === 'off' ? 'Disabling…' : 'Disable'}
        </button>
      </div>
      <p className="muted small" style={{ marginTop: 10 }}>
        After enabling, the user's QR must encode their employee number so the device can match it to the enrolled user.
      </p>
      {err && <div className="err">{err}</div>}
      {result && <pre className="result">{JSON.stringify(result, null, 2)}</pre>}
      {raw && <details style={{ marginTop: 10 }}><summary className="muted small">Raw capabilities</summary><pre className="result">{raw}</pre></details>}
    </Modal>
  )
}

function WifiModal({ device, onClose }: { device: any; onClose: () => void }) {
  const id = device.deviceID
  const [ssid, setSsid] = useState('')
  const [password, setPassword] = useState('')
  const [securityMode, setSecurityMode] = useState('WPA2-personal')
  const [ifId, setIfId] = useState('')
  const [aps, setAps] = useState<any[]>([])
  const [current, setCurrent] = useState('')
  const [busy, setBusy] = useState<'' | 'load' | 'scan' | 'save'>('')
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')

  const loadCurrent = async () => {
    setBusy('load'); setErr(''); setMsg('')
    try {
      const r = await api.getWifi(id, ifId)
      setCurrent(r.raw || '')
      if (!r.ok) setErr(r.error || 'Could not read Wi-Fi config')
    } catch (e: any) { setErr(String(e)) } finally { setBusy('') }
  }
  const scan = async () => {
    setBusy('scan'); setErr(''); setMsg('')
    try {
      const r = await api.scanWifi(id, ifId)
      if (r.ok) setAps(r.accessPoints || [])
      else setErr(r.error || 'Scan failed (firmware may not support scanning)')
    } catch (e: any) { setErr(String(e)) } finally { setBusy('') }
  }
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!ssid) { setErr('SSID required'); return }
    if (!confirm(`Point ${id} at Wi-Fi network "${ssid}"? The device may drop off its current connection while it reconnects.`)) return
    setBusy('save'); setErr(''); setMsg('')
    try {
      const r = await api.setWifi(id, { ssid, password, securityMode, ifId: ifId || undefined })
      if (r.ok) setMsg('Saved. The device is switching networks — give it up to a minute, then Probe.')
      else setErr(r.error || 'Failed to set Wi-Fi')
    } catch (e: any) { setErr(String(e)) } finally { setBusy('') }
  }

  return (
    <Modal title={`Wi-Fi · ${device.name || id}`} onClose={onClose}>
      <form onSubmit={save} className="form">
        <Field label="Network (SSID)" hint="pick from a scan or type it in">
          <input value={ssid} onChange={(e) => setSsid(e.target.value)} placeholder="OfficeWiFi" required list="wifi-scan-list" />
          {aps.length > 0 && (
            <datalist id="wifi-scan-list">
              {aps.map((ap, i) => <option key={i} value={ap.ssid}>{ap.securityMode || ''} {ap.signalStrength ? `· ${ap.signalStrength}%` : ''}</option>)}
            </datalist>
          )}
        </Field>
        <Field label="Password">
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="(leave blank for open networks)" />
        </Field>
        <div className="form-row">
          <Field label="Security" grow={2}>
            <select value={securityMode} onChange={(e) => setSecurityMode(e.target.value)}>
              <option value="WPA2-personal">WPA2-Personal</option>
              <option value="WPA-personal">WPA-Personal</option>
              <option value="WEP">WEP</option>
              <option value="disable">Open (none)</option>
            </select>
          </Field>
          <Field label="Interface" grow={1} hint="blank = auto">
            <input value={ifId} onChange={(e) => setIfId(e.target.value)} placeholder="auto" />
          </Field>
        </div>
        <div className="form-actions">
          <button type="button" className="btn-ghost" onClick={scan} disabled={busy !== ''}>{busy === 'scan' ? 'Scanning…' : 'Scan'}</button>
          <button type="button" className="btn-ghost" onClick={loadCurrent} disabled={busy !== ''}>{busy === 'load' ? 'Reading…' : 'Read current'}</button>
          <button type="submit" className="btn-primary" disabled={busy !== ''}>{busy === 'save' ? 'Saving…' : 'Connect'}</button>
        </div>
      </form>
      {msg && <div className="ok-banner">{msg}</div>}
      {err && <div className="err">{err}</div>}
      {current && <pre className="result">{current}</pre>}
    </Modal>
  )
}

function DeviceModal({ device, onClose, onSaved }: { device?: any; onClose: () => void; onSaved: () => void }) {
  const editing = !!device
  const [form, setForm] = useState<DeviceForm>(device ? {
    deviceId: device.deviceID, name: device.name || '', ip: device.ip || '',
    port: device.port || 80, useHttps: !!device.useHttps,
    isapiUsername: device.isapiUsername || 'admin', isapiPassword: '',
    fdid: device.fdid || '1', faceLibType: device.faceLibType || 'blackFD',
    agentId: device.agentId || '',
    reach: device.reach || (device.agentId ? 'agent' : 'direct'),
  } : {
    deviceId: '', name: '', ip: '', port: 80, useHttps: false,
    isapiUsername: 'admin', isapiPassword: '',
    fdid: '1', faceLibType: 'blackFD', agentId: '', reach: 'direct',
  })
  const [agents, setAgents] = useState<any[]>([])
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [probe, setProbe] = useState<any>(null)

  useEffect(() => { api.listAgents().then((a) => setAgents(a || [])).catch(() => {}) }, [])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true); setErr(''); setProbe(null)
    try {
      const r = await api.registerDevice(form)
      setProbe(r.probe)
      if (r.probe?.reachable || editing || form.reach === 'otap') {
        onSaved()
      }
    } catch (e: any) { setErr(String(e)) } finally { setBusy(false) }
  }

  return (
    <Modal title={editing ? `Edit ${device.deviceID}` : 'Add device'} onClose={onClose}>
      <form onSubmit={submit} className="form">
        <Field label="Device ID" hint="serial number works">
          <input value={form.deviceId} onChange={(e) => setForm({ ...form, deviceId: e.target.value })} placeholder="GA2848858" required disabled={editing} />
        </Field>
        <Field label="Display name">
          <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Front Door" />
        </Field>
        <Field label="Reach via" hint="how the platform sends commands to this device">
          <select value={form.reach || 'direct'} onChange={(e) => setForm({ ...form, reach: e.target.value, agentId: e.target.value === 'agent' ? form.agentId : '' })}>
            <option value="direct">Direct ISAPI (same LAN)</option>
            <option value="agent">Via agent (cloud → LAN bridge)</option>
            <option value="otap">OTAP push (device dials out — no agent)</option>
          </select>
        </Field>
        {form.reach === 'otap' && (
          <p className="muted small">
            The device dials out to the platform's push listener; no IP or agent is needed.
            Make sure the device's PUSHCfg points at this server and it has authenticated (AuthInfo/Login).
            Commands are delivered on the device's next poll.
          </p>
        )}
        <div className="form-row">
          <Field label="Device IP" grow={3}>
            <input value={form.ip} onChange={(e) => setForm({ ...form, ip: e.target.value })} placeholder="192.168.100.64" required={form.reach !== 'otap'} disabled={form.reach === 'otap'} />
          </Field>
          <Field label="Port" grow={1}>
            <input type="number" value={form.port} onChange={(e) => setForm({ ...form, port: parseInt(e.target.value || '80', 10) })} />
          </Field>
        </div>
        <label className="check">
          <input type="checkbox" checked={form.useHttps} onChange={(e) => setForm({ ...form, useHttps: e.target.checked })} />
          <span>Use HTTPS</span>
        </label>
        <div className="form-row">
          <Field label="Username">
            <input value={form.isapiUsername} onChange={(e) => setForm({ ...form, isapiUsername: e.target.value })} placeholder="admin" />
          </Field>
          <Field label={editing ? 'Password (blank = keep)' : 'Password'}>
            <input type="password" value={form.isapiPassword} onChange={(e) => setForm({ ...form, isapiPassword: e.target.value })} />
          </Field>
        </div>
        <div className="form-row">
          <Field label="FDID"><input value={form.fdid} onChange={(e) => setForm({ ...form, fdid: e.target.value })} /></Field>
          <Field label="Face lib type"><input value={form.faceLibType} onChange={(e) => setForm({ ...form, faceLibType: e.target.value })} /></Field>
        </div>
        {form.reach === 'agent' && (
          <Field label="Agent" hint="which on-prem agent bridges to this device">
            <select value={form.agentId || ''} onChange={(e) => setForm({ ...form, agentId: e.target.value })}>
              <option value="">— select an agent —</option>
              {agents.map((a) => (
                <option key={a.id} value={a.id}>{a.name || a.id} {a.online ? '· online' : '· offline'}</option>
              ))}
            </select>
          </Field>
        )}
        <div className="form-actions">
          <button type="button" className="btn-ghost" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn-primary" disabled={busy}>{busy ? 'Saving…' : (editing ? 'Save changes' : 'Save & probe')}</button>
        </div>
      </form>
      {err && <div className="err">{err}</div>}
      {probe && <pre className="result">{JSON.stringify(probe, null, 2)}</pre>}
    </Modal>
  )
}

// Snapshot polling component
function SnapshotImg({ deviceId, pollMs = 1500 }: { deviceId: string; pollMs?: number }) {
  const [src, setSrc] = useState(() => `${api.snapshotUrl(deviceId)}?t=${Date.now()}`)
  const [err, setErr] = useState(false)
  const tRef = useRef<number | null>(null)
  useEffect(() => {
    tRef.current = window.setInterval(() => setSrc(`${api.snapshotUrl(deviceId)}?t=${Date.now()}`), pollMs)
    return () => { if (tRef.current) window.clearInterval(tRef.current) }
  }, [deviceId, pollMs])
  if (err) return <div className="snap-placeholder">no preview</div>
  return <img className="snap-img" src={src} alt="" onError={() => setErr(true)} onLoad={() => setErr(false)} />
}

// ===================== Live =====================

function LiveTab() {
  const [devices, setDevices] = useState<any[]>([])
  const [focused, setFocused] = useState<string>('')
  const [mode, setMode] = useState<'snapshot' | 'stream'>('snapshot')
  useEffect(() => {
    const load = () => api.listDevices().then((d) => setDevices(d || [])).catch(() => {})
    load(); const t = setInterval(load, 10_000); return () => clearInterval(t)
  }, [])
  const online = devices.filter((d) => d.online)
  const focusedDev = online.find((d) => d.deviceID === focused) || online[0]
  return (
    <>
      <div className="page-toolbar">
        <div className="toolbar-left">
          <h1 className="page-title">Live preview <span className="muted">· {online.length} online</span></h1>
        </div>
        <div className="toolbar-right" style={{ gap: 8 }}>
          <label className="muted small">Mode</label>
          <select value={mode} onChange={(e) => setMode(e.target.value as any)}>
            <option value="snapshot">Snapshot grid (light)</option>
            <option value="stream">MJPEG stream (focused)</option>
          </select>
        </div>
      </div>
      {online.length === 0 ? (
        <div className="empty">No online devices to preview. Add and probe a device first.</div>
      ) : mode === 'stream' && focusedDev ? (
        <>
          <Card title={focusedDev.name || focusedDev.deviceID}>
            <div className="live-frame" style={{ minHeight: 360 }}>
              <img className="snap-img" alt="" src={api.mjpegUrl(focusedDev.deviceID, 6)} />
            </div>
            <div className="muted small" style={{ marginTop: 8 }}>
              Continuous JPEG multipart stream — works in any &lt;img&gt; tag, no plugin.
            </div>
          </Card>
          <Card title="Switch camera">
            <div className="live-grid">
              {online.map((d) => (
                <div key={d.deviceID} className={`live-tile ${focused === d.deviceID ? 'active' : ''}`} onClick={() => setFocused(d.deviceID)} style={{ cursor: 'pointer' }}>
                  <div className="live-frame"><SnapshotImg deviceId={d.deviceID} pollMs={2500} /></div>
                  <div className="live-meta">
                    <strong>{d.name || d.deviceID}</strong>
                    <span className="muted mono small">{d.ip}</span>
                  </div>
                </div>
              ))}
            </div>
          </Card>
        </>
      ) : (
        <div className="live-grid">
          {online.map((d) => (
            <div key={d.deviceID} className="live-tile" onClick={() => { setFocused(d.deviceID); setMode('stream') }} style={{ cursor: 'pointer' }}>
              <div className="live-frame"><SnapshotImg deviceId={d.deviceID} pollMs={1200} /></div>
              <div className="live-meta">
                <strong>{d.name || d.deviceID}</strong>
                <span className="muted mono small">{d.ip}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </>
  )
}

// ===================== Persons =====================

function PersonsTab({ onEnrol }: { onEnrol: (personId: string) => void }) {
  const [persons, setPersons] = useState<any[]>([])
  const [devices, setDevices] = useState<any[]>([])
  const [search, setSearch] = useState('')
  const [deviceFilter, setDeviceFilter] = useState('')
  const [creating, setCreating] = useState(false)
  const [viewing, setViewing] = useState<any | null>(null)
  const [syncing, setSyncing] = useState(false)
  const [syncResult, setSyncResult] = useState<any>(null)
  const [err, setErr] = useState('')

  const load = () => api.listPersons().then((p) => setPersons(p || [])).catch(() => setPersons([]))
  useEffect(() => {
    load()
    api.listDevices().then((d) => {
      const list = d || []
      setDevices(list)
      // Convenience: if there's exactly one device, preselect it so "Sync
      // from device" is immediately usable (the button is disabled otherwise).
      if (list.length === 1) setDeviceFilter(list[0].deviceID)
    }).catch(() => setDevices([]))
  }, [])

  const sync = async () => {
    if (!deviceFilter) { setErr('Pick a device to sync from'); return }
    setSyncing(true); setErr(''); setSyncResult(null)
    try {
      const r = await api.syncPersons(deviceFilter)
      setSyncResult(r)
      load()
    } catch (e: any) { setErr(String(e)) } finally { setSyncing(false) }
  }

  const del = async (p: any) => {
    if (!confirm(`Delete ${p.name}?${deviceFilter ? '\nThis will also delete the user from the device.' : ''}`)) return
    try {
      await api.deletePerson(p.id, deviceFilter || undefined)
      load()
    } catch (e: any) { alert(String(e)) }
  }

  const filtered = useMemo(() => {
    let list = persons
    if (deviceFilter) {
      // We only have per-person metadata.deviceSyncedFrom; treat anything synced from this device as belonging to it
      list = list.filter((p) => {
        try {
          return p.metadata?.deviceSyncedFrom === deviceFilter
        } catch { return false }
      })
      // If no filter matches and persons exist locally without sync metadata, fall back to showing all
      if (list.length === 0) list = persons
    }
    if (search) {
      const s = search.toLowerCase()
      list = list.filter((p) =>
        (p.name || '').toLowerCase().includes(s) ||
        (p.employeeNo || '').toLowerCase().includes(s)
      )
    }
    return list
  }, [persons, search, deviceFilter])

  return (
    <>
      <div className="page-toolbar">
        <div className="toolbar-left">
          <h1 className="page-title">Persons <span className="muted">· {persons.length}</span></h1>
          <input className="search" placeholder="Search name or employee #…" value={search} onChange={(e) => setSearch(e.target.value)} />
          <select className="search" value={deviceFilter} onChange={(e) => setDeviceFilter(e.target.value)}>
            <option value="">All devices</option>
            {devices.map((d) => <option key={d.deviceID} value={d.deviceID}>{d.name || d.deviceID}</option>)}
          </select>
        </div>
        <div className="toolbar-right">
          <button className="btn-ghost" onClick={sync} disabled={syncing || !deviceFilter} title={!deviceFilter ? 'Pick a device in the dropdown first' : 'Import users & faces from the selected device'}>{syncing ? 'Syncing…' : 'Sync from device'}</button>
          <button className="btn-primary" onClick={() => setCreating(true)}>Add person</button>
        </div>
      </div>

      {err && <div className="err">{err}</div>}
      {syncResult && (
        <div className="info">Synced {syncResult.synced} of {syncResult.users} users · {syncResult.faces} faces · {syncResult.cards} cards from the device.</div>
      )}

      <Card>
        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr>
                <th>Person</th>
                <th>Employee #</th>
                <th>Type</th>
                <th>Role</th>
                <th>Validity</th>
                <th className="ta-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((p) => (
                <tr key={p.id}>
                  <td data-label="Person">
                    <div className="cell-stack">
                      <strong>{p.name}</strong>
                      <span className="muted small">{p.gender || 'unknown'}</span>
                    </div>
                  </td>
                  <td data-label="Employee #" className="mono">{p.employeeNo}</td>
                  <td data-label="Type"><span className="chip">{p.personType || 'normal'}</span></td>
                  <td data-label="Role"><span className="chip">{p.personRole || 'basic'}</span></td>
                  <td data-label="Validity" className="small muted">
                    {p.longTerm
                      ? 'long-term'
                      : (p.validBegin && p.validEnd
                          ? `${new Date(p.validBegin).toLocaleDateString()} → ${new Date(p.validEnd).toLocaleDateString()}`
                          : '—')}
                    {p.attendanceOnly && <div>attendance only</div>}
                  </td>
                  <td data-label="Actions" className="ta-right">
                    <div className="cell-actions">
                      <button className="btn-ghost" onClick={() => setViewing(p)}>View</button>
                      <button className="btn-ghost" onClick={() => onEnrol(p.id)}>Enrol face</button>
                      <button className="btn-danger" onClick={() => del(p)}>Delete</button>
                    </div>
                  </td>
                </tr>
              ))}
              {filtered.length === 0 && (
                <tr><td colSpan={6}>
                  <div className="empty">
                    {persons.length === 0
                      ? 'No persons yet. Pick a device and click "Sync from device" to import existing users, or click "Add person".'
                      : 'No persons match your filter.'}
                  </div>
                </td></tr>
              )}
            </tbody>
          </table>
        </div>
      </Card>

      {creating && <PersonModal onClose={() => setCreating(false)} onSaved={() => { setCreating(false); load() }} />}
      {viewing && <PersonDetailModal personId={viewing.id} onClose={() => setViewing(null)} onDeleted={() => { setViewing(null); load() }} onEnrol={onEnrol} />}
    </>
  )
}

function PersonModal({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
  const [form, setForm] = useState<PersonForm>({
    name: '', employeeNo: '', gender: 'male',
    personType: 'normal', personRole: 'basic',
    longTerm: true, attendanceOnly: false,
    doorRight: '1', planTemplate: '1',
  })
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true); setErr('')
    try { await api.createPerson(form); onSaved() }
    catch (e: any) { setErr(String(e)) } finally { setBusy(false) }
  }

  return (
    <Modal title="Add person" onClose={onClose}>
      <form onSubmit={submit} className="form">
        <div className="form-row">
          <Field label="Employee ID" hint="required by device">
            <input value={form.employeeNo} onChange={(e) => setForm({ ...form, employeeNo: e.target.value })} required />
          </Field>
          <Field label="Name">
            <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required />
          </Field>
        </div>
        <Field label="Gender">
          <div className="seg">
            {(['male', 'female', 'unknown'] as const).map((g) => (
              <button type="button" key={g} className={form.gender === g ? 'active' : ''} onClick={() => setForm({ ...form, gender: g })}>{g}</button>
            ))}
          </div>
        </Field>
        <Field label="Type">
          <div className="seg">
            {([['normal', 'Normal'], ['visitor', 'Visitor'], ['blackList', 'Blocklist']] as const).map(([v, l]) => (
              <button type="button" key={v} className={form.personType === v ? 'active' : ''} onClick={() => setForm({ ...form, personType: v })}>{l}</button>
            ))}
          </div>
        </Field>
        <Field label="Role">
          <div className="seg">
            {([['basic', 'Basic'], ['administrator', 'Admin'], ['operator', 'Operator']] as const).map(([v, l]) => (
              <button type="button" key={v} className={form.personRole === v ? 'active' : ''} onClick={() => setForm({ ...form, personRole: v })}>{l}</button>
            ))}
          </div>
        </Field>
        <div className="form-row">
          <Field label="Door right"><input value={form.doorRight} onChange={(e) => setForm({ ...form, doorRight: e.target.value })} /></Field>
          <Field label="Plan template"><input value={form.planTemplate} onChange={(e) => setForm({ ...form, planTemplate: e.target.value })} /></Field>
        </div>
        <label className="check">
          <input type="checkbox" checked={form.longTerm} onChange={(e) => setForm({ ...form, longTerm: e.target.checked })} />
          <span>Long-term effective user (no expiry)</span>
        </label>
        {!form.longTerm && (
          <div className="form-row">
            <Field label="Valid from"><input type="datetime-local" value={form.validBegin || ''} onChange={(e) => setForm({ ...form, validBegin: e.target.value })} /></Field>
            <Field label="Valid until"><input type="datetime-local" value={form.validEnd || ''} onChange={(e) => setForm({ ...form, validEnd: e.target.value })} /></Field>
          </div>
        )}
        <label className="check">
          <input type="checkbox" checked={form.attendanceOnly} onChange={(e) => setForm({ ...form, attendanceOnly: e.target.checked })} />
          <span>Attendance check only</span>
        </label>
        <div className="form-actions">
          <button type="button" className="btn-ghost" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn-primary" disabled={busy}>{busy ? 'Saving…' : 'Save person'}</button>
        </div>
      </form>
      {err && <div className="err">{err}</div>}
    </Modal>
  )
}

function PersonDetailModal({ personId, onClose, onDeleted, onEnrol }: { personId: string; onClose: () => void; onDeleted: () => void; onEnrol?: (personId: string) => void }) {
  const [data, setData] = useState<any>(null)
  const [err, setErr] = useState('')
  const [qrBust, setQrBust] = useState(0)

  useEffect(() => {
    api.getPerson(personId).then(setData).catch((e) => setErr(String(e)))
  }, [personId])

  const rotateQR = async () => {
    try {
      const r = await api.rotateQR(personId)
      const fresh = await api.getPerson(personId)
      setData(fresh)
      setQrBust(Date.now())
      if (r.qrToken) navigator.clipboard?.writeText(r.qrToken)
    } catch (e: any) { alert(String(e)) }
  }

  if (err) return <Modal title="Person" onClose={onClose}><div className="err">{err}</div></Modal>
  if (!data) return <Modal title="Person" onClose={onClose}><div className="empty">Loading…</div></Modal>

  const p = data.person
  const faces = data.faces || []
  const cards: string[] = p?.metadata?.cards || []

  return (
    <Modal title={p.name} onClose={onClose}>
      <div className="person-detail">
        <div className="detail-faces">
          {faces.length === 0
            ? <div className="snap-box big"><div className="snap-placeholder">no face</div></div>
            : faces.map((f: any) => (
                <div key={f.id} className="snap-box big">
                  <img className="snap-img" src={api.imageUrl(f.imageKey)} alt="" />
                </div>
              ))
          }
        </div>
        {onEnrol && (
          <button className="btn-primary" onClick={() => onEnrol(personId)}>
            {faces.length === 0 ? 'Enrol face' : 'Add / replace face'}
          </button>
        )}
        <div className="detail-rows">
          <DetailRow label="Employee #" value={<span className="mono">{p.employeeNo}</span>} />
          <DetailRow label="Gender" value={p.gender} />
          <DetailRow label="Type" value={<span className="chip">{p.personType}</span>} />
          <DetailRow label="Role" value={<span className="chip">{p.personRole}</span>} />
          <DetailRow label="Door right" value={p.doorRight} />
          <DetailRow label="Plan template" value={p.planTemplate} />
          <DetailRow label="Validity" value={
            p.longTerm ? 'long-term' :
            (p.validBegin && p.validEnd
              ? `${new Date(p.validBegin).toLocaleString()} → ${new Date(p.validEnd).toLocaleString()}`
              : '—')
          } />
          <DetailRow label="Attendance only" value={p.attendanceOnly ? 'yes' : 'no'} />
          <DetailRow label="Cards" value={cards.length ? cards.join(', ') : <span className="muted">none</span>} />
          <DetailRow label="ID" value={<span className="mono small">{p.id}</span>} />
          <DetailRow label="Created" value={<span className="small muted">{new Date(p.createdAt).toLocaleString()}</span>} />
        </div>
      </div>

      <div className="qr-block">
        <h3 className="section-title">QR token (for 2-step auth)</h3>
        {p.qrToken ? (
          <div className="qr-row">
            <img className="qr-img" src={`${api.qrImageUrl(personId, 240)}&_b=${qrBust}`} alt="QR" />
            <div className="qr-meta">
              <div className="mono small" style={{ wordBreak: 'break-all' }}>{p.qrToken}</div>
              <p className="muted small">Print or save this. Scanning it at an agent's USB scanner unlocks the face camera briefly for this user.</p>
              <button className="btn-ghost" onClick={rotateQR}>Rotate token</button>
            </div>
          </div>
        ) : (
          <div className="qr-row">
            <div className="empty" style={{ flex: 1 }}>No QR token yet.</div>
            <button className="btn-primary" onClick={rotateQR}>Generate</button>
          </div>
        )}
      </div>
    </Modal>
  )
}

function DetailRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="detail-row">
      <span className="detail-label">{label}</span>
      <span className="detail-value">{value}</span>
    </div>
  )
}

// ===================== Enrol =====================

function EnrolTab({ initialPersonId, onConsumeInitial }: { initialPersonId?: string; onConsumeInitial?: () => void } = {}) {
  const [devices, setDevices] = useState<any[]>([])
  const [persons, setPersons] = useState<any[]>([])
  const [deviceId, setDeviceId] = useState('')
  const [personId, setPersonId] = useState(initialPersonId || '')
  const [file, setFile] = useState<File | null>(null)
  const [FDID, setFDID] = useState('1')
  const [faceLibType, setFaceLibType] = useState('blackFD')
  const [preview, setPreview] = useState<string | null>(null)
  const [result, setResult] = useState<any>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [cameraOpen, setCameraOpen] = useState(false)
  // Phase 2 — capture-at-device state
  const [capBusy, setCapBusy] = useState('')
  const [capResult, setCapResult] = useState<any>(null)
  const [cardNo, setCardNo] = useState('')

  useEffect(() => {
    api.listDevices().then((d) => {
      const list = d || []
      setDevices(list)
      // Pre-select the device: the only one, else the first that's online.
      if (list.length === 1) setDeviceId(list[0].deviceID)
      else { const on = list.find((x: any) => x.online); if (on) setDeviceId(on.deviceID) }
    }).catch(() => setDevices([]))
    api.listPersons().then((p) => setPersons(p || [])).catch(() => setPersons([]))
    // If we arrived here from a person's "Enrol face" button, clear that
    // hand-off state now that personId has been seeded from it.
    if (initialPersonId) onConsumeInitial?.()
  }, [])

  useEffect(() => {
    if (!file) { setPreview(null); return }
    const url = URL.createObjectURL(file); setPreview(url)
    return () => URL.revokeObjectURL(url)
  }, [file])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!deviceId || !file || !personId) { setErr('Pick a device, a person, and a face image'); return }
    setBusy(true); setErr(''); setResult(null)
    try {
      const r = await api.enrolFace(deviceId, personId, file, { FDID, faceLibType })
      setResult(r)
    } catch (e: any) { setErr(String(e)) } finally { setBusy(false) }
  }

  const selectedPerson = persons.find((p) => p.id === personId)
  const employeeNo = selectedPerson?.employeeNo || ''

  // Run a capture/card/fingerprint action against the device and show the raw result.
  const runCapture = async (label: string, fn: () => Promise<any>) => {
    if (!deviceId) { setErr('Pick a device first'); return }
    setCapBusy(label); setErr(''); setCapResult(null)
    try { setCapResult(await fn()) }
    catch (e: any) { setErr(String(e)) }
    finally { setCapBusy('') }
  }

  return (
    <>
      <div className="page-toolbar"><h1 className="page-title">Enrol face</h1></div>
      <div className="grid two-col">
        <Card title="Push face to device">
          <p className="card-sub">Sends the person record (with role and validity) to the device, then attaches the face image.</p>
          <form onSubmit={submit} className="form">
            <Field label="Device">
              <select value={deviceId} onChange={(e) => setDeviceId(e.target.value)} required>
                <option value="">— Select device —</option>
                {devices.map((d) => (
                  <option key={d.deviceID} value={d.deviceID}>
                    {(d.name || d.deviceID) + (d.online ? '  (online)' : '  (offline)')}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Person">
              <select value={personId} onChange={(e) => setPersonId(e.target.value)} required>
                <option value="">— Select person —</option>
                {persons.map((p) => (
                  <option key={p.id} value={p.id}>{p.name} — #{p.employeeNo} — {p.personRole || 'basic'}</option>
                ))}
              </select>
            </Field>
            <Field label="Face image">
              <div className="image-source">
                <label className="upload-tile">
                  <input type="file" accept="image/jpeg,image/jpg,image/png" onChange={(e) => setFile(e.target.files?.[0] || null)} />
                  <span className="upload-tile-title">Upload file</span>
                  <span className="upload-tile-sub">JPEG or PNG</span>
                </label>
                <button type="button" className="upload-tile" onClick={() => setCameraOpen(true)}>
                  <span className="upload-tile-title">Use camera</span>
                  <span className="upload-tile-sub">Capture from webcam or phone</span>
                </button>
              </div>
              {file && <div className="muted small file-info">{file.name} · {(file.size / 1024).toFixed(0)} KB</div>}
            </Field>
            <div className="form-row">
              <Field label="FDID"><input value={FDID} onChange={(e) => setFDID(e.target.value)} /></Field>
              <Field label="Face lib type"><input value={faceLibType} onChange={(e) => setFaceLibType(e.target.value)} /></Field>
            </div>
            <button type="submit" className="btn-primary" disabled={busy}>{busy ? 'Pushing to device…' : 'Enrol'}</button>
          </form>
          {err && <div className="err">{err}</div>}
          {result && <pre className="result">{JSON.stringify(result, null, 2)}</pre>}
        </Card>
        <Card title="Preview">
          {preview
            ? <img src={preview} className="preview-img" alt="" />
            : <div className="empty">Upload a file or capture from your camera.</div>}
        </Card>
      </div>

      <Card title="Capture at the device (reader)">
        <p className="card-sub">
          Ask the selected reader to capture a credential live — the user presents their face / card / finger
          at the door instead of uploading a photo. Captured data is returned below.
          {employeeNo
            ? <> Card/fingerprint actions bind to <strong>{selectedPerson?.name}</strong> (#{employeeNo}).</>
            : <> Select a person above to bind cards/fingerprints.</>}
        </p>
        <div className="form-row" style={{ flexWrap: 'wrap', gap: 8 }}>
          <button type="button" className="btn-ghost" disabled={!!capBusy}
            onClick={() => runCapture('face', () => api.captureFace(deviceId))}>
            {capBusy === 'face' ? 'Capturing face…' : 'Capture face'}
          </button>
          <button type="button" className="btn-ghost" disabled={!!capBusy}
            onClick={() => runCapture('card', () => api.captureCard(deviceId))}>
            {capBusy === 'card' ? 'Swipe card now…' : 'Capture card'}
          </button>
          <button type="button" className="btn-ghost" disabled={!!capBusy}
            onClick={() => runCapture('finger', () => api.captureFingerprint(deviceId))}>
            {capBusy === 'finger' ? 'Press finger now…' : 'Capture fingerprint'}
          </button>
        </div>
        <div className="form-row" style={{ marginTop: 12, alignItems: 'flex-end' }}>
          <Field label="Bind card number" grow={2}>
            <input value={cardNo} onChange={(e) => setCardNo(e.target.value)} placeholder="e.g. 1234567890" />
          </Field>
          <button type="button" className="btn-primary" disabled={!!capBusy || !employeeNo || !cardNo}
            onClick={() => runCapture('bindcard', () => api.setCard(deviceId, { employeeNo, cardNo }))}>
            {capBusy === 'bindcard' ? 'Binding…' : 'Bind card to person'}
          </button>
        </div>
        {capResult && <pre className="result">{JSON.stringify(capResult, null, 2)}</pre>}
      </Card>

      {cameraOpen && (
        <CameraCapture
          onClose={() => setCameraOpen(false)}
          onCapture={(f) => { setFile(f); setCameraOpen(false) }}
        />
      )}
    </>
  )
}

// ===================== Camera =====================

function CameraCapture({ onCapture, onClose }: { onCapture: (file: File) => void; onClose: () => void }) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const streamRef = useRef<MediaStream | null>(null)
  const [facing, setFacing] = useState<'user' | 'environment'>('user')
  const [flashOn, setFlashOn] = useState(false)
  const [flashSupported, setFlashSupported] = useState(false)
  const [previewUrl, setPreviewUrl] = useState<string | null>(null)
  const [blob, setBlob] = useState<Blob | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [starting, setStarting] = useState(true)
  const [multiCamera, setMultiCamera] = useState(false)

  // Check if multiple cameras exist (otherwise hide flip button)
  useEffect(() => {
    navigator.mediaDevices?.enumerateDevices?.().then((devs) => {
      const cams = devs.filter((d) => d.kind === 'videoinput')
      setMultiCamera(cams.length > 1)
    }).catch(() => {})
  }, [])

  // Start / restart camera when facing toggles
  useEffect(() => {
    let cancelled = false
    const start = async () => {
      setStarting(true); setError(null)
      if (streamRef.current) {
        streamRef.current.getTracks().forEach((t) => t.stop())
        streamRef.current = null
      }
      try {
        const stream = await navigator.mediaDevices.getUserMedia({
          video: { facingMode: facing, width: { ideal: 1280 }, height: { ideal: 960 } },
          audio: false,
        })
        if (cancelled) { stream.getTracks().forEach((t) => t.stop()); return }
        streamRef.current = stream
        if (videoRef.current) {
          videoRef.current.srcObject = stream
          await videoRef.current.play().catch(() => {})
        }
        const track = stream.getVideoTracks()[0]
        const caps = (track.getCapabilities?.() as any) || {}
        setFlashSupported(!!caps.torch)
        setFlashOn(false)
        setStarting(false)
      } catch (e: any) {
        setError(e?.message || 'Could not open camera')
        setStarting(false)
      }
    }
    if (!previewUrl) start()
    return () => {
      cancelled = true
      if (streamRef.current) {
        streamRef.current.getTracks().forEach((t) => t.stop())
        streamRef.current = null
      }
    }
  }, [facing, previewUrl])

  const flip = () => setFacing((f) => (f === 'user' ? 'environment' : 'user'))

  const toggleFlash = async () => {
    const track = streamRef.current?.getVideoTracks()[0]
    if (!track) return
    try {
      await track.applyConstraints({ advanced: [{ torch: !flashOn } as any] })
      setFlashOn(!flashOn)
    } catch {
      setFlashSupported(false)
    }
  }

  const snap = () => {
    const video = videoRef.current
    const canvas = canvasRef.current
    if (!video || !canvas || !video.videoWidth) return
    canvas.width = video.videoWidth
    canvas.height = video.videoHeight
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    ctx.drawImage(video, 0, 0, canvas.width, canvas.height)
    canvas.toBlob((b) => {
      if (b) {
        setBlob(b)
        setPreviewUrl(URL.createObjectURL(b))
      }
    }, 'image/jpeg', 0.92)
  }

  const retake = () => {
    if (previewUrl) URL.revokeObjectURL(previewUrl)
    setPreviewUrl(null); setBlob(null)
  }

  const commit = () => {
    if (!blob) return
    const file = new File([blob], `capture-${Date.now()}.jpg`, { type: 'image/jpeg' })
    onCapture(file)
  }

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
      else if (e.key === ' ' && !previewUrl) { e.preventDefault(); snap() }
    }
    document.addEventListener('keydown', onKey)
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = ''
    }
  }, [previewUrl, onClose])

  return (
    <div className="camera-overlay" onClick={onClose}>
      <div className="camera-app" onClick={(e) => e.stopPropagation()}>
        <header className="camera-header">
          <button className="camera-icon-btn" onClick={onClose} aria-label="close">×</button>
          <span className="camera-title">{previewUrl ? 'Preview' : 'Camera'}</span>
          <span style={{ width: 36 }} />
        </header>

        <div className="camera-stage">
          {previewUrl ? (
            <img className="camera-stage-media" src={previewUrl} alt="" />
          ) : (
            <video
              ref={videoRef}
              className={`camera-stage-media ${facing === 'user' ? 'mirror' : ''}`}
              playsInline
              muted
              autoPlay
            />
          )}
          {starting && !previewUrl && !error && <div className="camera-status">Opening camera…</div>}
          {error && (
            <div className="camera-status camera-error">
              <div>{error}</div>
              <div className="muted small" style={{ marginTop: 8 }}>
                Grant camera permission in your browser. On phones, the site needs HTTPS unless you're on localhost.
              </div>
            </div>
          )}
        </div>

        <div className="camera-controls">
          {previewUrl ? (
            <>
              <button className="camera-btn" onClick={retake}>Retake</button>
              <button className="btn-primary camera-commit" onClick={commit}>Use this photo</button>
            </>
          ) : (
            <>
              <button
                className="camera-btn"
                onClick={toggleFlash}
                disabled={!flashSupported}
                title={flashSupported ? 'Toggle flash' : 'Flash not supported by this camera'}
              >
                {flashOn ? 'Flash on' : 'Flash'}
              </button>
              <button
                className="camera-shutter"
                onClick={snap}
                disabled={starting || !!error}
                aria-label="capture"
              />
              <button
                className="camera-btn"
                onClick={flip}
                disabled={!multiCamera && facing === 'user'}
                title="Switch camera"
              >Flip</button>
            </>
          )}
        </div>

        <canvas ref={canvasRef} style={{ display: 'none' }} />
      </div>
    </div>
  )
}

// ===================== Events =====================

function EventsTab() {
  const [events, setEvents] = useState<any[]>([])
  const [deviceFilter, setDeviceFilter] = useState('')

  useEffect(() => {
    api.listEvents(deviceFilter || undefined).then((e) => setEvents(e || [])).catch(() => setEvents([]))
    const unsub = subscribeEvents((e) => {
      if (deviceFilter && e.deviceId !== deviceFilter) return
      setEvents((prev) => [e, ...prev].slice(0, 300))
    })
    return unsub
  }, [deviceFilter])

  return (
    <>
      <div className="page-toolbar">
        <div className="toolbar-left">
          <h1 className="page-title">Events <span className="muted">· {events.length}</span></h1>
          <input className="search" placeholder="Filter by device ID" value={deviceFilter} onChange={(e) => setDeviceFilter(e.target.value)} />
        </div>
        <div className="toolbar-right">
          <button className="btn-ghost" onClick={async () => {
            const r = await api.purgeEmptyEvents()
            if (r.deleted > 0) {
              const fresh = await api.listEvents(deviceFilter || undefined)
              setEvents(fresh || [])
            }
          }}>Purge empty</button>
        </div>
      </div>

      <div className="event-list">
        {events.map((e) => (
          <div key={e.id} className="event">
            {e.imageKey && <img src={api.imageUrl(e.imageKey)} alt="" />}
            <div className="event-body">
              <div className="event-head">
                <span className="mono small">{e.deviceId}</span>
                <span className="chip">{e.eventType || 'event'}</span>
                <span className="muted small">{new Date(e.receivedAt).toLocaleString()}</span>
              </div>
              <pre>{JSON.stringify(e.raw, null, 2)}</pre>
            </div>
          </div>
        ))}
        {events.length === 0 && (
          <div className="empty">No events yet. Click "Events" on a device to register face_auth as its alarm host.</div>
        )}
      </div>
    </>
  )
}

// ===================== ISAPI Console =====================

function ConsoleTab() {
  const [devices, setDevices] = useState<any[]>([])
  const [deviceId, setDeviceId] = useState('')
  const [method, setMethod] = useState('GET')
  const [path, setPath] = useState('/ISAPI/System/deviceInfo?format=json')
  const [body, setBody] = useState('')
  const [result, setResult] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  useEffect(() => { api.listDevices().then((d) => setDevices(d || [])).catch(() => setDevices([])) }, [])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!deviceId) return
    setBusy(true); setErr(''); setResult(null)
    try {
      const r = await api.rawIsapi(deviceId, method, path, body)
      setResult(typeof r === 'string' ? r : JSON.stringify(r, null, 2))
    } catch (e: any) { setErr(String(e)) } finally { setBusy(false) }
  }

  const presets = [
    { label: 'Device info', method: 'GET', path: '/ISAPI/System/deviceInfo?format=json' },
    { label: 'User capabilities', method: 'GET', path: '/ISAPI/AccessControl/UserInfo/capabilities?format=json' },
    { label: 'List users', method: 'POST', path: '/ISAPI/AccessControl/UserInfo/Search?format=json',
      body: '{"UserInfoSearchCond":{"searchID":"1","searchResultPosition":0,"maxResults":50}}' },
    { label: 'List face libs', method: 'GET', path: '/ISAPI/Intelligent/FDLib?format=json' },
    { label: 'Reboot', method: 'PUT', path: '/ISAPI/System/reboot' },
    { label: 'Open door 1', method: 'PUT', path: '/ISAPI/AccessControl/RemoteControl/door/1',
      body: '<RemoteControlDoor><cmd>open</cmd></RemoteControlDoor>' },
  ]

  return (
    <>
      <div className="page-toolbar"><h1 className="page-title">ISAPI Console</h1></div>
      <div className="grid two-col">
        <Card title="Request">
          <p className="card-sub">Send any request straight to the device. Backend handles Digest auth.</p>
          <form onSubmit={submit} className="form">
            <Field label="Device">
              <select value={deviceId} onChange={(e) => setDeviceId(e.target.value)} required>
                <option value="">— Select —</option>
                {devices.map((d) => <option key={d.deviceID} value={d.deviceID}>{d.name || d.deviceID}</option>)}
              </select>
            </Field>
            <Field label="Preset">
              <select onChange={(e) => {
                const p = presets[parseInt(e.target.value, 10)]
                if (p) { setMethod(p.method); setPath(p.path); setBody(p.body || '') }
              }} defaultValue="">
                <option value="">—</option>
                {presets.map((p, i) => <option key={i} value={i}>{p.label}</option>)}
              </select>
            </Field>
            <div className="form-row">
              <Field label="Method" grow={1}>
                <select value={method} onChange={(e) => setMethod(e.target.value)}>
                  <option>GET</option><option>POST</option><option>PUT</option><option>DELETE</option>
                </select>
              </Field>
              <Field label="Path" grow={5}>
                <input value={path} onChange={(e) => setPath(e.target.value)} />
              </Field>
            </div>
            <Field label="Body (JSON or XML)">
              <textarea rows={6} value={body} onChange={(e) => setBody(e.target.value)} />
            </Field>
            <button type="submit" className="btn-primary" disabled={busy}>{busy ? 'Sending…' : 'Send'}</button>
          </form>
          {err && <div className="err">{err}</div>}
        </Card>
        <Card title="Response">
          {result ? <pre className="result tall">{result}</pre> : <div className="empty">Response will appear here.</div>}
        </Card>
      </div>
    </>
  )
}

// ===================== Agents =====================

function AgentsTab() {
  const [agents, setAgents] = useState<any[]>([])
  const [downloads, setDownloads] = useState<any[]>([])
  const [creating, setCreating] = useState(false)
  const [credentials, setCredentials] = useState<{ id: string; token: string } | null>(null)
  const cloudHost = window.location.host

  const load = () => api.listAgents().then((a) => setAgents(a || [])).catch(() => setAgents([]))
  useEffect(() => {
    load(); const t = setInterval(load, 4000)
    api.agentDownloads().then((d) => setDownloads(d || [])).catch(() => {})
    return () => clearInterval(t)
  }, [])

  const del = async (id: string) => {
    if (!confirm(`Delete agent ${id}? Devices reaching via this agent will go offline.`)) return
    await api.deleteAgent(id); load()
  }
  const regen = async (id: string) => {
    if (!confirm(`Regenerate token for ${id}? The current agent will disconnect until updated.`)) return
    const r = await api.regenAgentToken(id)
    setCredentials({ id, token: r.token })
  }

  return (
    <>
      <div className="page-toolbar">
        <div className="toolbar-left">
          <h1 className="page-title">Agents <span className="muted">· {agents.length}</span></h1>
        </div>
        <div className="toolbar-right">
          <button className="btn-primary" onClick={() => setCreating(true)}>Add agent</button>
        </div>
      </div>

      <Card title="How agents work">
        <div className="arch-diagram">
          <div className="arch-node cloud">
            <strong>Cloud (this server)</strong>
            <span className="muted small">face_auth on your VPS · public URL · admin UI accessible anywhere</span>
          </div>
          <div className="arch-arrow">↑ outbound WebSocket (TLS)<br /><span className="muted small">agent dials cloud — no port-forward, no NAT pain</span></div>
          <div className="arch-node lan">
            <strong>LAN (where the device lives)</strong>
            <span className="muted small">face_auth-agent · single small binary · runs on any always-on box</span>
          </div>
          <div className="arch-arrow">↓ HTTP/Digest (LAN only)</div>
          <div className="arch-node device">
            <strong>Hik face device</strong>
            <span className="muted small">192.168.x.x · stays on private network · never exposed to internet</span>
          </div>
        </div>
        <p className="card-sub" style={{ marginTop: 14 }}>
          Create an agent below to generate a token, install the agent binary on any always-on machine on the same LAN as the device,
          and the cloud picks it up automatically. When you add a device, choose this agent in the "Reach via" dropdown.
        </p>
      </Card>

      {credentials && (
        <Card title="Agent ready — copy the credentials now" header={<button className="btn-ghost" onClick={() => setCredentials(null)}>Dismiss</button>}>
          <p className="card-sub">Token is shown only once. After you dismiss this, you can only regenerate.</p>
          <div className="bridge-rows">
            <BridgeRow label="Agent ID" value={credentials.id} onCopy={() => navigator.clipboard?.writeText(credentials.id)} />
            <BridgeRow label="Token" value={credentials.token} onCopy={() => navigator.clipboard?.writeText(credentials.token)} />
            <BridgeRow label="Cloud URL" value={`wss://${cloudHost}`} onCopy={() => navigator.clipboard?.writeText(`wss://${cloudHost}`)} />
          </div>

          <h3 className="section-title">Install the agent on a LAN machine</h3>
          <div className="install-tabs">
            <details className="bridge-steps" open>
              <summary>Windows</summary>
              <p>Download <code>face_auth-agent-windows-amd64.exe</code> below, then in a Command Prompt run:</p>
              <pre className="result">{`set CLOUD_URL=wss://${cloudHost}
set AGENT_ID=${credentials.id}
set AGENT_TOKEN=${credentials.token}
set AGENT_NAME=LAN agent
face_auth-agent-windows-amd64.exe`}</pre>
              <p className="muted small">To install as a service (auto-start on boot), use NSSM: <code>nssm install face_auth-agent C:\path\to\face_auth-agent.exe</code></p>
            </details>
            <details className="bridge-steps">
              <summary>Linux / Raspberry Pi</summary>
              <p>Download the right binary for your machine, then:</p>
              <pre className="result">{`chmod +x face_auth-agent-linux-amd64
CLOUD_URL=wss://${cloudHost} \\
AGENT_ID=${credentials.id} \\
AGENT_TOKEN=${credentials.token} \\
AGENT_NAME="LAN agent" \\
./face_auth-agent-linux-amd64`}</pre>
              <p className="muted small">For auto-start, create a systemd unit at <code>/etc/systemd/system/face_auth-agent.service</code>.</p>
            </details>
            <details className="bridge-steps">
              <summary>macOS</summary>
              <pre className="result">{`chmod +x face_auth-agent-darwin-arm64
CLOUD_URL=wss://${cloudHost} \\
AGENT_ID=${credentials.id} \\
AGENT_TOKEN=${credentials.token} \\
./face_auth-agent-darwin-arm64`}</pre>
            </details>
            <details className="bridge-steps">
              <summary>Docker (any OS)</summary>
              <pre className="result">{`docker run -d --name face_auth-agent --restart unless-stopped --network host \\
  -e CLOUD_URL=wss://${cloudHost} \\
  -e AGENT_ID=${credentials.id} \\
  -e AGENT_TOKEN=${credentials.token} \\
  -e AGENT_NAME="LAN agent" \\
  face_auth/agent`}</pre>
            </details>
            <details className="bridge-steps">
              <summary>USB QR scanner — plug into the agent host (Windows)</summary>
              <p>Windows doesn't have <code>/dev/input/*</code>, but a small AutoHotkey script catches scanner keystrokes (faster than human typing) and posts them to the agent. No interference with normal typing.</p>
              <ol>
                <li>Install <a href="https://www.autohotkey.com/" target="_blank" rel="noreferrer">AutoHotkey v2</a> on the Windows machine where the agent runs.</li>
                <li>Download the watcher script: <a href={api.agentScriptUrl('qr-watcher.ahk')} download>qr-watcher.ahk</a></li>
                <li>Right-click the downloaded file → <strong>Run script</strong>. You'll see a tray icon "face_auth QR watcher".</li>
                <li>Make it auto-start at login: press <kbd>Win+R</kbd> → type <code>shell:startup</code> → drag the .ahk file into that folder.</li>
                <li>Scan a QR — the tray icon shows "Scan forwarded: 1" and the QR Auth tab shows a new session.</li>
              </ol>
              <p className="muted small">Configure your scanner to add a fixed prefix (e.g. <code>in#</code>) and set <code>QR_STRIP_PREFIX=in#</code> on the agent. That gives an extra layer of certainty that only scanner input ever reaches face_auth — even if someone manually types something that happens to match a QR token format, it'll be ignored without the prefix.</p>
            </details>
            <details className="bridge-steps">
              <summary>USB QR scanner — plug into the agent host (Linux)</summary>
              <p>When the agent is running on a Linux box (Pi, mini-PC, NAS), it can read keystrokes from a USB QR scanner natively — no helper script, no admin UI clicks. The flow becomes: user scans QR → device's face camera unlocks for ~5 seconds → user shows face → door opens.</p>
              <ol>
                <li>Plug the scanner into the agent host. Find its event device:
                  <pre className="result">{`ls -l /dev/input/by-id/  # find the scanner — usually contains "barcode" / "scanner" / vendor name`}</pre>
                </li>
                <li>Add to the agent's env (or docker run):
                  <pre className="result">{`QR_DEVICE=/dev/input/by-id/usb-Symbol_Bar_Code_Scanner-event-kbd
QR_STRIP_PREFIX=in#   # only if your scanner is programmed to add this`}</pre>
                </li>
                <li>If running in Docker, expose the device:
                  <pre className="result">{`docker run -d --restart unless-stopped --network host \\
  --device /dev/input/by-id/usb-Symbol_Bar_Code_Scanner-event-kbd:/dev/input/scanner \\
  -e QR_DEVICE=/dev/input/scanner \\
  -e CLOUD_URL=wss://${cloudHost} \\
  -e AGENT_ID=${credentials.id} \\
  -e AGENT_TOKEN=${credentials.token} \\
  face_auth/agent`}</pre>
                </li>
                <li>Or auto-discover: set <code>QR_DEVICE_AUTO=true</code> and the agent picks the first scanner-like device under <code>/dev/input/by-id/</code>.</li>
                <li>You'll see in the agent log: <code>HID: listening on /dev/input/...</code> and each scan logged as <code>HID scan -&gt; cloud: 200 ...</code></li>
              </ol>
              <p className="muted small">Tip: the user running the agent needs read access on the device — either add to the <code>input</code> group (<code>sudo usermod -aG input $USER</code>) or run with elevated privileges. Docker with <code>--device</code> handles this automatically.</p>
            </details>
            <details className="bridge-steps">
              <summary>Android (via Termux)</summary>
              <p>Install <a href="https://termux.dev/" target="_blank" rel="noreferrer">Termux</a> from F-Droid. Inside Termux:</p>
              <pre className="result">{`pkg install wget
wget ${api.agentDownloadUrl('face_auth-agent-linux-arm64')} -O face_auth-agent
chmod +x face_auth-agent
CLOUD_URL=wss://${cloudHost} \\
AGENT_ID=${credentials.id} \\
AGENT_TOKEN=${credentials.token} \\
./face_auth-agent`}</pre>
              <p className="muted small">A native APK isn't shipped — Termux is the simplest way to run the agent on an Android device.</p>
            </details>
          </div>
        </Card>
      )}

      <Card title={`Agent binaries (${downloads.length})`}>
        <p className="card-sub">Pre-built for every common platform. Download, drop on your LAN machine, run with the env vars from a new agent.</p>
        <div className="download-grid">
          {downloads.map((d) => (
            <a key={d.file} className="download-tile" href={api.agentDownloadUrl(d.file)} download>
              <strong>{d.label}</strong>
              <span className="muted mono small">{d.file}</span>
              <span className="muted small">{(d.size / 1024 / 1024).toFixed(1)} MB</span>
            </a>
          ))}
          {downloads.length === 0 && <div className="empty">No agent binaries bundled in this build.</div>}
        </div>
      </Card>

      <Card title="Companion: USB QR scanner watcher">
        <p className="card-sub">
          For production with a USB QR scanner plugged into the agent's host machine. Pick the helper that matches the host OS.
        </p>
        <div className="download-grid">
          <a className="download-tile" href={api.agentScriptUrl('qr-watcher.ahk')} download>
            <strong>Windows — AutoHotkey v2 script</strong>
            <span className="muted mono small">qr-watcher.ahk</span>
            <span className="muted small">Install AHK v2 → run this once → put in shell:startup</span>
          </a>
          <div className="download-tile" style={{ cursor: 'default' }}>
            <strong>Linux — built into the agent</strong>
            <span className="muted small">set <code>QR_DEVICE=/dev/input/by-id/usb-…</code> in the env, or <code>QR_DEVICE_AUTO=true</code></span>
            <span className="muted small">no extra script needed — agent reads HID natively</span>
          </div>
          <div className="download-tile" style={{ cursor: 'default' }}>
            <strong>macOS — manual</strong>
            <span className="muted small">most users plug into a Pi/Linux box instead. If macOS is required, a small helper can POST to <code>/scan</code></span>
          </div>
        </div>

        <details className="bridge-steps" style={{ marginTop: 14 }}>
          <summary>Windows setup — full steps</summary>
          <ol>
            <li>Install <a href="https://www.autohotkey.com/" target="_blank" rel="noreferrer">AutoHotkey v2</a> on the Windows machine where the agent runs.</li>
            <li>Click the <strong>Windows — AutoHotkey v2 script</strong> tile above to download <code>qr-watcher.ahk</code>.</li>
            <li>Double-click the file. A tray icon appears (look in the bottom-right of your taskbar, click the <code>^</code> arrow to expand hidden icons).</li>
            <li>Right-click the tray icon → <strong>Send a test scan</strong>. Within ~1 s a new entry should appear in the <strong>QR Auth</strong> tab (likely as "unknown QR token" — that's expected, it proves the pipeline works).</li>
            <li>Make it auto-start at login: press <kbd>Win+R</kbd> → type <code>shell:startup</code> → drag the .ahk file into that folder.</li>
            <li>Configure your scanner to add a fixed prefix like <code>in#</code> (one-time scan of a config QR from its manual). Set <code>QR_STRIP_PREFIX=in#</code> on the agent.</li>
            <li>Plug in scanner. Done — scans now flow scanner → AHK → agent → cloud → device unlocks face for 5s.</li>
          </ol>
          <p className="muted small">
            <strong>Troubleshooting:</strong> right-click the tray icon → <strong>Open log file</strong>. Every keystroke buffer assembly, forward, and agent response is logged. If the log shows <code>ERROR: 12029</code> the agent isn't reachable on <code>127.0.0.1:7771</code> — start the agent service first.
          </p>
        </details>
      </Card>

      <Card title={`Registered agents (${agents.length})`}>
        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr><th>Agent</th><th>Status</th><th>Created</th><th className="ta-right">Actions</th></tr>
            </thead>
            <tbody>
              {agents.map((a) => (
                <tr key={a.id}>
                  <td data-label="Agent">
                    <div className="cell-stack">
                      <strong>{a.name || a.id}</strong>
                      <span className="muted mono small">{a.id}</span>
                    </div>
                  </td>
                  <td data-label="Status">
                    <span className={`badge ${a.online ? 'ok' : ''}`}>
                      <span className="status-dot" />{a.online ? 'connected' : 'disconnected'}
                    </span>
                  </td>
                  <td data-label="Created" className="small muted">{new Date(a.createdAt).toLocaleString()}</td>
                  <td data-label="Actions" className="ta-right">
                    <div className="cell-actions">
                      <button className="btn-ghost" onClick={() => regen(a.id)}>Regen token</button>
                      <button className="btn-danger" onClick={() => del(a.id)}>Delete</button>
                    </div>
                  </td>
                </tr>
              ))}
              {agents.length === 0 && (
                <tr><td colSpan={4}><div className="empty">No agents yet. Click "Add agent" to generate credentials, then install the agent on a LAN machine.</div></td></tr>
              )}
            </tbody>
          </table>
        </div>
      </Card>

      {creating && <AgentModal onClose={() => setCreating(false)} onCreated={(c) => { setCreating(false); setCredentials(c); load() }} />}
    </>
  )
}

function AgentModal({ onClose, onCreated }: { onClose: () => void; onCreated: (c: { id: string; token: string }) => void }) {
  const [id, setId] = useState('')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true); setErr('')
    try {
      const r = await api.createAgent(id, name)
      onCreated({ id: r.id, token: r.token })
    } catch (e: any) { setErr(String(e)) } finally { setBusy(false) }
  }

  return (
    <Modal title="Add agent" onClose={onClose}>
      <form onSubmit={submit} className="form">
        <Field label="Agent ID" hint="short, no spaces, e.g. office-lan">
          <input value={id} onChange={(e) => setId(e.target.value)} placeholder="office-lan" required pattern="[A-Za-z0-9_-]+" />
        </Field>
        <Field label="Name">
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Office front-door LAN" />
        </Field>
        <div className="form-actions">
          <button type="button" className="btn-ghost" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn-primary" disabled={busy}>{busy ? 'Creating…' : 'Create agent'}</button>
        </div>
        {err && <div className="err">{err}</div>}
      </form>
    </Modal>
  )
}

// ===================== QR Auth =====================

function QRAuthTab() {
  const [data, setData] = useState<{ active: any[]; history: any[] }>({ active: [], history: [] })
  const [devices, setDevices] = useState<any[]>([])
  const [scanInput, setScanInput] = useState('')
  const [scanResult, setScanResult] = useState<any>(null)

  const load = () => api.qrAuthSessions().then((d) => setData(d || { active: [], history: [] })).catch(() => {})
  useEffect(() => {
    load(); const t = setInterval(load, 1000)
    api.listDevices().then((d) => setDevices(d || []))
    return () => clearInterval(t)
  }, [])

  const simulateScan = async (e: React.FormEvent) => {
    e.preventDefault()
    setScanResult(null)
    try {
      const r = await api.qrAuthScan(scanInput.trim())
      setScanResult(r)
      setScanInput('')
    } catch (e: any) { setScanResult({ error: String(e) }) }
  }

  const lockAll = async (id: string) => {
    if (!confirm(`Set ALL users on ${id} to "locked" state (cardAndPw — face requires a QR scan)?`)) return
    try {
      const r = await api.lockAllUsers(id)
      alert(`Locked ${r.locked} users on the device.`)
    } catch (e: any) { alert(String(e)) }
  }

  const statusClass = (s: string) => s === 'open' ? 'ok' : s === 'face_matched' ? 'ok' : s === 'timed_out' ? '' : ''

  return (
    <>
      <div className="page-toolbar">
        <h1 className="page-title">QR Auth <span className="muted">· custom 2-step</span></h1>
      </div>

      <Card title="How it works">
        <p className="card-sub">
          Default state: every user on the device is in a "locked" verify mode (<code>cardAndPw</code>) that they can't satisfy.
          When a user scans their QR at an agent's USB scanner, face_auth briefly switches that user's mode to <code>face</code> for ~5 seconds.
          As soon as the face camera matches the user (or the window expires) face_auth re-locks them, preventing tailgating.
        </p>
        <p className="card-sub">
          One-time setup per device: click <strong>Lock all users</strong> below to seed every device user into the locked baseline. New persons enrolled via face_auth go straight into the locked state.
        </p>
        <div className="device-list" style={{ marginTop: 10 }}>
          {devices.map((d) => (
            <div key={d.deviceID} className="row-item">
              <div className="row-item-main">
                <strong>{d.name || d.deviceID}</strong>
                <div className="muted small mono">{d.deviceID}</div>
              </div>
              <button className="btn-ghost" onClick={() => lockAll(d.deviceID)}>Lock all users</button>
            </div>
          ))}
          {devices.length === 0 && <div className="empty">No devices yet.</div>}
        </div>
      </Card>

      <Card title="Simulate a scan" header={<span className="muted small">use this to test without a USB scanner</span>}>
        <form onSubmit={simulateScan} className="form-row">
          <input value={scanInput} onChange={(e) => setScanInput(e.target.value)} placeholder="paste a QR token here" style={{ flex: 4 }} />
          <button type="submit" className="btn-primary" style={{ flex: 1 }}>Scan</button>
        </form>
        {scanResult && <pre className="result">{JSON.stringify(scanResult, null, 2)}</pre>}
      </Card>

      <Card title={`Active sessions (${data.active.length})`}>
        <div className="row-list">
          {data.active.map((s) => (
            <div key={s.id} className="row-item">
              <div className="row-item-main">
                <div className="row-item-title">
                  <strong>{s.name}</strong>
                  <span className="chip">#{s.employeeNo}</span>
                  <span className="badge ok"><span className="status-dot" />face window open</span>
                </div>
                <div className="row-item-meta">
                  <span className="muted small">device {s.deviceId}</span>
                  <span className="muted small">expires {new Date(s.expiresAt).toLocaleTimeString()}</span>
                </div>
              </div>
            </div>
          ))}
          {data.active.length === 0 && <div className="empty">No active sessions. Scan a QR to open one.</div>}
        </div>
      </Card>

      <Card title={`History (${data.history.length})`}>
        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr><th>Person</th><th>Opened</th><th>Result</th><th>Duration</th></tr>
            </thead>
            <tbody>
              {data.history.map((s) => {
                const dur = (new Date(s.expiresAt).getTime() - new Date(s.openedAt).getTime()) / 1000
                return (
                  <tr key={s.id}>
                    <td data-label="Person">
                      <div className="cell-stack">
                        <strong>{s.name}</strong>
                        <span className="muted small mono">#{s.employeeNo}</span>
                      </div>
                    </td>
                    <td data-label="Opened" className="small muted">{new Date(s.openedAt).toLocaleTimeString()}</td>
                    <td data-label="Result">
                      <span className={`badge ${statusClass(s.status)}`}>
                        <span className="status-dot" />{s.status.replace('_', ' ')}
                      </span>
                    </td>
                    <td data-label="Duration" className="small muted">{dur.toFixed(1)} s</td>
                  </tr>
                )
              })}
              {data.history.length === 0 && (
                <tr><td colSpan={4}><div className="empty">No history yet.</div></td></tr>
              )}
            </tbody>
          </table>
        </div>
      </Card>
    </>
  )
}

// ===================== Bridge helper (kept for credentials display) =====================

function BridgeRow({ label, value, onCopy, copied }: { label: string; value: string | number; onCopy?: () => void; copied?: boolean }) {
  return (
    <div className="bridge-row">
      <span className="bridge-label">{label}</span>
      <span className="bridge-value mono">{value}</span>
      {onCopy && <button className="btn-ghost btn-tiny" onClick={onCopy}>{copied ? 'copied' : 'copy'}</button>}
    </div>
  )
}

// ===================== Reusable =====================

function Card({ title, children, header }: { title?: string; children: React.ReactNode; header?: React.ReactNode }) {
  return (
    <section className="card">
      {(title || header) && (
        <div className="card-head">
          {title && <h2>{title}</h2>}
          {header}
        </div>
      )}
      {children}
    </section>
  )
}

function Field({ label, children, hint, grow }: { label: string; children: React.ReactNode; hint?: string; grow?: number }) {
  return (
    <div className="field" style={grow !== undefined ? { flex: grow } : undefined}>
      {label && <label>{label}{hint && <span className="hint"> · {hint}</span>}</label>}
      {children}
    </div>
  )
}

function Modal({ title, children, onClose }: { title: string; children: React.ReactNode; onClose: () => void }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', onKey)
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = ''
    }
  }, [onClose])
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <h3>{title}</h3>
          <button className="modal-close" onClick={onClose} aria-label="close">×</button>
        </div>
        <div className="modal-body">{children}</div>
      </div>
    </div>
  )
}

// ===================== Settings =====================

function SettingsTab() {
  const [settings, setSettings] = useState<any | null>(null)
  const [devices, setDevices] = useState<any[]>([])
  const [devOverrides, setDevOverrides] = useState<Record<string, any>>({})
  const [keys, setKeys] = useState<any[]>([])
  const [newKeyName, setNewKeyName] = useState('')
  const [createdKey, setCreatedKey] = useState<any | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  const load = async () => {
    try {
      const [s, ds, ks] = await Promise.all([api.getSettings(), api.listDevices(), api.listAPIKeys()])
      setSettings(s); setDevices(ds || []); setKeys(ks || [])
      const overrides: Record<string, any> = {}
      await Promise.all((ds || []).map(async (d: any) => {
        try { overrides[d.deviceID] = await api.getDeviceRequireQR(d.deviceID) } catch {}
      }))
      setDevOverrides(overrides)
    } catch (e: any) { setErr(String(e)) }
  }
  useEffect(() => { load() }, [])

  const [applyMsg, setApplyMsg] = useState('')

  const save = async () => {
    if (!settings) return
    setBusy(true); setErr(''); setApplyMsg('')
    try {
      const resp: any = await api.saveSettings(settings)
      // Backend now returns { settings, applied? }. Stay backwards-compatible.
      const next = resp?.settings || resp
      setSettings(next)
      if (Array.isArray(resp?.applied) && resp.applied.length > 0) {
        const total = resp.applied.reduce((a: number, r: any) => a + (r.updated || 0), 0)
        setApplyMsg(`Policy saved. Pushed verify mode to ${resp.applied.length} device(s), updating ${total} user record(s).`)
      } else {
        setApplyMsg('Policy saved. (Toggle unchanged → no device sync needed.)')
      }
      load()
    } catch (e: any) { setErr(String(e)) } finally { setBusy(false) }
  }

  const setDeviceOverride = async (deviceId: string, value: boolean | null) => {
    setApplyMsg('')
    try {
      const r: any = await api.setDeviceRequireQR(deviceId, value)
      if (typeof r?.appliedToUsers === 'number') {
        setApplyMsg(`${deviceId}: pushed ${r.appliedMode} to ${r.appliedToUsers} user(s).`)
      }
      load()
    } catch (e: any) { setErr(String(e)) }
  }

  const applyOne = async (deviceId: string) => {
    setApplyMsg('')
    try {
      const r: any = await api.applyDeviceMode(deviceId)
      setApplyMsg(`${deviceId}: re-applied ${r.mode} to ${r.users} user(s).`)
    } catch (e: any) { setErr(String(e)) }
  }
  const applyAll = async () => {
    setApplyMsg('')
    setBusy(true)
    try {
      const r: any = await api.applyAllDeviceModes()
      const total = (r?.results || []).reduce((a: number, x: any) => a + (x.updated || 0), 0)
      setApplyMsg(`Re-applied mode to ${(r?.results || []).length} device(s), ${total} user record(s) updated.`)
    } catch (e: any) { setErr(String(e)) } finally { setBusy(false) }
  }

  const createKey = async () => {
    setBusy(true); setErr('')
    try {
      const k = await api.createAPIKey(newKeyName || 'untitled')
      setCreatedKey(k); setNewKeyName(''); load()
    } catch (e: any) { setErr(String(e)) } finally { setBusy(false) }
  }
  const deleteKey = async (id: string) => {
    if (!confirm(`Delete API key ${id}? Calls using it will start failing.`)) return
    await api.deleteAPIKey(id); load()
  }

  if (!settings) return <div className="empty">Loading settings…</div>

  return (
    <>
      <div className="page-toolbar">
        <h1 className="page-title">Settings</h1>
      </div>
      {err && <div className="err">{err}</div>}

      <Card title="Authentication policy">
        <div className="muted small" style={{ marginBottom: 12, lineHeight: 1.5 }}>
          <strong>How it works:</strong><br />
          <strong>QR required (ON)</strong> — every user is parked in <code>cardAndPw</code> mode on the device, which is unsatisfiable, so face alone does nothing. The system briefly flips the user to <code>face</code> mode only after a QR scan identifies them.<br />
          <strong>Face only (OFF)</strong> — every user sits in <code>face</code> mode permanently; walking up and showing a face unlocks the door without any QR.
          <br /><br />
          Saving this card pushes the new mode to every device that follows the global setting. Per-device overrides below are unaffected.
        </div>
        <Field label="Require QR before face (global default)" hint="When ON, every user must scan a QR before face will work. When OFF, face matching alone unlocks the door.">
          <label className="toggle">
            <input type="checkbox" checked={!!settings.requireQR2FA} onChange={(e) => setSettings({ ...settings, requireQR2FA: e.target.checked })} />
            <span>{settings.requireQR2FA ? 'QR required' : 'Face only'}</span>
          </label>
        </Field>
        <Field label="Face-auth window (seconds)" hint="How long the device stays in face-verify mode after a session opens. 5–60 typical.">
          <input
            type="number"
            min={3} max={120}
            value={settings.faceAuthWindowSec || 10}
            onChange={(e) => setSettings({ ...settings, faceAuthWindowSec: parseInt(e.target.value || '10', 10) })}
          />
        </Field>
        <Field label="Public /api/v1 enabled" hint="Master kill-switch for third-party callers.">
          <label className="toggle">
            <input type="checkbox" checked={!!settings.publicApiEnabled} onChange={(e) => setSettings({ ...settings, publicApiEnabled: e.target.checked })} />
            <span>{settings.publicApiEnabled ? 'enabled' : 'disabled'}</span>
          </label>
        </Field>
        <div className="form-actions" style={{ gap: 8 }}>
          <button className="btn-primary" disabled={busy} onClick={save}>{busy ? 'Saving…' : 'Save policy'}</button>
          <button className="btn-ghost" disabled={busy} onClick={applyAll} title="Re-push current verify mode to every device, without changing the toggle">Re-apply to all devices</button>
        </div>
        {applyMsg && <div className="muted small" style={{ marginTop: 8 }}>{applyMsg}</div>}
      </Card>

      <Card title="Per-device overrides">
        <div className="muted small" style={{ marginBottom: 12 }}>
          Per-device override wins over the global default. Choose "Inherit" to follow the global setting.
        </div>
        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr>
                <th>Device</th>
                <th>Effective</th>
                <th className="ta-right">Override</th>
              </tr>
            </thead>
            <tbody>
              {devices.map((d) => {
                const ov = devOverrides[d.deviceID] || {}
                const current = ov.override === undefined || ov.override === null ? 'inherit' : (ov.override ? 'require' : 'face-only')
                return (
                  <tr key={d.deviceID}>
                    <td>
                      <div className="cell-stack">
                        <strong>{d.name || d.deviceID}</strong>
                        <span className="muted mono small">{d.deviceID}</span>
                      </div>
                    </td>
                    <td>
                      <span className={ov.effectiveRequireQR ? 'badge ok' : 'badge'}>
                        {ov.effectiveRequireQR ? 'QR required' : 'Face only'}
                      </span>
                    </td>
                    <td className="ta-right">
                      <div style={{ display: 'inline-flex', gap: 6, alignItems: 'center' }}>
                        <select value={current} onChange={(e) => {
                          const v = e.target.value
                          setDeviceOverride(d.deviceID, v === 'inherit' ? null : (v === 'require'))
                        }}>
                          <option value="inherit">Inherit global</option>
                          <option value="require">Force require QR</option>
                          <option value="face-only">Force face-only</option>
                        </select>
                        <button className="btn-ghost" onClick={() => applyOne(d.deviceID)} title="Re-push the effective mode to this device now">Apply</button>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </Card>

      <Card title="API keys (for /api/v1/* third-party callers)">
        <div className="form-row" style={{ marginBottom: 12 }}>
          <input
            placeholder="Key name (e.g. lobby kiosk, ERP integration)"
            value={newKeyName}
            onChange={(e) => setNewKeyName(e.target.value)}
            style={{ flex: 1 }}
          />
          <button className="btn-primary" disabled={busy} onClick={createKey}>Create key</button>
        </div>
        {createdKey && (
          <div className="result" style={{ background: 'var(--ok-bg, #14361f)', color: 'var(--ok, #6cffa6)', padding: 12, marginBottom: 12 }}>
            <strong>New key — save this now, it will not be shown again:</strong>
            <pre className="mono small" style={{ marginTop: 6, whiteSpace: 'pre-wrap' }}>{createdKey.key}</pre>
          </div>
        )}
        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>ID</th>
                <th>Last used</th>
                <th>Created</th>
                <th className="ta-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {keys.length === 0 ? (
                <tr><td colSpan={5}><div className="empty" style={{ padding: 12 }}>No API keys yet.</div></td></tr>
              ) : keys.map((k: any) => (
                <tr key={k.id}>
                  <td>{k.name || <span className="muted">—</span>}</td>
                  <td><span className="mono small">{k.id}</span></td>
                  <td>{k.lastUsedAt ? new Date(k.lastUsedAt).toLocaleString() : <span className="muted">never</span>}</td>
                  <td>{new Date(k.createdAt).toLocaleString()}</td>
                  <td className="ta-right">
                    <button className="btn-ghost danger" onClick={() => deleteKey(k.id)}>Delete</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
    </>
  )
}

// ===================== Test =====================

function TestTab() {
  const [devices, setDevices] = useState<any[]>([])
  const [persons, setPersons] = useState<any[]>([])
  const [keys, setKeys] = useState<any[]>([])
  const [apiKey, setApiKey] = useState<string>(() => localStorage.getItem('face_auth.testApiKey') || '')
  const [deviceId, setDeviceId] = useState('')
  const [identifierType, setIdentifierType] = useState<'none' | 'personId' | 'employeeNo' | 'qrToken'>('none')
  const [identifierValue, setIdentifierValue] = useState('')
  const [session, setSession] = useState<any | null>(null)
  const [busy, setBusy] = useState(false)
  const [log, setLog] = useState<string[]>([])
  const pollRef = useRef<number | null>(null)

  useEffect(() => {
    api.listDevices().then((d) => setDevices(d || [])).catch(() => {})
    api.listPersons().then((p) => setPersons(p || [])).catch(() => {})
    api.listAPIKeys().then((k) => setKeys(k || [])).catch(() => {})
  }, [])
  useEffect(() => { localStorage.setItem('face_auth.testApiKey', apiKey) }, [apiKey])

  const append = (m: string) => setLog((prev) => [`${new Date().toLocaleTimeString()} · ${m}`, ...prev].slice(0, 200))

  const start = async () => {
    if (!apiKey) { append('ERROR: paste an API key first (create one in Settings)'); return }
    if (!deviceId) { append('ERROR: pick a device'); return }
    setBusy(true)
    const body: any = { deviceId }
    if (identifierType !== 'none' && identifierValue) body[identifierType] = identifierValue
    append(`POST /api/v1/auth/face/start ${JSON.stringify(body)}`)
    try {
      const s = await api.startFaceAuth(apiKey, body)
      setSession(s)
      append(`session ${s.id} opened mode=${s.mode} expires=${s.expiresAt}`)
      poll(s.id)
    } catch (e: any) { append(`ERROR: ${String(e)}`) } finally { setBusy(false) }
  }

  const poll = (id: string) => {
    if (pollRef.current) window.clearInterval(pollRef.current)
    pollRef.current = window.setInterval(async () => {
      try {
        const s = await api.getFaceAuthSession(apiKey, id)
        setSession(s)
        if (s.status !== 'open') {
          append(`session ${id} → ${s.status}${s.matchedEmployeeNo ? ` matched=${s.matchedEmployeeNo}` : ''}`)
          if (pollRef.current) window.clearInterval(pollRef.current)
        }
      } catch (e: any) {
        append(`poll ERROR: ${String(e)}`)
        if (pollRef.current) window.clearInterval(pollRef.current)
      }
    }, 1000) as any
  }
  useEffect(() => () => { if (pollRef.current) window.clearInterval(pollRef.current) }, [])

  const cancel = async () => {
    if (!session) return
    try { await api.cancelFaceAuthSession(apiKey, session.id); append(`cancel sent for ${session.id}`) } catch (e: any) { append(String(e)) }
  }

  const device = devices.find((d) => d.deviceID === deviceId)

  return (
    <>
      <div className="page-toolbar"><h1 className="page-title">Test face auth</h1></div>
      <Card title="Configuration">
        <Field label="API key" hint="Created in Settings → API keys. Stored in this browser only.">
          {keys.length > 0 && (
            <select value="" onChange={(e) => { if (e.target.value) setApiKey(e.target.value) }} style={{ marginBottom: 6 }}>
              <option value="">— pick a saved key (you'll need the secret) —</option>
              {keys.map((k: any) => <option key={k.id} value={k.id}>{k.name || k.id}</option>)}
            </select>
          )}
          <input type="password" placeholder="fa_xxxxxxxx" value={apiKey} onChange={(e) => setApiKey(e.target.value)} />
        </Field>
        <Field label="Device">
          <select value={deviceId} onChange={(e) => setDeviceId(e.target.value)}>
            <option value="">— pick a device —</option>
            {devices.map((d: any) => (
              <option key={d.deviceID} value={d.deviceID}>{d.name || d.deviceID} {d.online ? '· online' : '· offline'}</option>
            ))}
          </select>
        </Field>
        <Field label="Identify user by" hint="Leave on 'none' to allow any enrolled face (only works if device is face-only).">
          <select value={identifierType} onChange={(e) => setIdentifierType(e.target.value as any)}>
            <option value="none">None (face-any)</option>
            <option value="personId">Person ID</option>
            <option value="employeeNo">Employee No</option>
            <option value="qrToken">QR token</option>
          </select>
        </Field>
        {identifierType === 'personId' && (
          <Field label="Person">
            <select value={identifierValue} onChange={(e) => setIdentifierValue(e.target.value)}>
              <option value="">— pick a person —</option>
              {persons.map((p: any) => <option key={p.id} value={p.id}>{p.name} ({p.employeeNo})</option>)}
            </select>
          </Field>
        )}
        {identifierType === 'employeeNo' && (
          <Field label="Employee No">
            <input value={identifierValue} onChange={(e) => setIdentifierValue(e.target.value)} placeholder="e.g. 1042" />
          </Field>
        )}
        {identifierType === 'qrToken' && (
          <Field label="QR token">
            <input value={identifierValue} onChange={(e) => setIdentifierValue(e.target.value)} placeholder="paste raw QR text" />
          </Field>
        )}
        <div className="form-actions">
          <button className="btn-primary" disabled={busy} onClick={start}>Start face auth</button>
          {session && session.status === 'open' && (
            <button className="btn-ghost" onClick={cancel}>Cancel session</button>
          )}
        </div>
      </Card>

      {device && (
        <Card title="Live camera">
          <div className="live-frame" style={{ minHeight: 320 }}>
            <img className="snap-img" alt="" src={api.mjpegUrl(device.deviceID, 6)} />
          </div>
          <div className="muted small" style={{ marginTop: 6 }}>Present a face to the camera while the session is open.</div>
        </Card>
      )}

      {session && (
        <Card title={`Session ${session.id}`}>
          <pre className="result">{JSON.stringify(session, null, 2)}</pre>
        </Card>
      )}

      <Card title="Log">
        <pre className="result" style={{ maxHeight: 240, overflow: 'auto' }}>{log.join('\n') || '(empty)'}</pre>
      </Card>
    </>
  )
}

// ===================== Setup Guide =====================

function GuideTab() {
  const [step, setStep] = useState<number>(() => parseInt(localStorage.getItem('face_auth.guideStep') || '0', 10))
  const [downloads, setDownloads] = useState<any[]>([])
  const [status, setStatus] = useState<any | null>(null)
  const [devices, setDevices] = useState<any[]>([])
  const [agents, setAgents] = useState<any[]>([])

  useEffect(() => {
    api.agentDownloads().then((d) => setDownloads(Array.isArray(d) ? d : [])).catch(() => setDownloads([]))
    api.status().then(setStatus).catch(() => {})
    api.listDevices().then((d) => setDevices(d || [])).catch(() => {})
    api.listAgents().then((a) => setAgents(a || [])).catch(() => {})
  }, [])
  useEffect(() => { localStorage.setItem('face_auth.guideStep', String(step)) }, [step])

  const steps: { id: string; title: string; render: () => React.ReactNode }[] = [
    {
      id: 'overview',
      title: '1. Overview — how face_auth fits together',
      render: () => (
        <>
          <p>face_auth is a bridge between your Hikvision face-recognition cameras and any third-party software (kiosks, POS, ERP, attendance). It does three things:</p>
          <ul>
            <li><strong>Stores enrolled people + faces</strong> centrally so you push faces to one device, copy to many.</li>
            <li><strong>Watches device events</strong> over HTTP alarm-host push so face matches show up live.</li>
            <li><strong>Exposes a public API</strong> under <code>/api/v1/*</code> so third-party software can trigger face auth, listen for matches, open doors.</li>
          </ul>
          <h4>Two operating modes</h4>
          <ul>
            <li><strong>QR + Face (2FA)</strong> — user scans a QR (lanyard or app), system briefly unlocks face mode, user shows face.</li>
            <li><strong>Face only</strong> — device is always armed; face match alone authenticates.</li>
          </ul>
          <p>Toggle in <em>Settings → Authentication policy</em>; per-device overrides also live there.</p>
          <h4>You'll do these steps</h4>
          <ol>
            <li>Add a Hikvision device.</li>
            <li>(Optional) Install an on-prem agent to reach the device through NAT.</li>
            <li>(Optional, for QR-2FA) Install AutoHotkey on the kiosk PC so the USB QR scanner works even when idle.</li>
            <li>Enrol people + faces.</li>
            <li>Create an API key and integrate third-party software.</li>
          </ol>
        </>
      ),
    },
    {
      id: 'device',
      title: '2. Add your Hikvision device',
      render: () => (
        <>
          <p>You'll need the device's LAN IP, ISAPI port (usually 80), and admin username + password from the device's web UI.</p>
          <ol>
            <li>Open <strong>Devices → Add device</strong>.</li>
            <li>Enter a unique device ID (e.g. <code>lobby-1</code>), the IP, ISAPI credentials.</li>
            <li>If face_auth runs on the same LAN as the device, pick <em>Direct ISAPI</em>. Otherwise leave the agent blank for now — you'll attach one in step 3.</li>
            <li>Click <strong>Save &amp; probe</strong>. Probe should show <code>reachable: true</code>.</li>
            <li>Once it's online, click <strong>Events</strong> on the device row to register face_auth as the alarm host. Face matches will then stream live.</li>
          </ol>
          <Card title="Your devices">
            {devices.length === 0 ? <div className="empty">No devices yet.</div> : (
              <div className="table-wrap">
                <table className="data-table">
                  <thead><tr><th>Device</th><th>Address</th><th>Status</th></tr></thead>
                  <tbody>
                    {devices.map((d) => (
                      <tr key={d.deviceID}>
                        <td><strong>{d.name || d.deviceID}</strong></td>
                        <td><span className="mono small">{d.ip}:{d.port}</span></td>
                        <td>{d.online ? <span className="badge ok">online</span> : <span className="badge">offline</span>}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </>
      ),
    },
    {
      id: 'agent',
      title: '3. (Optional) Install the on-prem agent',
      render: () => (
        <>
          <p>The agent is a tiny binary that runs on any always-on machine on the same LAN as your camera. It opens a WebSocket to face_auth and tunnels ISAPI calls + QR scans through it — no port forwarding needed.</p>
          <p><strong>Skip this step if</strong> face_auth itself is on the same LAN as the camera.</p>
          <h4>Steps</h4>
          <ol>
            <li>Open <strong>Agents → Add agent</strong>. Pick an ID (e.g. <code>lobby-agent</code>). Copy the token — it's shown only once.</li>
            <li>Download the binary for your machine's OS below.</li>
            <li>Run on the LAN machine:
              <pre className="result mono small" style={{ marginTop: 6 }}>{`# Windows PowerShell:
$env:CLOUD_URL  = "https://face_auth.example.com"
$env:AGENT_ID   = "lobby-agent"
$env:AGENT_TOKEN= "paste-token"
.\\face_auth-agent-windows-amd64.exe

# Linux / macOS:
CLOUD_URL=https://face_auth.example.com \\
AGENT_ID=lobby-agent \\
AGENT_TOKEN=paste-token \\
./face_auth-agent-linux-amd64`}</pre>
            </li>
            <li>Back in <strong>Agents</strong>, the agent flips to <code>online</code> within ~5s.</li>
            <li>Edit your device and set <em>Reach via</em> to this agent. Probe should still pass.</li>
            <li>For production: install as a service.
              <ul>
                <li><strong>Windows</strong>: <code>nssm install face_auth-agent C:\\path\\to\\face_auth-agent.exe</code></li>
                <li><strong>Linux</strong>: drop a systemd unit (sample in agent ZIP).</li>
              </ul>
            </li>
          </ol>
          <Card title="Download the agent">
            {downloads.length === 0 ? (
              <div className="empty">No agent binaries available on this server.</div>
            ) : (
              <div className="download-grid">
                {downloads.map((d) => (
                  <a key={d.file} className="download-tile" href={api.agentDownloadUrl(d.file)} download>
                    <strong>{d.label}</strong>
                    <span className="muted mono small">{d.file}</span>
                    <span className="muted small">{Math.round(d.size / (1024 * 1024) * 10) / 10} MB</span>
                  </a>
                ))}
              </div>
            )}
          </Card>
          <Card title="Your agents">
            {agents.length === 0 ? <div className="empty">No agents yet.</div> : (
              <div className="table-wrap">
                <table className="data-table">
                  <thead><tr><th>Agent</th><th>Status</th></tr></thead>
                  <tbody>
                    {agents.map((a: any) => (
                      <tr key={a.id}>
                        <td><strong>{a.name || a.id}</strong> <span className="mono small muted">· {a.id}</span></td>
                        <td>{a.online ? <span className="badge ok">online</span> : <span className="badge">offline</span>}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </>
      ),
    },
    {
      id: 'qr-scanner',
      title: '4. (QR-2FA only) Install the AutoHotkey QR watcher',
      render: () => (
        <>
          <p>For QR + Face mode you need a way to capture USB QR scans even when the kiosk's foreground app is idle or showing a screensaver. We use AutoHotkey because it can listen globally to USB-keyboard-style scanners without stealing keystrokes from the user.</p>
          <h4>One-time setup on the kiosk PC</h4>
          <ol>
            <li>Install AutoHotkey v2: <a href="https://www.autohotkey.com/" target="_blank" rel="noreferrer">autohotkey.com</a> → Download v2 → run installer.</li>
            <li>Download the watcher: <a href={api.agentScriptUrl('qr-watcher.ahk')} download><code>qr-watcher.ahk</code></a>.</li>
            <li>Open it in Notepad. Check the <code>AGENT_URL</code> line near the top — default <code>http://127.0.0.1:7771/scan</code> works when the agent runs on the same machine. Change it if the agent is elsewhere.</li>
            <li>Double-click <code>qr-watcher.ahk</code> to run. A tray icon appears.</li>
            <li>Auto-start on boot: <kbd>Win+R</kbd> → <code>shell:startup</code> → drop a shortcut to the script in there.</li>
            <li>Test: scan any QR (or right-click tray → <em>Send a test scan</em>). Tray tooltip counts up.</li>
          </ol>
          <h4>Why AutoHotkey vs. the agent's built-in keyboard listener?</h4>
          <p>The agent's keyboard mode (Linux <code>/dev/input/*</code> or Windows raw HID) needs the agent process to own the keyboard. AutoHotkey v2's <code>InputHook</code> works <strong>system-wide and non-intrusively</strong> — your normal typing keeps working everywhere; only strings arriving faster than human typing are forwarded as scans. That's what makes a kiosk usable even when idle.</p>
          <Card title="Downloads">
            <div className="download-grid">
              <a className="download-tile" href="https://www.autohotkey.com/" target="_blank" rel="noreferrer">
                <strong>AutoHotkey v2</strong>
                <span className="muted small">autohotkey.com</span>
              </a>
              <a className="download-tile" href={api.agentScriptUrl('qr-watcher.ahk')} download>
                <strong>qr-watcher.ahk</strong>
                <span className="muted small">QR forwarder script</span>
              </a>
            </div>
          </Card>
        </>
      ),
    },
    {
      id: 'enrol',
      title: '5. Enrol people and faces',
      render: () => (
        <>
          <p>Two ways to populate the system:</p>
          <h4>Option A — Pull existing faces from the device</h4>
          <ol>
            <li>Go to <strong>Persons</strong>, pick the device filter, click <strong>Sync from device</strong>.</li>
            <li>face_auth pulls users, cards, and face JPEGs into its own database.</li>
          </ol>
          <h4>Option B — Enrol from scratch</h4>
          <ol>
            <li><strong>Persons → Add person</strong>. Fill name + employee number (this is the device's key for the user — short and unique).</li>
            <li><strong>Enrol</strong>: pick device + person, upload a JPEG or capture from the webcam modal.</li>
            <li>The face is pushed to the device with the user record.</li>
          </ol>
          <h4>QR mode — rotate a token per user</h4>
          <ol>
            <li>Open a person via <strong>Persons → ⋯ → View</strong>.</li>
            <li>Click <strong>Rotate QR</strong>. The PNG renders — print or screenshot for the user.</li>
          </ol>
        </>
      ),
    },
    {
      id: 'integrate',
      title: '6. Connect your third-party software',
      render: () => (
        <>
          <p>Anything that can speak HTTP can drive face_auth. Full reference is in the <strong>API Docs</strong> tab.</p>
          <h4>Steps</h4>
          <ol>
            <li><strong>Settings → API keys → Create key</strong>. Save the value — shown once.</li>
            <li>In your software, send <code>X-API-Key: fa_xxx</code> (or <code>Authorization: Bearer fa_xxx</code>) on every <code>/api/v1/*</code> request.</li>
            <li>To trigger face auth, POST <code>/api/v1/auth/face/start</code> with <code>deviceId</code> (and optionally <code>qrToken</code> / <code>personId</code> / <code>employeeNo</code>). Poll <code>GET /api/v1/auth/face/&#123;id&#125;</code> or subscribe to SSE.</li>
            <li>To embed live camera, drop an <code>&lt;img&gt;</code> tag with <code>src="/api/v1/devices/&#123;id&#125;/stream.mjpg"</code>.</li>
          </ol>
          <p>Try it now: head to <strong>Test</strong> and run a session, then open <strong>API Docs</strong> and click <em>Try it</em> on any endpoint.</p>
        </>
      ),
    },
    {
      id: 'done',
      title: "7. You're done",
      render: () => (
        <>
          <p>System is healthy when all of these are true:</p>
          <ul>
            <li>Devices show as <code>online</code>.</li>
            <li>Face matches appear live in <strong>Events</strong>.</li>
            <li>A <strong>Test</strong> session flips to <code>face_matched</code>.</li>
            <li><strong>API Docs → /api/v1/devices → Try it</strong> returns your devices with the API key.</li>
          </ul>
          <p>Hand the API key + base URL to your third-party developer and you're shipped.</p>
          <p>Diagnostics: <strong>QR Auth</strong> (session history), <strong>Events</strong> (raw payloads), <strong>Agents → Bridge log</strong>, <strong>ISAPI</strong> (raw calls).</p>
        </>
      ),
    },
  ]

  const goto = (n: number) => setStep(Math.max(0, Math.min(steps.length - 1, n)))
  const currentStatus = status ? (status.devicesOnline > 0 ? 'devices reachable' : 'no devices online yet') : 'unknown'

  return (
    <>
      <div className="page-toolbar">
        <h1 className="page-title">Setup guide</h1>
        <div className="muted small">System status: {currentStatus}</div>
      </div>

      <div className="step-nav" style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginBottom: 12 }}>
        {steps.map((s, i) => (
          <button
            key={s.id}
            className={`step-pill ${i === step ? 'active' : ''} ${i < step ? 'done' : ''}`}
            onClick={() => goto(i)}
            style={{
              padding: '6px 10px',
              borderRadius: 6,
              border: '1px solid var(--border, #2a2a2a)',
              background: i === step ? 'var(--accent, #3b82f6)' : 'transparent',
              color: i === step ? '#fff' : 'inherit',
              cursor: 'pointer',
              fontSize: 13,
            }}
          >
            {i + 1}
          </button>
        ))}
      </div>

      <Card>
        <h2 style={{ marginTop: 0 }}>{steps[step].title}</h2>
        <div className="step-body">{steps[step].render()}</div>
        <div className="form-actions" style={{ marginTop: 16 }}>
          <button className="btn-ghost" disabled={step === 0} onClick={() => goto(step - 1)}>← Previous</button>
          <button className="btn-primary" disabled={step >= steps.length - 1} onClick={() => goto(step + 1)}>Next →</button>
        </div>
      </Card>
    </>
  )
}

// ===================== API Docs =====================

type EndpointDef = {
  method: 'GET' | 'POST' | 'PUT' | 'DELETE'
  path: string
  group: string
  summary: string
  description?: string
  auth: 'v1' | 'admin' | 'none'
  params?: { name: string; in: 'path' | 'query' | 'body' | 'form'; type?: string; required?: boolean; description?: string }[]
  bodyExample?: any
  responseType?: 'json' | 'image' | 'mjpeg' | 'sse' | 'text' | 'binary'
  responses?: { status: number; description: string; example?: any }[]
}

const DEFAULT_OK_RESPONSE = (example?: any) => [
  { status: 200, description: 'Success', example },
  { status: 401, description: 'Missing or invalid API key', example: { error: 'missing api key' } },
  { status: 500, description: 'Internal error', example: { error: 'reason…' } },
]

const ENDPOINTS: EndpointDef[] = [
  // v1 PUBLIC
  {
    method: 'GET', path: '/api/v1/ping', group: 'v1 — Health',
    summary: 'Liveness check',
    description: 'Returns 200 with the current server time. Use this from your monitoring to assert the public API surface is up and your API key is valid.',
    auth: 'v1',
    responses: [
      { status: 200, description: 'OK', example: { ok: true, service: 'face_auth', time: '2026-06-03T08:00:00Z' } },
      { status: 401, description: 'Missing/invalid API key', example: { error: 'missing api key' } },
    ],
  },
  {
    method: 'GET', path: '/api/v1/devices', group: 'v1 — Devices',
    summary: 'List devices (with effective QR mode)',
    description: 'Returns every device registered in face_auth, decorated with its effective `requireQR2FA` setting (per-device override, otherwise global default).',
    auth: 'v1',
    responses: [
      {
        status: 200, description: 'Device list', example: [
          { deviceId: 'lobby-1', name: 'Lobby entry', model: 'DS-K1T804AEF', online: true, requireQR2FA: false, agentId: 'lobby-agent' },
        ]
      },
    ],
  },
  { method: 'POST', path: '/api/v1/devices/:id/probe', group: 'v1 — Devices', summary: 'Probe reachability + update status', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'GET', path: '/api/v1/devices/:id/snapshot', group: 'v1 — Devices', summary: 'Single JPEG frame', auth: 'v1', responseType: 'image', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'GET', path: '/api/v1/devices/:id/stream.mjpg', group: 'v1 — Devices', summary: 'Continuous MJPEG stream', auth: 'v1', responseType: 'mjpeg', params: [{ name: 'id', in: 'path', required: true }, { name: 'fps', in: 'query', type: 'int', description: '1-15, default 4' }, { name: 'seconds', in: 'query', type: 'int', description: 'auto-close after N sec, 0=forever' }] },
  { method: 'POST', path: '/api/v1/devices/:id/open-door', group: 'v1 — Devices', summary: 'Trigger door unlock', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }, { name: 'door', in: 'query', type: 'int', description: 'door number, default 1' }] },
  { method: 'POST', path: '/api/v1/devices/:id/sync-persons', group: 'v1 — Devices', summary: 'Pull users from device → local DB', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'GET', path: '/api/v1/devices/:id/face-lib', group: 'v1 — Devices', summary: 'List faces stored on the device', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'POST', path: '/api/v1/devices/:id/isapi', group: 'v1 — Devices', summary: 'Raw ISAPI passthrough (advanced)', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }], bodyExample: { method: 'GET', path: '/ISAPI/System/deviceInfo', body: '' } },

  { method: 'GET', path: '/api/v1/persons', group: 'v1 — Persons', summary: 'List people', auth: 'v1' },
  { method: 'POST', path: '/api/v1/persons', group: 'v1 — Persons', summary: 'Create a person', auth: 'v1', bodyExample: { name: 'Alice', employeeNo: '1001', personType: 'normal', personRole: 'basic' } },
  { method: 'GET', path: '/api/v1/persons/:id', group: 'v1 — Persons', summary: 'Get person + enrolled faces', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'DELETE', path: '/api/v1/persons/:id', group: 'v1 — Persons', summary: 'Delete a person', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }, { name: 'syncDevice', in: 'query', description: 'also delete from device' }] },
  { method: 'POST', path: '/api/v1/persons/:id/qr/rotate', group: 'v1 — Persons', summary: 'Generate/rotate QR token', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'GET', path: '/api/v1/persons/:id/qr.png', group: 'v1 — Persons', summary: 'Render QR as PNG', auth: 'v1', responseType: 'image', params: [{ name: 'id', in: 'path', required: true }, { name: 'size', in: 'query', type: 'int', description: '96-1024, default 256' }] },

  { method: 'GET', path: '/api/v1/devices/:id/faces', group: 'v1 — Faces', summary: 'List enrolled faces (local DB)', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }, { name: 'personId', in: 'query' }] },
  { method: 'POST', path: '/api/v1/devices/:id/faces', group: 'v1 — Faces', summary: 'Enrol a face (multipart upload)', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }, { name: 'file', in: 'form', required: true, description: 'JPEG' }, { name: 'personId', in: 'form' }, { name: 'name', in: 'form' }, { name: 'employeeNo', in: 'form' }] },
  { method: 'DELETE', path: '/api/v1/devices/:id/faces/:personId', group: 'v1 — Faces', summary: 'Delete a face from device', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }, { name: 'personId', in: 'path', required: true }] },

  {
    method: 'POST', path: '/api/v1/auth/face/start', group: 'v1 — Face Auth',
    summary: 'Open a face-auth session ★',
    description: 'The main third-party entry point. Behavior depends on the device\'s effective requireQR2FA toggle:\n\n• When the device requires QR (default), you MUST supply one of `qrToken`, `personId`, or `employeeNo` to identify which user is about to authenticate. The system briefly unlocks face mode for that user.\n\n• When the device is face-only, you can omit all three — the device is always armed and the session just waits for ANY enrolled user to present a face.\n\nThe call returns a session record. Poll `GET /api/v1/auth/face/{id}` or subscribe to the SSE stream until `status !== "open"`.',
    auth: 'v1',
    bodyExample: { deviceId: 'lobby-1', personId: 'optional', employeeNo: 'optional', qrToken: 'optional' },
    responses: [
      {
        status: 200, description: 'Session opened', example: {
          id: 'fa-9c3f1b8d04',
          personId: 'p_1', employeeNo: '1001', name: 'Alice',
          deviceId: 'lobby-1',
          openedAt: '2026-06-03T08:00:00Z',
          expiresAt: '2026-06-03T08:00:10Z',
          mode: 'face-only', status: 'open', source: 'api',
        }
      },
      { status: 400, description: 'Bad request (e.g. unknown QR token, missing deviceId)', example: { error: 'unknown QR token' } },
      { status: 404, description: 'Device not found', example: { error: 'device not found' } },
      { status: 409, description: 'Device requires QR — supply identifier', example: { error: 'qr_required', detail: 'this device requires QR scan before face — supply qrToken, personId, or employeeNo' } },
      { status: 503, description: 'Public API disabled by admin', example: { error: 'public api disabled' } },
    ],
  },
  {
    method: 'GET', path: '/api/v1/auth/face/:id', group: 'v1 — Face Auth',
    summary: 'Get session status',
    description: 'Poll for the outcome. `status` is one of:\n\n  • `open` — still waiting for face\n  • `face_matched` — success; `matchedEmployeeNo` is set\n  • `timed_out` — window closed with no match\n  • `cancelled` — aborted by caller or admin',
    auth: 'v1',
    params: [{ name: 'id', in: 'path', required: true, description: 'Session id returned by /start' }],
    responses: [
      {
        status: 200, description: 'Session record', example: {
          id: 'fa-9c3f1b8d04',
          status: 'face_matched', matchedEmployeeNo: '1001',
          deviceId: 'lobby-1', openedAt: '2026-06-03T08:00:00Z', expiresAt: '2026-06-03T08:00:10Z',
        }
      },
      { status: 404, description: 'Session not found (already evicted from history)' },
    ],
  },
  {
    method: 'POST', path: '/api/v1/auth/face/:id/cancel', group: 'v1 — Face Auth',
    summary: 'Cancel an open session',
    description: 'Aborts a session that is still `open`. No-op if the session has already ended.',
    auth: 'v1',
    params: [{ name: 'id', in: 'path', required: true }],
    responses: [{ status: 200, description: 'OK', example: { ok: true } }],
  },
  {
    method: 'GET', path: '/api/v1/auth/face/stream', group: 'v1 — Face Auth',
    summary: 'SSE stream of every face match',
    description: 'Server-Sent Events. Each `face_match` event payload contains `{ deviceId, employeeNo, receivedAt }`. Useful for attendance dashboards or door-open hooks without per-session polling.',
    auth: 'v1', responseType: 'sse',
    responses: [{ status: 200, description: 'Stream (text/event-stream)', example: 'event: face_match\ndata: {"deviceId":"lobby-1","employeeNo":"1001","receivedAt":"2026-06-03T08:00:08Z"}\n\n' }],
  },
  { method: 'POST', path: '/api/v1/qr-auth/scan', group: 'v1 — Face Auth', summary: 'Submit a QR token (third-party agent emulation)', auth: 'v1', bodyExample: { qrToken: 'paste-here', agentId: '' } },

  { method: 'GET', path: '/api/v1/events', group: 'v1 — Events', summary: 'List recent device events', auth: 'v1', params: [{ name: 'limit', in: 'query', type: 'int' }, { name: 'deviceId', in: 'query' }] },
  { method: 'GET', path: '/api/v1/events/stream', group: 'v1 — Events', summary: 'SSE event stream', auth: 'v1', responseType: 'sse' },

  // ADMIN
  { method: 'GET', path: '/api/status', group: 'admin — System', summary: 'System status summary', auth: 'admin' },
  { method: 'GET', path: '/api/settings', group: 'admin — Settings', summary: 'Get global settings', auth: 'admin' },
  { method: 'PUT', path: '/api/settings', group: 'admin — Settings', summary: 'Save global settings', auth: 'admin', bodyExample: { requireQR2FA: true, faceAuthWindowSec: 10, publicApiEnabled: true } },
  { method: 'PUT', path: '/api/devices/:id/require-qr', group: 'admin — Settings', summary: 'Per-device QR override', auth: 'admin', bodyExample: { value: true } },
  { method: 'GET', path: '/api/api-keys', group: 'admin — API Keys', summary: 'List API keys', auth: 'admin' },
  { method: 'POST', path: '/api/api-keys', group: 'admin — API Keys', summary: 'Create API key (returns plaintext once)', auth: 'admin', bodyExample: { name: 'kiosk' } },
  { method: 'DELETE', path: '/api/api-keys/:id', group: 'admin — API Keys', summary: 'Revoke API key', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'GET', path: '/api/devices', group: 'admin — Devices', summary: 'List devices (full)', auth: 'admin' },
  { method: 'POST', path: '/api/devices', group: 'admin — Devices', summary: 'Register a device', auth: 'admin', bodyExample: { deviceId: 'lobby-1', name: 'Lobby', ip: '192.168.1.50', port: 80, isapiUsername: 'admin', isapiPassword: 'pass' } },
  { method: 'DELETE', path: '/api/devices/:id', group: 'admin — Devices', summary: 'Delete a device', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'POST', path: '/api/devices/:id/setup-alarm-host', group: 'admin — Devices', summary: 'Register face_auth as alarm host on device', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'GET', path: '/api/devices/:id/wifi', group: 'admin — Devices', summary: 'Read device Wi-Fi config', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }, { name: 'ifId', in: 'query', required: false }] },
  { method: 'POST', path: '/api/devices/:id/wifi/scan', group: 'admin — Devices', summary: 'Scan for nearby Wi-Fi access points', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }, { name: 'ifId', in: 'query', required: false }] },
  { method: 'PUT', path: '/api/devices/:id/wifi', group: 'admin — Devices', summary: 'Join a Wi-Fi network', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }], bodyExample: { ssid: 'OfficeWiFi', password: 'secret123', securityMode: 'WPA2-personal' } },
  { method: 'GET', path: '/api/agents', group: 'admin — Agents', summary: 'List agents', auth: 'admin' },
  { method: 'POST', path: '/api/agents', group: 'admin — Agents', summary: 'Register agent (token shown once)', auth: 'admin', bodyExample: { id: 'lobby-agent', name: 'Lobby Agent' } },
  { method: 'DELETE', path: '/api/agents/:id', group: 'admin — Agents', summary: 'Delete an agent', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'POST', path: '/api/agents/:id/regen-token', group: 'admin — Agents', summary: 'Rotate agent token', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'GET', path: '/api/agents/downloads', group: 'admin — Agents', summary: 'List agent binaries (public — no auth)', auth: 'none' },
  { method: 'GET', path: '/api/agents/downloads/:file', group: 'admin — Agents', summary: 'Download an agent binary (public — no auth)', auth: 'none', responseType: 'binary', params: [{ name: 'file', in: 'path', required: true }] },
  { method: 'GET', path: '/api/qr-auth/sessions', group: 'admin — QR Auth', summary: 'Active + historical sessions', auth: 'admin' },
  { method: 'POST', path: '/api/qr-auth/scan', group: 'admin — QR Auth', summary: 'Agent-style QR scan (admin)', auth: 'admin', bodyExample: { qrToken: 'paste-here', agentId: '' } },
  { method: 'POST', path: '/api/devices/:id/lock-all-users', group: 'admin — QR Auth', summary: 'Lock every device user into baseline', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'GET', path: '/api/events', group: 'admin — Events', summary: 'List events', auth: 'admin' },
  { method: 'GET', path: '/api/events/stream', group: 'admin — Events', summary: 'SSE stream', auth: 'admin', responseType: 'sse' },

  // ---- Enrolment: capture-at-device + cards + fingerprints (v1 + admin) ----
  { method: 'POST', path: '/api/v1/devices/:id/capture/face', group: 'v1 — Enrolment', summary: 'Capture a live face at the reader', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }, { name: 'infrared', in: 'query', description: 'true to also grab IR' }] },
  { method: 'POST', path: '/api/v1/devices/:id/capture/card', group: 'v1 — Enrolment', summary: 'Capture a card swipe', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'POST', path: '/api/v1/devices/:id/capture/fingerprint', group: 'v1 — Enrolment', summary: 'Capture a fingerprint', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }, { name: 'finger', in: 'query', type: 'int', description: 'finger no, default 1' }] },
  { method: 'POST', path: '/api/v1/devices/:id/cards', group: 'v1 — Enrolment', summary: 'Bind/modify a card', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }], bodyExample: { employeeNo: '1001', cardNo: '0468229170', cardType: 'normalCard', mode: '' } },
  { method: 'DELETE', path: '/api/v1/devices/:id/cards/:cardNo', group: 'v1 — Enrolment', summary: 'Delete a card', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }, { name: 'cardNo', in: 'path', required: true }] },
  { method: 'POST', path: '/api/v1/devices/:id/fingerprints', group: 'v1 — Enrolment', summary: 'Upload a fingerprint template', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }], bodyExample: { employeeNo: '1001', fingerPrintID: 1, fingerData: '<base64>' } },
  { method: 'DELETE', path: '/api/v1/devices/:id/fingerprints/:employeeNo', group: 'v1 — Enrolment', summary: 'Delete fingerprint(s)', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }, { name: 'employeeNo', in: 'path', required: true }, { name: 'finger', in: 'query', type: 'int', description: 'one print; omit for all' }] },

  // ---- Health & access schedules (v1) ----
  { method: 'GET', path: '/api/v1/devices/:id/work-status', group: 'v1 — Health & Schedules', summary: 'Door / lock / tamper / battery / capacity', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'PUT', path: '/api/v1/devices/:id/week-plan/:planNo', group: 'v1 — Health & Schedules', summary: 'Write per-weekday allow windows', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }, { name: 'planNo', in: 'path', required: true }], bodyExample: { days: [{ week: 'Monday', enable: true, begin: '09:00:00', end: '18:00:00' }] } },
  { method: 'PUT', path: '/api/v1/devices/:id/plan-template/:tplNo', group: 'v1 — Health & Schedules', summary: 'Bind a template to a week plan', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }, { name: 'tplNo', in: 'path', required: true }], bodyExample: { name: 'office-hours', weekPlanNo: 1 } },

  // ---- QR-via-camera + intercom (v1) ----
  { method: 'GET', path: '/api/v1/devices/:id/qr-capability', group: 'v1 — QR & Intercom', summary: 'Does the camera support QR?', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'POST', path: '/api/v1/devices/:id/qr-scan', group: 'v1 — QR & Intercom', summary: 'Enable/disable camera QR scanning', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }], bodyExample: { enable: true } },
  { method: 'GET', path: '/api/v1/devices/:id/intercom/capabilities', group: 'v1 — QR & Intercom', summary: 'VideoIntercom support (isSupportCallSignal)', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'GET', path: '/api/v1/devices/:id/intercom/status', group: 'v1 — QR & Intercom', summary: 'Current call status (idle/ring/onCall)', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'POST', path: '/api/v1/devices/:id/intercom/signal', group: 'v1 — QR & Intercom', summary: 'Drive the call (request/answer/hangUp/…)', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }, { name: 'cmd', in: 'query', description: 'request|answer|hangUp|cancel|reject, default request' }] },

  // ---- Same features on the admin mount (session auth) ----
  { method: 'POST', path: '/api/devices/:id/capture/face', group: 'admin — Enrolment', summary: 'Capture a live face at the reader', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }, { name: 'infrared', in: 'query' }] },
  { method: 'POST', path: '/api/devices/:id/capture/card', group: 'admin — Enrolment', summary: 'Capture a card swipe', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'POST', path: '/api/devices/:id/capture/fingerprint', group: 'admin — Enrolment', summary: 'Capture a fingerprint', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }, { name: 'finger', in: 'query', type: 'int' }] },
  { method: 'POST', path: '/api/devices/:id/cards', group: 'admin — Enrolment', summary: 'Bind/modify a card', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }], bodyExample: { employeeNo: '1001', cardNo: '0468229170' } },
  { method: 'DELETE', path: '/api/devices/:id/cards/:cardNo', group: 'admin — Enrolment', summary: 'Delete a card', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }, { name: 'cardNo', in: 'path', required: true }] },
  { method: 'POST', path: '/api/devices/:id/fingerprints', group: 'admin — Enrolment', summary: 'Upload a fingerprint template', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }], bodyExample: { employeeNo: '1001', fingerPrintID: 1, fingerData: '<base64>' } },
  { method: 'DELETE', path: '/api/devices/:id/fingerprints/:employeeNo', group: 'admin — Enrolment', summary: 'Delete fingerprint(s)', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }, { name: 'employeeNo', in: 'path', required: true }, { name: 'finger', in: 'query', type: 'int' }] },
  { method: 'GET', path: '/api/devices/:id/work-status', group: 'admin — Health & Schedules', summary: 'Device health', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'GET', path: '/api/devices/:id/week-plan/:planNo', group: 'admin — Health & Schedules', summary: 'Read a week plan', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }, { name: 'planNo', in: 'path', required: true }] },
  { method: 'PUT', path: '/api/devices/:id/week-plan/:planNo', group: 'admin — Health & Schedules', summary: 'Write per-weekday allow windows', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }, { name: 'planNo', in: 'path', required: true }], bodyExample: { days: [{ week: 'Monday', enable: true, begin: '09:00:00', end: '18:00:00' }] } },
  { method: 'PUT', path: '/api/devices/:id/plan-template/:tplNo', group: 'admin — Health & Schedules', summary: 'Bind a template to a week plan', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }, { name: 'tplNo', in: 'path', required: true }], bodyExample: { name: 'office-hours', weekPlanNo: 1 } },
  { method: 'GET', path: '/api/devices/:id/qr-capability', group: 'admin — QR & Intercom', summary: 'Does the camera support QR?', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'POST', path: '/api/devices/:id/qr-scan', group: 'admin — QR & Intercom', summary: 'Enable/disable camera QR scanning', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }], bodyExample: { enable: true } },
  { method: 'GET', path: '/api/devices/:id/intercom/capabilities', group: 'admin — QR & Intercom', summary: 'VideoIntercom support (isSupportCallSignal)', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'GET', path: '/api/devices/:id/intercom/status', group: 'admin — QR & Intercom', summary: 'Current call status', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'POST', path: '/api/devices/:id/intercom/signal', group: 'admin — QR & Intercom', summary: 'Drive the call (request/answer/hangUp/…)', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }, { name: 'cmd', in: 'query', description: 'request|answer|hangUp|cancel|reject' }] },
]

const METHOD_COLOR: Record<string, string> = { GET: '#3b82f6', POST: '#10b981', PUT: '#f59e0b', DELETE: '#ef4444' }
const slug = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')

function ApiDocsTab() { return <ApiDocsView showHeader /> }

export function ApiDocsStandalone() {
  return (
    <div className="docs-app" style={{ background: 'var(--bg, #0c0c0e)', minHeight: '100vh', color: 'var(--fg, #e6e6e6)' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '14px 24px', borderBottom: '1px solid var(--border, #2a2a2a)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <span className="logo-dot" />
          <strong style={{ fontSize: 18 }}>face_auth — API Reference</strong>
        </div>
        <a href="/" style={{ color: 'inherit', opacity: 0.7, fontSize: 13 }}>← Back to dashboard</a>
      </div>
      <ApiDocsView />
    </div>
  )
}

// Build a single paste-ready text spec of the whole API — drop it into an LLM
// (Claude/ChatGPT/etc.) and ask it to write the integration for you.
function buildLlmSpec(origin: string): string {
  const L: string[] = []
  L.push('# face_auth HTTP API — integration spec')
  L.push('')
  L.push(`Base URL: ${origin || 'https://YOUR-SERVER'}`)
  L.push('')
  L.push('Two surfaces:')
  L.push('- `/api/v1/*` — third-party API. Auth: send your API key as header `Authorization: Bearer fa_…` or `X-API-Key: fa_…` (or `?apiKey=` in a pinch). Create keys in Settings → API keys.')
  L.push('- `/api/*` — admin API behind the dashboard (same-origin session). A few agent-download routes are public (no auth).')
  L.push('')
  L.push('Notes for integration:')
  L.push('- Device-command endpoints return the device\'s raw response in `response` or `raw`; exact JSON shapes depend on the Hikvision firmware.')
  L.push('- Endpoints work over any reach mode (direct / agent / OTAP). Binary pulls (snapshot/MJPEG) need direct or agent reach.')
  L.push('- The headline flow is: POST `/api/v1/auth/face/start` to arm a short face-auth window, then poll GET `/api/v1/auth/face/{id}` (or subscribe to `/auth/face/stream`).')
  L.push('')
  const groups: Record<string, EndpointDef[]> = {}
  ENDPOINTS.forEach((e) => { (groups[e.group] = groups[e.group] || []).push(e) })
  for (const [group, items] of Object.entries(groups)) {
    L.push(`## ${group}`)
    for (const e of items) {
      L.push('')
      L.push(`### ${e.method} ${e.path}`)
      L.push(`${e.summary} (auth: ${e.auth})`)
      if (e.params && e.params.length) {
        L.push('Params:')
        e.params.forEach((p) => L.push(`- ${p.name} (${p.in}${p.required ? ', required' : ''}${p.type ? ', ' + p.type : ''})${p.description ? ': ' + p.description : ''}`))
      }
      if (e.bodyExample !== undefined) L.push('Body JSON: ' + JSON.stringify(e.bodyExample))
      if (e.responseType && e.responseType !== 'json') L.push(`Response: ${e.responseType}`)
    }
    L.push('')
  }
  return L.join('\n')
}

function ApiDocsView({ showHeader = false }: { showHeader?: boolean }) {
  const [filter, setFilter] = useState('')
  const [groupFilter, setGroupFilter] = useState<'all' | 'v1' | 'admin'>('all')
  const [apiKey, setApiKey] = useState<string>(() => localStorage.getItem('face_auth.testApiKey') || '')
  useEffect(() => { localStorage.setItem('face_auth.testApiKey', apiKey) }, [apiKey])

  const filtered = useMemo(() => {
    const f = filter.trim().toLowerCase()
    return ENDPOINTS.filter((e) => {
      if (groupFilter !== 'all' && e.auth !== groupFilter) return false
      if (!f) return true
      return (
        e.path.toLowerCase().includes(f) ||
        e.summary.toLowerCase().includes(f) ||
        e.group.toLowerCase().includes(f) ||
        e.method.toLowerCase().includes(f)
      )
    })
  }, [filter, groupFilter])

  const grouped = useMemo(() => {
    const m: Record<string, EndpointDef[]> = {}
    filtered.forEach((e) => { (m[e.group] = m[e.group] || []).push(e) })
    return m
  }, [filtered])

  const origin = typeof location !== 'undefined' ? location.origin : ''
  const standaloneUrl = origin + '/docs'

  const [copied, setCopied] = useState(false)
  const copyForLLM = async () => {
    try {
      await navigator.clipboard.writeText(buildLlmSpec(origin))
      setCopied(true); setTimeout(() => setCopied(false), 1800)
    } catch { /* clipboard blocked — ignore */ }
  }

  return (
    <div className="docs-shell" style={{ display: 'grid', gridTemplateColumns: '260px 1fr', gap: 0, alignItems: 'start' }}>
      <aside className="docs-sidebar" style={{ position: 'sticky', top: 0, maxHeight: '100vh', overflowY: 'auto', borderRight: '1px solid var(--border, #2a2a2a)', padding: '16px 12px' }}>
        <ApiKeyPanel apiKey={apiKey} onApiKey={setApiKey} />
        <input
          className="search"
          placeholder="Filter endpoints…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          style={{ width: '100%', marginBottom: 12 }}
        />
        <select value={groupFilter} onChange={(e) => setGroupFilter(e.target.value as any)} style={{ width: '100%', marginBottom: 16 }}>
          <option value="all">All endpoints ({ENDPOINTS.length})</option>
          <option value="v1">v1 — third-party</option>
          <option value="admin">admin — UI</option>
        </select>
        {Object.entries(grouped).map(([group, items]) => (
          <div key={group} style={{ marginBottom: 14 }}>
            <div className="muted small" style={{ textTransform: 'uppercase', letterSpacing: 0.5, padding: '2px 6px', fontSize: 11, fontWeight: 600 }}>{group}</div>
            {items.map((e) => (
              <a
                key={e.method + e.path}
                href={`#${slug(e.method + '-' + e.path)}`}
                style={{ display: 'flex', gap: 6, alignItems: 'center', padding: '4px 6px', borderRadius: 4, color: 'inherit', textDecoration: 'none', fontSize: 12 }}
              >
                <span style={{ background: METHOD_COLOR[e.method], color: '#fff', padding: '0 6px', borderRadius: 3, fontFamily: 'monospace', fontSize: 10, fontWeight: 700, minWidth: 50, textAlign: 'center' }}>{e.method}</span>
                <span className="mono" style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{e.path.replace(/^\/api\/?(v1\/)?/, '')}</span>
              </a>
            ))}
          </div>
        ))}
      </aside>

      <main className="docs-main" style={{ padding: '24px 32px', maxWidth: 1080 }}>
        {showHeader && (
          <div className="page-toolbar">
            <div className="toolbar-left">
              <h1 className="page-title">API docs <span className="muted">· {ENDPOINTS.length} endpoints</span></h1>
            </div>
            <div className="toolbar-right">
              <button className="btn-primary" onClick={copyForLLM}>{copied ? 'Copied ✓' : 'Copy for LLM'}</button>
              <a className="btn-ghost" href={standaloneUrl} target="_blank" rel="noreferrer">Open standalone view ↗</a>
            </div>
          </div>
        )}

        <section style={{ marginBottom: 32 }}>
          <h2>Introduction</h2>
          <p>face_auth exposes two HTTP surfaces:</p>
          <ul>
            <li><strong><code>/api/v1/*</code></strong> — public, versioned API for third-party software. API key required.</li>
            <li><strong><code>/api/*</code></strong> — admin surface backing this dashboard. Same-origin only.</li>
          </ul>
          <p>Base URL: <code>{origin}</code></p>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginTop: 12, padding: '12px 14px', border: '1px solid var(--border, #2a2a2a)', borderRadius: 8 }}>
            <button className="btn-primary" onClick={copyForLLM}>{copied ? 'Copied ✓' : 'Copy for LLM'}</button>
            <span className="muted small">Copies the whole API (every endpoint, params &amp; body examples) as text — paste into an LLM and ask it to build your integration.</span>
          </div>
        </section>

        <section style={{ marginBottom: 32 }}>
          <h2 id="auth">Authentication</h2>
          <p>
            API keys live in face_auth's database — you cannot use random strings.
            Either click <strong>+ Create new key</strong> in the sidebar on the left,
            or go to the dashboard's <strong>Settings → API keys</strong>. The plaintext
            value is shown <strong>once</strong> on creation; copy it immediately.
          </p>
          <p>Then send it on every <code>/api/v1/*</code> call:</p>
          <pre className="result mono small">{`# Header (recommended)
Authorization: Bearer fa_xxxxxxxxxxxxxxxxxxxxxxxx

# Or custom header
X-API-Key: fa_xxxxxxxxxxxxxxxxxxxxxxxx

# Or query string (avoid for sensitive endpoints — ends up in logs)
?apiKey=fa_xxxxxxxxxxxxxxxxxxxxxxxx`}</pre>
          <Field label="API key (saved locally — used by every Try it below)">
            <input type="password" placeholder="fa_xxxxxxxxxxxx" value={apiKey} onChange={(e) => setApiKey(e.target.value)} style={{ width: '100%' }} />
          </Field>
        </section>

        {Object.keys(grouped).length === 0
          ? <div className="empty">No endpoints match.</div>
          : Object.entries(grouped).map(([group, items]) => (
            <section key={group} style={{ marginBottom: 28 }}>
              <h2 id={slug(group)} style={{ borderBottom: '1px solid var(--border, #2a2a2a)', paddingBottom: 8, marginBottom: 16 }}>{group}</h2>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 32 }}>
                {items.map((e) => <EndpointBlock key={e.method + e.path} ep={e} apiKey={apiKey} onApiKey={setApiKey} />)}
              </div>
            </section>
          ))}
      </main>
    </div>
  )
}

function ApiKeyPanel({ apiKey, onApiKey }: { apiKey: string; onApiKey: (v: string) => void }) {
  const [keys, setKeys] = useState<any[]>([])
  const [creating, setCreating] = useState(false)
  const [newName, setNewName] = useState('')
  const [createdKey, setCreatedKey] = useState<string>('')
  const [err, setErr] = useState('')

  const loadKeys = () => api.listAPIKeys().then((k) => setKeys(k || [])).catch(() => setKeys([]))
  useEffect(() => { loadKeys() }, [])

  const create = async () => {
    setErr('')
    try {
      const k = await api.createAPIKey(newName || 'docs-try')
      setCreatedKey(k.key)
      onApiKey(k.key)
      setNewName('')
      setCreating(false)
      loadKeys()
    } catch (e: any) {
      setErr(String(e))
    }
  }

  return (
    <div style={{ marginBottom: 14, padding: 10, background: 'rgba(255,255,255,0.03)', border: `1px solid ${apiKey ? 'var(--border, #2a2a2a)' : '#ef4444'}`, borderRadius: 6 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
        <strong style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: 0.5 }}>API key</strong>
        <span className={apiKey ? 'badge ok' : 'badge err'} style={{ fontSize: 10 }}>{apiKey ? 'set' : 'missing'}</span>
      </div>
      <input
        type="password"
        placeholder="fa_xxxxxxxxxxxx"
        value={apiKey}
        onChange={(e) => onApiKey(e.target.value)}
        style={{ width: '100%', fontFamily: 'monospace', fontSize: 12 }}
        autoComplete="off"
      />
      {createdKey && (
        <div style={{ marginTop: 6, padding: 8, background: 'rgba(16,185,129,0.15)', border: '1px solid #10b981', borderRadius: 4 }}>
          <div className="small" style={{ marginBottom: 4, fontWeight: 600, color: '#10b981' }}>New key — saved & shown once:</div>
          <code style={{ wordBreak: 'break-all', fontSize: 11 }}>{createdKey}</code>
        </div>
      )}
      {!creating ? (
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: 6, gap: 6 }}>
          <button className="btn-primary" style={{ fontSize: 11, padding: '4px 8px', flex: 1 }} onClick={() => setCreating(true)}>+ Create new key</button>
          {keys.length > 0 && (
            <select
              onChange={(e) => { if (e.target.value === '__new__') { setCreating(true); return } }}
              style={{ fontSize: 11, flex: 1 }}
              defaultValue=""
            >
              <option value="" disabled>Saved keys ({keys.length})</option>
              {keys.map((k: any) => (
                <option key={k.id} value={k.id} disabled>{k.name || k.id}</option>
              ))}
              <option value="__new__">+ create another</option>
            </select>
          )}
        </div>
      ) : (
        <div style={{ marginTop: 6 }}>
          <input
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder="name (e.g. test-kiosk)"
            style={{ width: '100%', fontSize: 12, marginBottom: 4 }}
          />
          <div style={{ display: 'flex', gap: 4 }}>
            <button className="btn-primary" style={{ fontSize: 11, padding: '4px 8px', flex: 1 }} onClick={create}>Create</button>
            <button className="btn-ghost" style={{ fontSize: 11, padding: '4px 8px' }} onClick={() => { setCreating(false); setErr('') }}>Cancel</button>
          </div>
        </div>
      )}
      {err && <div className="err small" style={{ marginTop: 4, fontSize: 11 }}>{err}</div>}
      <div className="muted" style={{ fontSize: 10, marginTop: 6 }}>
        Keys live in the database. Random strings won't work — create one here or in Settings → API keys.
      </div>
    </div>
  )
}

function EndpointBlock({ ep, apiKey, onApiKey }: { ep: EndpointDef; apiKey: string; onApiKey: (v: string) => void }) {
  const [pathParams, setPathParams] = useState<Record<string, string>>({})
  const [queryParams, setQueryParams] = useState<Record<string, string>>({})
  const [bodyText, setBodyText] = useState<string>(() => ep.bodyExample ? JSON.stringify(ep.bodyExample, null, 2) : '')
  const [formFile, setFormFile] = useState<File | null>(null)
  const [formFields, setFormFields] = useState<Record<string, string>>({})
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<any | null>(null)
  const [copied, setCopied] = useState(false)

  const isMultipart = (ep.params || []).some((p) => p.in === 'form')
  const origin = typeof location !== 'undefined' ? location.origin : ''
  const anchor = slug(ep.method + '-' + ep.path)

  const buildPath = (): string => {
    let path = ep.path
    Object.entries(pathParams).forEach(([k, v]) => { if (v) path = path.replace(`:${k}`, encodeURIComponent(v)) })
    const qp = Object.entries(queryParams).filter(([_, v]) => v !== '').map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`).join('&')
    if (qp) path += (path.includes('?') ? '&' : '?') + qp
    return path
  }

  const run = async () => {
    if (ep.auth === 'v1' && !apiKey) {
      setResult({ status: 0, ok: false, body: 'No API key set. Paste one in the field above, or create one in Settings → API keys.' })
      return
    }
    setRunning(true); setResult(null)
    try {
      const path = buildPath()
      if (isMultipart) {
        const fd = new FormData()
        if (formFile) fd.append('file', formFile)
        Object.entries(formFields).forEach(([k, v]) => { if (v) fd.append(k, v) })
        const r = await api.raw(ep.method, path, { apiKey: ep.auth === 'v1' ? apiKey : undefined, body: fd, contentType: 'multipart/form-data' })
        setResult(r)
      } else {
        const opts: any = { apiKey: ep.auth === 'v1' ? apiKey : undefined }
        if (ep.method !== 'GET' && bodyText.trim()) {
          try { opts.body = JSON.parse(bodyText) } catch { opts.body = bodyText; opts.contentType = 'text/plain' }
        }
        const r = await api.raw(ep.method, path, opts)
        setResult(r)
      }
    } catch (e: any) {
      setResult({ status: 0, ok: false, body: String(e) })
    } finally { setRunning(false) }
  }

  const curlText = useMemo(() => {
    const lines: string[] = [`curl -X ${ep.method} '${origin}${buildPath()}'`]
    if (ep.auth === 'v1') lines.push(`  -H 'X-API-Key: $KEY'`)
    if (ep.method !== 'GET' && !isMultipart && bodyText.trim()) {
      lines.push(`  -H 'Content-Type: application/json'`)
      const escaped = bodyText.replace(/'/g, `'\\''`)
      lines.push(`  --data '${escaped}'`)
    }
    if (isMultipart) {
      lines.push(`  -F 'file=@/path/to/image.jpg'`)
      ;(ep.params || []).filter((p) => p.in === 'form' && p.name !== 'file').forEach((p) => {
        const v = formFields[p.name] || `<${p.name}>`
        lines.push(`  -F '${p.name}=${v}'`)
      })
    }
    return lines.join(' \\\n')
  }, [ep, bodyText, formFields, origin, isMultipart, pathParams, queryParams])

  const copy = async () => {
    try { await navigator.clipboard.writeText(curlText); setCopied(true); setTimeout(() => setCopied(false), 1500) } catch {}
  }

  const fullUrl = origin + buildPath()
  const responses = ep.responses && ep.responses.length > 0 ? ep.responses : DEFAULT_OK_RESPONSE()

  return (
    <article id={anchor} style={{ border: '1px solid var(--border, #2a2a2a)', borderRadius: 12, overflow: 'hidden', scrollMarginTop: 12 }}>
      <header style={{ padding: '16px 20px', background: 'rgba(255,255,255,0.02)', borderBottom: '1px solid var(--border, #2a2a2a)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 6, flexWrap: 'wrap' }}>
          <span style={{ background: METHOD_COLOR[ep.method], color: '#fff', padding: '4px 10px', borderRadius: 5, fontFamily: 'monospace', fontSize: 13, fontWeight: 700, minWidth: 64, textAlign: 'center' }}>{ep.method}</span>
          <code style={{ fontSize: 15, fontWeight: 600 }}>{ep.path}</code>
          {ep.auth === 'v1' && <span className="badge ok">requires API key</span>}
          <a href={`#${anchor}`} style={{ marginLeft: 'auto', opacity: 0.5, color: 'inherit', textDecoration: 'none', fontSize: 14 }} title="link to endpoint">#</a>
        </div>
        <div style={{ fontSize: 15 }}>{ep.summary}</div>
      </header>

      <div className="docs-endpoint-grid" style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) minmax(0, 1fr)', gap: 0 }}>
        <div style={{ padding: '16px 20px', borderRight: '1px solid var(--border, #2a2a2a)' }}>
          {ep.description && (
            <div style={{ marginBottom: 16 }}>
              {ep.description.split('\n\n').map((para, i) => <p key={i} style={{ whiteSpace: 'pre-wrap', margin: '0 0 8px' }}>{para}</p>)}
            </div>
          )}

          {ep.auth === 'v1' && (
            <div style={{ marginBottom: 16 }}>
              <h4 style={{ margin: '0 0 6px' }}>Authorization</h4>
              <pre className="result mono small" style={{ margin: 0 }}>{`Authorization: Bearer fa_xxx
# or
X-API-Key: fa_xxx`}</pre>
            </div>
          )}

          {(ep.params && ep.params.length > 0) && (
            <div style={{ marginBottom: 16 }}>
              <h4 style={{ margin: '0 0 6px' }}>Parameters</h4>
              <div className="table-wrap">
                <table className="data-table" style={{ fontSize: 13 }}>
                  <thead><tr><th>Name</th><th>In</th><th>Type</th><th>Required</th><th>Description</th></tr></thead>
                  <tbody>
                    {ep.params.map((p) => (
                      <tr key={p.name + p.in}>
                        <td><code>{p.name}</code></td>
                        <td><span className="muted small">{p.in}</span></td>
                        <td><span className="muted small">{p.type || 'string'}</span></td>
                        <td>{p.required ? <span className="badge err">yes</span> : <span className="muted small">no</span>}</td>
                        <td className="muted small">{p.description || ''}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {ep.bodyExample && !isMultipart && (
            <div style={{ marginBottom: 16 }}>
              <h4 style={{ margin: '0 0 6px' }}>Request body</h4>
              <div className="muted small" style={{ marginBottom: 4 }}>Content-Type: <code>application/json</code></div>
              <pre className="result mono small" style={{ margin: 0 }}>{JSON.stringify(ep.bodyExample, null, 2)}</pre>
            </div>
          )}

          {isMultipart && (
            <div style={{ marginBottom: 16 }}>
              <h4 style={{ margin: '0 0 6px' }}>Request body</h4>
              <div className="muted small">Content-Type: <code>multipart/form-data</code></div>
            </div>
          )}

          <div style={{ marginBottom: 16 }}>
            <h4 style={{ margin: '0 0 6px' }}>Responses</h4>
            {responses.map((r) => (
              <div key={r.status} style={{ marginBottom: 8 }}>
                <div style={{ display: 'flex', gap: 8, alignItems: 'baseline' }}>
                  <code style={{ color: r.status >= 200 && r.status < 300 ? '#10b981' : (r.status >= 400 ? '#ef4444' : '#f59e0b'), fontWeight: 700 }}>{r.status}</code>
                  <span className="muted small">{r.description}</span>
                </div>
                {r.example !== undefined && (
                  <pre className="result mono small" style={{ margin: '4px 0 0', whiteSpace: 'pre-wrap' }}>{typeof r.example === 'string' ? r.example : JSON.stringify(r.example, null, 2)}</pre>
                )}
              </div>
            ))}
          </div>
        </div>

        <div style={{ padding: '16px 20px', background: 'rgba(0,0,0,0.18)' }}>
          <h4 style={{ margin: '0 0 8px' }}>Try it</h4>

          {ep.auth === 'v1' && (
            <div style={{ marginBottom: 12, padding: 8, border: `1px solid ${apiKey ? 'var(--border, #2a2a2a)' : '#ef4444'}`, borderRadius: 6, background: apiKey ? 'transparent' : 'rgba(239,68,68,0.08)' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 4 }}>
                <label className="muted small" style={{ fontWeight: 600 }}>API key {!apiKey && <span style={{ color: '#ef4444' }}>(required)</span>}</label>
                <span className={apiKey ? 'badge ok' : 'badge err'} style={{ fontSize: 10 }}>{apiKey ? 'set' : 'missing'}</span>
              </div>
              <input
                type="password"
                placeholder="fa_xxxxxxxxxxxx — paste your API key"
                value={apiKey}
                onChange={(e) => onApiKey(e.target.value)}
                style={{ width: '100%', fontFamily: 'monospace', fontSize: 12 }}
                autoComplete="off"
              />
              {!apiKey && (
                <div className="muted" style={{ fontSize: 11, marginTop: 4 }}>
                  Random strings won't work — keys must exist in the database. Use the sidebar's <strong>+ Create new key</strong> button, or go to <a href="/" style={{ color: 'inherit' }}>Settings → API keys</a> in the dashboard.
                </div>
              )}
            </div>
          )}

          {(ep.params || []).filter((p) => p.in === 'path').length > 0 && (
            <div style={{ marginBottom: 10 }}>
              <div className="muted small" style={{ marginBottom: 4 }}>Path parameters</div>
              {(ep.params || []).filter((p) => p.in === 'path').map((p) => (
                <div key={p.name} style={{ marginBottom: 6 }}>
                  <label className="muted small">{p.name}{p.required && <span style={{ color: '#ef4444' }}> *</span>}</label>
                  <input value={pathParams[p.name] || ''} onChange={(e) => setPathParams({ ...pathParams, [p.name]: e.target.value })} placeholder={p.description || p.name} style={{ width: '100%' }} />
                </div>
              ))}
            </div>
          )}

          {(ep.params || []).filter((p) => p.in === 'query').length > 0 && (
            <div style={{ marginBottom: 10 }}>
              <div className="muted small" style={{ marginBottom: 4 }}>Query parameters</div>
              {(ep.params || []).filter((p) => p.in === 'query').map((p) => (
                <div key={p.name} style={{ marginBottom: 6 }}>
                  <label className="muted small">{p.name}</label>
                  <input value={queryParams[p.name] || ''} onChange={(e) => setQueryParams({ ...queryParams, [p.name]: e.target.value })} placeholder={p.description || p.name} style={{ width: '100%' }} />
                </div>
              ))}
            </div>
          )}

          {isMultipart && (
            <div style={{ marginBottom: 10 }}>
              <div className="muted small" style={{ marginBottom: 4 }}>Multipart fields</div>
              {(ep.params || []).filter((p) => p.in === 'form').map((p) => (
                <div key={p.name} style={{ marginBottom: 6 }}>
                  <label className="muted small">{p.name}{p.required && <span style={{ color: '#ef4444' }}> *</span>}</label>
                  {p.name === 'file'
                    ? <input type="file" onChange={(e) => setFormFile(e.target.files?.[0] || null)} style={{ width: '100%' }} />
                    : <input value={formFields[p.name] || ''} onChange={(e) => setFormFields({ ...formFields, [p.name]: e.target.value })} placeholder={p.description || p.name} style={{ width: '100%' }} />}
                </div>
              ))}
            </div>
          )}

          {ep.method !== 'GET' && !isMultipart && (
            <div style={{ marginBottom: 10 }}>
              <div className="muted small" style={{ marginBottom: 4 }}>Request body (JSON)</div>
              <textarea
                value={bodyText}
                onChange={(e) => setBodyText(e.target.value)}
                rows={Math.min(14, Math.max(4, (bodyText.match(/\n/g) || []).length + 2))}
                className="mono small"
                style={{ width: '100%', fontFamily: 'monospace', fontSize: 12 }}
                spellCheck={false}
              />
            </div>
          )}

          <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 12, flexWrap: 'wrap' }}>
            <button className="btn-primary" onClick={run} disabled={running}>{running ? 'Running…' : 'Try it ▶'}</button>
            {ep.responseType === 'image' && (
              <a className="btn-ghost" href={fullUrl + (ep.auth === 'v1' && apiKey ? (fullUrl.includes('?') ? '&' : '?') + 'apiKey=' + encodeURIComponent(apiKey) : '')} target="_blank" rel="noreferrer">Open image ↗</a>
            )}
            {ep.responseType === 'mjpeg' && (
              <a className="btn-ghost" href={fullUrl + (ep.auth === 'v1' && apiKey ? (fullUrl.includes('?') ? '&' : '?') + 'apiKey=' + encodeURIComponent(apiKey) : '')} target="_blank" rel="noreferrer">Open MJPEG ↗</a>
            )}
            {ep.responseType === 'sse' && (
              <span className="muted small">SSE stream — use curl to see live events</span>
            )}
          </div>

          <div>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 4 }}>
              <div className="muted small">cURL</div>
              <button className="btn-ghost" style={{ fontSize: 11, padding: '2px 8px' }} onClick={copy}>{copied ? 'copied!' : 'copy'}</button>
            </div>
            <pre className="result mono small" style={{ margin: 0, whiteSpace: 'pre-wrap', fontSize: 12 }}>{curlText}</pre>
          </div>

          {result && (
            <div style={{ marginTop: 12 }}>
              <div className="muted small" style={{ marginBottom: 4 }}>Response</div>
              <div style={{ display: 'flex', gap: 8, alignItems: 'baseline', marginBottom: 4 }}>
                <code style={{ color: result.ok ? '#10b981' : '#ef4444', fontWeight: 700 }}>{result.status}</code>
                <span className="muted small">{result.contentType}</span>
              </div>
              <pre className="result mono small" style={{ margin: 0, maxHeight: 280, overflow: 'auto' }}>{typeof result.body === 'string' ? result.body : JSON.stringify(result.body, null, 2)}</pre>
            </div>
          )}
        </div>
      </div>
    </article>
  )
}

// ===================== FaceApp Plugin =====================
//
// Single-page plugin for the standalone faceapp_main system. Manages config,
// status, people list, and bulk seeding. To remove this plugin: delete this
// component + its tab entry + the FaceApp api.ts wrappers + the backend
// faceapp_plugin.go file. Nothing else depends on it.

function FaceAppTab() {
  const [cfg, setCfg] = useState<any | null>(null)
  const [draft, setDraft] = useState<any>({ enabled: false, baseUrl: '', apiToken: '', deviceId: 0, timeoutSeconds: 10 })
  const [health, setHealth] = useState<any | null>(null)
  const [deviceStatus, setDeviceStatus] = useState<any | null>(null)
  const [people, setPeople] = useState<any[]>([])
  const [search, setSearch] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')

  const loadConfig = async () => {
    try {
      const c = await api.faceappGetConfig()
      setCfg(c)
      setDraft({
        enabled: !!c.enabled,
        baseUrl: c.baseUrl || '',
        apiToken: '',
        deviceId: c.deviceId || 0,
        timeoutSeconds: c.timeoutSeconds || 10,
      })
    } catch (e: any) { setErr(String(e)) }
  }

  const refreshLive = async () => {
    setErr(''); setMsg('')
    try {
      const [h, d, p] = await Promise.allSettled([
        api.faceappHealth(),
        api.faceappDeviceStatus(),
        api.faceappPeople(),
      ])
      setHealth(h.status === 'fulfilled' ? h.value : { error: String((h as any).reason) })
      setDeviceStatus(d.status === 'fulfilled' ? d.value : { error: String((d as any).reason) })
      setPeople(p.status === 'fulfilled' ? (p.value?.users || []) : [])
    } catch (e: any) { setErr(String(e)) }
  }

  useEffect(() => { loadConfig() }, [])
  useEffect(() => {
    if (cfg?.enabled) refreshLive()
  }, [cfg?.enabled])

  const saveConfig = async () => {
    setBusy(true); setErr(''); setMsg('')
    try {
      await api.faceappSaveConfig(draft)
      setMsg('Saved.')
      await loadConfig()
      if (draft.enabled) refreshLive()
    } catch (e: any) { setErr(String(e)) } finally { setBusy(false) }
  }

  const filtered = useMemo(() => {
    if (!search) return people
    const s = search.toLowerCase()
    return people.filter((p: any) =>
      (p.name || '').toLowerCase().includes(s) ||
      (p.employee_id || '').toLowerCase().includes(s) ||
      (p.department || '').toLowerCase().includes(s) ||
      (p.role || '').toLowerCase().includes(s)
    )
  }, [people, search])

  return (
    <>
      <div className="page-toolbar">
        <div className="toolbar-left">
          <h1 className="page-title">FaceApp <span className="muted">· plugin</span></h1>
        </div>
        <div className="toolbar-right" style={{ gap: 8 }}>
          {cfg?.enabled && <button className="btn-ghost" onClick={refreshLive}>↻ Refresh</button>}
          <span className={cfg?.enabled ? 'badge ok' : 'badge'}>
            <span className="status-dot" />
            {cfg?.enabled ? 'enabled' : 'disabled'}
          </span>
        </div>
      </div>

      {err && <div className="err">{err}</div>}
      {msg && <div className="muted small" style={{ marginBottom: 12 }}>{msg}</div>}

      <Card title="What this plugin does">
        <p className="muted" style={{ margin: 0, lineHeight: 1.6 }}>
          Bridges face_auth to a standalone <strong>faceapp_main</strong> instance (Laravel + Java gateway + Newland-style devices).
          Third-party callers hit <code>/api/v1/plugins/faceapp/*</code> with their face_auth API key — face_auth forwards to FaceApp using the bearer token stored here. The token never leaves the server.
        </p>
      </Card>

      <Card title="Configuration">
        <Field label="Enabled" hint="Turn the plugin on after you've filled in the URL + token.">
          <label className="toggle">
            <input type="checkbox" checked={!!draft.enabled} onChange={(e) => setDraft({ ...draft, enabled: e.target.checked })} />
            <span>{draft.enabled ? 'on' : 'off'}</span>
          </label>
        </Field>
        <Field label="Base URL" hint="Where FaceApp's Laravel API lives. No trailing slash.">
          <input
            placeholder="https://face.qbot.now"
            value={draft.baseUrl}
            onChange={(e) => setDraft({ ...draft, baseUrl: e.target.value })}
          />
        </Field>
        <Field label="API token" hint={cfg?.apiTokenSet ? 'A token is already saved. Leave blank to keep it, or paste a new one to rotate.' : 'FACEAPP_EXTERNAL_API_TOKEN value from the FaceApp env.'}>
          <input
            type="password"
            placeholder={cfg?.apiTokenSet ? '•••••••• (saved)' : 'paste FACEAPP_EXTERNAL_API_TOKEN'}
            value={draft.apiToken}
            onChange={(e) => setDraft({ ...draft, apiToken: e.target.value })}
            autoComplete="off"
          />
        </Field>
        <div className="form-row">
          <Field label="Device ID" hint="Managed device id on the FaceApp side. 0 = default.">
            <input
              type="number"
              min={0}
              value={draft.deviceId}
              onChange={(e) => setDraft({ ...draft, deviceId: parseInt(e.target.value || '0', 10) })}
            />
          </Field>
          <Field label="Timeout (sec)">
            <input
              type="number"
              min={2} max={60}
              value={draft.timeoutSeconds}
              onChange={(e) => setDraft({ ...draft, timeoutSeconds: parseInt(e.target.value || '10', 10) })}
            />
          </Field>
        </div>
        <div className="form-actions">
          <button className="btn-primary" disabled={busy} onClick={saveConfig}>{busy ? 'Saving…' : 'Save configuration'}</button>
          <button className="btn-ghost" disabled={busy || !cfg?.enabled} onClick={refreshLive}>Test connection</button>
        </div>
      </Card>

      {cfg?.enabled && (
        <>
          <div className="stat-grid">
            <StatTile
              label="FaceApp health"
              value={health?.ok ? 'Online' : (health?.error ? 'Error' : 'Unknown')}
              tone={health?.ok ? 'ok' : (health?.error ? 'err' : 'muted')}
              sub={health?.error || (health?.service ? `service: ${health.service}` : '')}
            />
            <StatTile
              label="Device"
              value={deviceStatus?.online ? 'Online' : (deviceStatus?.error ? 'Error' : 'Offline')}
              tone={deviceStatus?.online ? 'ok' : (deviceStatus?.error ? 'err' : 'muted')}
              sub={deviceStatus?.error || (deviceStatus?.name ? deviceStatus.name : '')}
            />
            <StatTile
              label="Enrolled people"
              value={String(people.length)}
              tone="muted"
              sub={people.length ? `${people.filter((p: any) => p.status === 'active').length} active` : ''}
            />
            <StatTile
              label="Device ID"
              value={cfg?.deviceId ? String(cfg.deviceId) : 'default'}
              tone="muted"
            />
          </div>

          <FaceAppGateCard />
          <FaceAppBulkSeedCard onDone={refreshLive} />

          <Card
            title={`Enrolled people (${people.length})`}
            header={
              <input
                className="search"
                placeholder="Search name, employee, role…"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                style={{ maxWidth: 280 }}
              />
            }
          >
            {people.length === 0 ? (
              <div className="empty">No people loaded. Click ↻ Refresh, or seed some via Bulk seed above.</div>
            ) : (
              <div className="table-wrap">
                <table className="data-table">
                  <thead>
                    <tr>
                      <th>Photo</th>
                      <th>Name</th>
                      <th>Employee</th>
                      <th>Role / Dept</th>
                      <th>Status</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filtered.map((p: any) => (
                      <tr key={p.id || p.employee_id}>
                        <td data-label="Photo">
                          <div className="snap-box small">
                            {p.face_photo
                              ? <img className="snap-img" src={p.face_photo} alt="" />
                              : <div className="snap-placeholder">—</div>}
                          </div>
                        </td>
                        <td data-label="Name"><strong>{p.name || '—'}</strong></td>
                        <td data-label="Employee"><span className="mono small">{p.employee_id || '—'}</span></td>
                        <td data-label="Role">
                          <div className="cell-stack">
                            <span>{p.role || '—'}</span>
                            {p.department && <span className="muted small">{p.department}</span>}
                          </div>
                        </td>
                        <td data-label="Status">
                          <span className={`badge ${p.status === 'active' ? 'ok' : p.status === 'pending' ? '' : 'err'}`}>{p.status || 'unknown'}</span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </>
      )}

      {!cfg?.enabled && (
        <div className="empty" style={{ padding: 32 }}>
          The plugin is disabled. Fill in <strong>Base URL</strong> + <strong>API token</strong> above, flip <strong>Enabled</strong>, then save.
        </div>
      )}
    </>
  )
}

function FaceAppGateCard() {
  const [plate, setPlate] = useState('')
  const [reason, setReason] = useState('admin-test')
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<any | null>(null)

  const fire = async () => {
    setRunning(true); setResult(null)
    try {
      const r = await api.faceappOpenGate({ plate, reason })
      setResult(r)
    } catch (e: any) {
      setResult({ ok: false, error: String(e) })
    } finally { setRunning(false) }
  }

  return (
    <Card title="Open gate (test)">
      <div className="muted small" style={{ marginBottom: 8 }}>
        Calls FaceApp's <code>/api/external/open-gate</code> with an optional plate + reason. Useful to verify wiring without enrolling anyone.
      </div>
      <div className="form-row">
        <Field label="Plate" grow={2}>
          <input value={plate} onChange={(e) => setPlate(e.target.value)} placeholder="ABC1234" />
        </Field>
        <Field label="Reason" grow={1}>
          <input value={reason} onChange={(e) => setReason(e.target.value)} />
        </Field>
      </div>
      <div className="form-actions">
        <button className="btn-primary" onClick={fire} disabled={running}>{running ? 'Opening…' : 'Open gate'}</button>
      </div>
      {result && (
        <pre className="result mono small" style={{ marginTop: 8 }}>{typeof result === 'string' ? result : JSON.stringify(result, null, 2)}</pre>
      )}
    </Card>
  )
}

function FaceAppBulkSeedCard({ onDone }: { onDone: () => void }) {
  const [items, setItems] = useState<{ name: string; employee_id: string; file?: File; preview?: string }[]>([])
  const [running, setRunning] = useState(false)
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null)
  const [result, setResult] = useState<any | null>(null)

  const onFiles = (files: FileList | null) => {
    if (!files) return
    const next: typeof items = []
    Array.from(files).forEach((f) => {
      const base = f.name.replace(/\.[^.]+$/, '')
      // Use filename pattern "name__employee.jpg" for auto-fill, otherwise just name.
      const parts = base.split('__')
      next.push({
        name: parts[0]?.trim() || base,
        employee_id: parts[1]?.trim() || '',
        file: f,
        preview: URL.createObjectURL(f),
      })
    })
    setItems([...items, ...next])
  }

  const updateItem = (i: number, patch: Partial<typeof items[0]>) => {
    setItems(items.map((it, idx) => (idx === i ? { ...it, ...patch } : it)))
  }
  const removeItem = (i: number) => setItems(items.filter((_, idx) => idx !== i))

  const seed = async () => {
    if (items.length === 0) return
    setRunning(true); setResult(null)
    const payloads: any[] = []
    let i = 0
    setProgress({ done: 0, total: items.length })
    for (const it of items) {
      i++
      try {
        if (!it.file) continue
        // file → base64 data URL
        const dataUrl = await new Promise<string>((resolve, reject) => {
          const r = new FileReader()
          r.onload = () => resolve(r.result as string)
          r.onerror = () => reject(r.error)
          r.readAsDataURL(it.file!)
        })
        payloads.push({
          name: it.name,
          employee_id: it.employee_id || it.name.replace(/\s+/g, '').slice(0, 16),
          photo_data_url: dataUrl,
        })
      } catch (e: any) {
        payloads.push({ name: it.name, employee_id: it.employee_id, error: String(e) })
      }
      setProgress({ done: i, total: items.length })
    }
    try {
      const r = await api.faceappBulkEnroll(payloads)
      setResult(r)
      onDone()
    } catch (e: any) {
      setResult({ ok: false, error: String(e) })
    } finally {
      setRunning(false); setProgress(null)
    }
  }

  return (
    <Card title="Bulk seed faces">
      <div className="muted small" style={{ marginBottom: 8 }}>
        Drop multiple JPEG/PNG photos. Name/employee fields are pre-filled from the filename
        (use <code>Alice__1001.jpg</code> to auto-set both, or just <code>Alice.jpg</code>).
        Each file becomes a base64 data-URL and is POSTed to <code>/api/enrollments</code> sequentially.
      </div>
      <div
        className="dropzone"
        onDragOver={(e) => { e.preventDefault(); e.currentTarget.classList.add('over') }}
        onDragLeave={(e) => e.currentTarget.classList.remove('over')}
        onDrop={(e) => {
          e.preventDefault()
          e.currentTarget.classList.remove('over')
          onFiles(e.dataTransfer.files)
        }}
      >
        <label style={{ cursor: 'pointer', display: 'block', padding: 24, textAlign: 'center' }}>
          <strong>Drop photos here</strong> or click to choose
          <input type="file" accept="image/*" multiple style={{ display: 'none' }} onChange={(e) => onFiles(e.target.files)} />
          <div className="muted small" style={{ marginTop: 6 }}>Tip: name files <code>Name__EmpId.jpg</code> to auto-fill</div>
        </label>
      </div>

      {items.length > 0 && (
        <div className="table-wrap" style={{ marginTop: 12 }}>
          <table className="data-table">
            <thead>
              <tr>
                <th>Preview</th>
                <th>Name</th>
                <th>Employee ID</th>
                <th className="ta-right">Remove</th>
              </tr>
            </thead>
            <tbody>
              {items.map((it, i) => (
                <tr key={i}>
                  <td><div className="snap-box small">{it.preview ? <img className="snap-img" src={it.preview} alt="" /> : <div className="snap-placeholder">—</div>}</div></td>
                  <td><input value={it.name} onChange={(e) => updateItem(i, { name: e.target.value })} /></td>
                  <td><input value={it.employee_id} onChange={(e) => updateItem(i, { employee_id: e.target.value })} placeholder="auto" /></td>
                  <td className="ta-right"><button className="btn-ghost danger" onClick={() => removeItem(i)}>×</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="form-actions" style={{ marginTop: 12 }}>
        <button className="btn-primary" onClick={seed} disabled={running || items.length === 0}>
          {running
            ? (progress ? `Seeding ${progress.done}/${progress.total}…` : 'Seeding…')
            : `Seed ${items.length} face${items.length === 1 ? '' : 's'}`}
        </button>
        <button className="btn-ghost" onClick={() => setItems([])} disabled={running || items.length === 0}>Clear</button>
      </div>

      {result && (
        <div style={{ marginTop: 12 }}>
          <div className={result.ok ? 'badge ok' : 'badge err'} style={{ marginBottom: 6 }}>
            {result.ok ? 'All succeeded' : `${result.success}/${result.total} succeeded`}
          </div>
          <pre className="result mono small" style={{ maxHeight: 240, overflow: 'auto' }}>{JSON.stringify(result, null, 2)}</pre>
        </div>
      )}
    </Card>
  )
}

function StatTile({ label, value, sub, tone = 'muted' }: { label: string; value: string; sub?: string; tone?: 'ok' | 'err' | 'muted' }) {
  const color = tone === 'ok' ? '#10b981' : tone === 'err' ? '#ef4444' : 'inherit'
  return (
    <div className="stat-tile">
      <div className="stat-label">{label}</div>
      <div className="stat-value" style={{ color }}>{value}</div>
      {sub && <div className="stat-sub muted small">{sub}</div>}
    </div>
  )
}

// ===================== HQ overview =====================

function HQTab() {
  const [me, setMeState] = useState<any>(null)
  useEffect(() => { api.me().then(setMeState).catch(() => {}) }, [])
  const isHQ = me?.user?.role === 'hq_admin'

  const [rows, setRows] = useState<any[]>([])
  const [creating, setCreating] = useState(false)
  const [err, setErr] = useState('')
  const [users, setUsers] = useState<any[]>([])
  const [newUser, setNewUser] = useState({ email: '', password: '', name: '', role: 'tenant_admin', tenantId: '' })

  const load = () => {
    if (!isHQ) return
    api.hqListTenants().then((t) => setRows(t || [])).catch((e) => setErr(String(e)))
    api.hqListUsers().then((u) => setUsers(u || [])).catch(() => {})
  }
  useEffect(() => { if (isHQ) load() }, [isHQ])

  if (me && !isHQ) {
    return (
      <>
        <div className="page-toolbar">
          <h1 className="page-title">HQ overview</h1>
        </div>
        <div className="card" style={{ borderColor: 'var(--warning, #F59E0B)' }}>
          <h3 style={{ marginTop: 0 }}>HQ access required</h3>
          <p className="muted">
            You're signed in as <strong>{me.user?.role}</strong>. This page is only available to
            <strong> hq_admin</strong> accounts — the top-level operators who can see and manage every tenant in the system.
          </p>
          <p className="muted">
            Sign out and sign in as your HQ admin (the default is <code>hq@faceauth.local</code> if you haven't customised it) to see this page.
            Your current role can manage everything within your own tenant — Devices, Members, Plans, Settings, etc.
          </p>
        </div>
      </>
    )
  }
  const del = async (id: string) => {
    if (!confirm(`Delete tenant ${id}? This cascades — all its devices, persons, plans, users will be removed.`)) return
    await api.hqDeleteTenant(id); load()
  }
  const createUser = async () => {
    setErr('')
    try {
      await api.hqCreateUser(newUser)
      setNewUser({ email: '', password: '', name: '', role: 'tenant_admin', tenantId: '' })
      load()
    } catch (e: any) { setErr(String(e)) }
  }

  const total = rows.reduce((acc: any, r: any) => ({
    devices: acc.devices + (r.deviceCount || 0),
    online:  acc.online  + (r.devicesOnline || 0),
    persons: acc.persons + (r.personCount || 0),
    plans:   acc.plans   + (r.planCount || 0),
  }), { devices: 0, online: 0, persons: 0, plans: 0 })

  return (
    <>
      <div className="page-toolbar">
        <div className="toolbar-left">
          <h1 className="page-title">HQ overview <span className="muted">· {rows.length} tenant{rows.length === 1 ? '' : 's'}</span></h1>
        </div>
        <div className="toolbar-right">
          <button className="btn-primary" onClick={() => setCreating(true)}>+ New tenant</button>
        </div>
      </div>
      {err && <div className="err">{err}</div>}

      <div className="stat-grid">
        <StatTile label="Tenants" value={String(rows.length)} />
        <StatTile label="Devices online" value={`${total.online}/${total.devices}`} tone={total.online > 0 ? 'ok' : 'muted'} />
        <StatTile label="Persons" value={String(total.persons)} />
        <StatTile label="Plans" value={String(total.plans)} />
      </div>

      <Card title="Tenants">
        <div className="table-wrap">
          <table className="data-table">
            <thead><tr><th>Name</th><th>Premise</th><th>Devices</th><th>Persons</th><th>Plans</th><th>Status</th><th className="ta-right">Actions</th></tr></thead>
            <tbody>
              {rows.length === 0 ? (
                <tr><td colSpan={7}><div className="empty" style={{ padding: 16 }}>No tenants yet. Click <strong>New tenant</strong> above to create one.</div></td></tr>
              ) : rows.map((r: any) => (
                <tr key={r.tenant.id}>
                  <td data-label="Name"><strong>{r.tenant.name}</strong><div className="muted small mono">{r.tenant.slug}</div></td>
                  <td data-label="Premise"><span className="badge">{r.tenant.premiseType || 'generic'}</span></td>
                  <td data-label="Devices">{r.devicesOnline}/{r.deviceCount}</td>
                  <td data-label="Persons">{r.personCount}</td>
                  <td data-label="Plans">{r.planCount}</td>
                  <td data-label="Status">{r.tenant.active ? <span className="badge ok">active</span> : <span className="badge err">disabled</span>}</td>
                  <td data-label="Actions" className="ta-right">
                    <button className="btn-ghost" onClick={() => { auth.activeTenantId = r.tenant.id; location.reload() }}>Switch →</button>
                    <button className="btn-ghost danger" onClick={() => del(r.tenant.id)}>Delete</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>

      <Card title="Users">
        <div className="muted small" style={{ marginBottom: 12 }}>
          All login accounts. <code>hq_admin</code> sees everything. <code>tenant_admin</code> manages one tenant.
        </div>
        <div className="form-row" style={{ gap: 8, marginBottom: 12, flexWrap: 'wrap' }}>
          <input placeholder="email" value={newUser.email} onChange={(e) => setNewUser({ ...newUser, email: e.target.value })} />
          <input type="password" placeholder="password" value={newUser.password} onChange={(e) => setNewUser({ ...newUser, password: e.target.value })} />
          <input placeholder="name" value={newUser.name} onChange={(e) => setNewUser({ ...newUser, name: e.target.value })} />
          <select value={newUser.role} onChange={(e) => setNewUser({ ...newUser, role: e.target.value })}>
            <option value="hq_admin">hq_admin</option>
            <option value="tenant_admin">tenant_admin</option>
            <option value="tenant_operator">tenant_operator</option>
          </select>
          <select value={newUser.tenantId} onChange={(e) => setNewUser({ ...newUser, tenantId: e.target.value })}>
            <option value="">— no tenant (HQ) —</option>
            {rows.map((r: any) => <option key={r.tenant.id} value={r.tenant.id}>{r.tenant.name}</option>)}
          </select>
          <button className="btn-primary" onClick={createUser} disabled={!newUser.email || !newUser.password}>+ Create user</button>
        </div>
        <div className="table-wrap">
          <table className="data-table">
            <thead><tr><th>Email</th><th>Role</th><th>Tenant</th><th>Last login</th><th className="ta-right">Actions</th></tr></thead>
            <tbody>
              {users.map((u: any) => (
                <tr key={u.id}>
                  <td><strong>{u.email}</strong><div className="muted small">{u.name}</div></td>
                  <td><span className="badge">{u.role}</span></td>
                  <td className="mono small">{u.tenantId || '—'}</td>
                  <td className="muted small">{u.lastLoginAt ? new Date(u.lastLoginAt).toLocaleString() : 'never'}</td>
                  <td className="ta-right">
                    <button className="btn-ghost danger" onClick={async () => { if (confirm(`Delete ${u.email}?`)) { await api.hqDeleteUser(u.id); load() } }}>Delete</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>

      {creating && (
        <NewTenantModal onClose={() => setCreating(false)} onCreated={() => { setCreating(false); load() }} />
      )}
    </>
  )
}

// ===================== Plans & rules =====================

const WEEKDAY_NAMES = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

function PlansTab() {
  const [plans, setPlans] = useState<any[]>([])
  const [editing, setEditing] = useState<any | null>(null)
  const [persons, setPersons] = useState<any[]>([])
  const [devices, setDevices] = useState<any[]>([])
  const [assigning, setAssigning] = useState<any | null>(null)
  const [premiseTypes, setPremiseTypes] = useState<any[]>([])
  const [installingPresets, setInstallingPresets] = useState(false)
  const [presetMsg, setPresetMsg] = useState('')
  const [err, setErr] = useState('')

  const load = () => {
    api.listPlans().then((p) => setPlans(p || [])).catch((e) => setErr(String(e)))
    api.listPersons().then((p) => setPersons(p || [])).catch(() => {})
    api.listDevices().then((d) => setDevices(d || [])).catch(() => {})
    api.premiseTypes().then((pt) => setPremiseTypes(pt || [])).catch(() => {})
  }
  useEffect(() => { load() }, [])

  const installTemplate = async (premiseKey: string) => {
    setInstallingPresets(true); setErr(''); setPresetMsg('')
    try {
      const r: any = await api.installPresets(premiseKey)
      setPresetMsg(`Installed ${(r?.installed || []).length} plan(s) from ${premiseKey} template.`)
      load()
    } catch (e: any) { setErr(String(e)) } finally { setInstallingPresets(false) }
  }

  const del = async (id: string) => {
    if (!confirm('Delete plan?')) return
    await api.deletePlan(id); load()
  }

  return (
    <>
      <div className="page-toolbar">
        <div className="toolbar-left">
          <h1 className="page-title">Membership plans <span className="muted">· {plans.length}</span></h1>
        </div>
        <div className="toolbar-right">
          {premiseTypes.length > 0 && (
            <select
              defaultValue=""
              disabled={installingPresets}
              onChange={(e) => {
                const v = e.target.value
                e.target.value = ''
                if (v) installTemplate(v)
              }}
              title="Bulk-install a curated set of plans for this premise type"
            >
              <option value="">Install template…</option>
              {premiseTypes.map((pt: any) => (
                <option key={pt.key} value={pt.key}>{pt.label} ({pt.presets?.length || 0} plans)</option>
              ))}
            </select>
          )}
          <button className="btn-primary" onClick={() => setEditing({
            name: '', type: 'unlimited', defaultCredits: 0, mustExitBeforeReentry: false, rules: [],
          })}>+ New plan</button>
        </div>
      </div>
      {err && <div className="err">{err}</div>}
      {presetMsg && <div className="muted small" style={{ marginBottom: 12 }}>{presetMsg}</div>}

      <Card>
        <div className="muted small" style={{ marginBottom: 12 }}>
          A plan describes how a member can enter:
          <ul style={{ margin: '6px 0 0 18px' }}>
            <li><strong>Unlimited</strong> — every entry allowed.</li>
            <li><strong>Credit-based</strong> — each entry uses 1 credit. Recharge by re-assigning.</li>
            <li><strong>Rule-based</strong> — allowed during one or more weekday + time windows (e.g. 6:00–9:00 AND 19:00–22:00).</li>
          </ul>
          Toggle <strong>Must exit before re-enter</strong> to enforce one-direction-at-a-time tracking.
        </div>
        {plans.length === 0
          ? <div className="empty">No plans yet.</div>
          : (
            <div className="plan-grid">
              {plans.map((p: any) => (
                <div key={p.id} className="plan-card">
                  <div className="plan-card-head">
                    <strong>{p.name}</strong>
                    <span className={`badge ${planTone(p.type)}`}>{p.type}</span>
                  </div>
                  <div className="muted small mono">{p.id}</div>
                  {p.type === 'credit' && <div className="muted small">Default credits: {p.defaultCredits}</div>}
                  {p.mustExitBeforeReentry && <div className="muted small">↳ Must exit before re-enter</div>}
                  {p.type === 'rule' && (p.rules || []).length > 0 && (
                    <div style={{ marginTop: 8 }}>
                      <div className="muted small">Rules:</div>
                      {(p.rules || []).map((r: any) => (
                        <div key={r.id} className="rule-pill">
                          <span>{r.label || `${r.startTime}-${r.endTime}`}</span>
                          <span className="muted small">{weekdayString(r.weekdays)} {r.startTime}–{r.endTime}</span>
                        </div>
                      ))}
                    </div>
                  )}
                  <div className="form-actions" style={{ marginTop: 12, gap: 6 }}>
                    <button className="btn-ghost" onClick={() => setEditing({ ...p, rules: p.rules || [] })}>Edit</button>
                    <button className="btn-ghost" onClick={() => setAssigning(p)}>Assign…</button>
                    <button className="btn-ghost danger" onClick={() => del(p.id)}>Delete</button>
                  </div>
                </div>
              ))}
            </div>
          )}
      </Card>

      {editing && (
        <PlanEditor
          plan={editing}
          onClose={() => setEditing(null)}
          onSaved={() => { setEditing(null); load() }}
        />
      )}

      {assigning && (
        <PlanAssignModal
          plan={assigning}
          persons={persons}
          devices={devices}
          onClose={() => setAssigning(null)}
          onSaved={() => { setAssigning(null); load() }}
        />
      )}
    </>
  )
}

function planTone(t: string) {
  if (t === 'unlimited') return 'ok'
  if (t === 'credit') return ''
  return 'err'
}
function weekdayString(mask: number) {
  if (mask === 127) return 'every day'
  if (mask === 0b0111111) return 'Mon-Sat'
  if (mask === 0b0011111) return 'Mon-Fri'
  const days: string[] = []
  for (let i = 0; i < 7; i++) if (mask & (1 << i)) days.push(WEEKDAY_NAMES[i])
  return days.join(', ')
}

function PlanEditor({ plan, onClose, onSaved }: { plan: any; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState(plan.name || '')
  const [type, setType] = useState<string>(plan.type || 'unlimited')
  const [credits, setCredits] = useState<number>(plan.defaultCredits || 0)
  const [mustExit, setMustExit] = useState<boolean>(!!plan.mustExitBeforeReentry)
  const [rules, setRules] = useState<any[]>(plan.rules || [])
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  const addRule = () => setRules([...rules, { weekdays: 127, startTime: '06:00', endTime: '09:00', label: '' }])
  const updateRule = (i: number, patch: any) => setRules(rules.map((r, idx) => idx === i ? { ...r, ...patch } : r))
  const removeRule = (i: number) => setRules(rules.filter((_, idx) => idx !== i))
  const toggleWeekday = (i: number, bit: number) => {
    const cur = rules[i].weekdays || 0
    updateRule(i, { weekdays: cur ^ (1 << bit) })
  }

  const save = async () => {
    setBusy(true); setErr('')
    try {
      const body = { name, type, defaultCredits: credits, mustExitBeforeReentry: mustExit, rules, active: true }
      if (plan.id) await api.updatePlan(plan.id, body)
      else await api.createPlan(body)
      onSaved()
    } catch (e: any) { setErr(String(e)) } finally { setBusy(false) }
  }

  return (
    <Modal title={plan.id ? 'Edit plan' : 'New plan'} onClose={onClose}>
      <Field label="Name"><input value={name} onChange={(e) => setName(e.target.value)} autoFocus /></Field>
      <Field label="Type">
        <select value={type} onChange={(e) => setType(e.target.value)}>
          <option value="unlimited">Unlimited — every entry allowed</option>
          <option value="credit">Credit-based — each entry uses 1 credit</option>
          <option value="rule">Rule-based — allowed in specific weekday/time windows</option>
        </select>
      </Field>
      {type === 'credit' && (
        <Field label="Default credits" hint="Issued to a person when they're first assigned to this plan.">
          <input type="number" min={0} value={credits} onChange={(e) => setCredits(parseInt(e.target.value || '0', 10))} />
        </Field>
      )}
      <Field label="Must exit before re-enter" hint="Prevent the same person from triggering two consecutive 'in' events.">
        <label className="toggle">
          <input type="checkbox" checked={mustExit} onChange={(e) => setMustExit(e.target.checked)} />
          <span>{mustExit ? 'on' : 'off'}</span>
        </label>
      </Field>

      {type === 'rule' && (
        <>
          <h4 style={{ marginTop: 16 }}>Rules</h4>
          <div className="muted small" style={{ marginBottom: 8 }}>
            A person passes the gate if ANY rule matches (OR semantics). To express "06:00–09:00 AND 19:00–22:00", add two rules.
          </div>
          {rules.map((r, i) => (
            <div key={i} className="rule-editor">
              <div className="rule-editor-row">
                <input placeholder="Label (optional)" value={r.label || ''} onChange={(e) => updateRule(i, { label: e.target.value })} />
                <input type="time" value={r.startTime} onChange={(e) => updateRule(i, { startTime: e.target.value })} />
                <span className="muted small">to</span>
                <input type="time" value={r.endTime} onChange={(e) => updateRule(i, { endTime: e.target.value })} />
                <button className="btn-ghost danger" onClick={() => removeRule(i)}>×</button>
              </div>
              <div className="weekday-row">
                {WEEKDAY_NAMES.map((name, idx) => (
                  <button
                    key={idx}
                    type="button"
                    className={`weekday-pill ${(r.weekdays || 0) & (1 << idx) ? 'on' : ''}`}
                    onClick={() => toggleWeekday(i, idx)}
                  >{name}</button>
                ))}
              </div>
            </div>
          ))}
          <button className="btn-ghost" onClick={addRule}>+ Add rule</button>
        </>
      )}

      {err && <div className="err" style={{ marginTop: 8 }}>{err}</div>}
      <div className="form-actions">
        <button className="btn-ghost" onClick={onClose}>Cancel</button>
        <button className="btn-primary" disabled={busy || !name} onClick={save}>{busy ? 'Saving…' : 'Save plan'}</button>
      </div>
    </Modal>
  )
}

function PlanAssignModal({ plan, persons, devices, onClose, onSaved }: { plan: any; persons: any[]; devices: any[]; onClose: () => void; onSaved: () => void }) {
  const [personId, setPersonId] = useState('')
  const [credits, setCredits] = useState<number>(plan.defaultCredits || 0)
  const [deviceId, setDeviceId] = useState('')
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')

  const assignPerson = async () => {
    setErr(''); setMsg('')
    try {
      await api.assignPersonPlan(personId, plan.id, plan.type === 'credit' ? credits : 0)
      setMsg(`Plan assigned to ${persons.find((p) => p.id === personId)?.name || personId}.`)
    } catch (e: any) { setErr(String(e)) }
  }
  const assignDevice = async () => {
    setErr(''); setMsg('')
    try {
      await api.assignDevicePlan(deviceId, plan.id)
      setMsg(`Plan attached to ${devices.find((d) => d.deviceID === deviceId)?.name || deviceId}.`)
    } catch (e: any) { setErr(String(e)) }
  }

  return (
    <Modal title={`Assign — ${plan.name}`} onClose={onClose}>
      <h4>Assign to a person</h4>
      <Field label="Person">
        <select value={personId} onChange={(e) => setPersonId(e.target.value)}>
          <option value="">— pick —</option>
          {persons.map((p) => <option key={p.id} value={p.id}>{p.name} ({p.employeeNo})</option>)}
        </select>
      </Field>
      {plan.type === 'credit' && (
        <Field label="Credits"><input type="number" min={0} value={credits} onChange={(e) => setCredits(parseInt(e.target.value || '0', 10))} /></Field>
      )}
      <div className="form-actions">
        <button className="btn-primary" disabled={!personId} onClick={assignPerson}>Assign to person</button>
      </div>

      <h4 style={{ marginTop: 20 }}>Attach to a device</h4>
      <Field label="Device">
        <select value={deviceId} onChange={(e) => setDeviceId(e.target.value)}>
          <option value="">— pick —</option>
          {devices.map((d: any) => <option key={d.deviceID} value={d.deviceID}>{d.name || d.deviceID}</option>)}
        </select>
      </Field>
      <div className="form-actions">
        <button className="btn-primary" disabled={!deviceId} onClick={assignDevice}>Attach to device</button>
      </div>

      {msg && <div className="muted small" style={{ marginTop: 12 }}>{msg}</div>}
      {err && <div className="err" style={{ marginTop: 12 }}>{err}</div>}
      <div className="form-actions" style={{ marginTop: 16 }}>
        <button className="btn-ghost" onClick={() => { onSaved() }}>Done</button>
      </div>
    </Modal>
  )
}

// ===================== Access log =====================

function NewTenantModal({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [premiseType, setPremiseType] = useState('generic')
  const [timezone, setTimezone] = useState('Asia/Kuala_Lumpur')
  const [contactEmail, setContactEmail] = useState('')
  const [contactPhone, setContactPhone] = useState('')
  const [address, setAddress] = useState('')
  const [installPresets, setInstallPresets] = useState(true)
  const [premiseTypes, setPremiseTypes] = useState<any[]>([])
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  useEffect(() => {
    api.premiseTypes().then((pt) => setPremiseTypes(pt || [])).catch(() => {})
  }, [])

  const selected = premiseTypes.find((p) => p.key === premiseType)

  const submit = async () => {
    setErr(''); setBusy(true)
    try {
      await api.hqCreateTenant({ name, slug, premiseType, timezone, contactEmail, contactPhone, address, installPresets })
      onCreated()
    } catch (e: any) { setErr(String(e)) } finally { setBusy(false) }
  }

  return (
    <Modal title="New tenant" onClose={onClose}>
      <Field label="Name">
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Acme Gym" autoFocus />
      </Field>
      <Field label="Slug" hint="URL-friendly identifier, auto-generated if blank">
        <input value={slug} onChange={(e) => setSlug(e.target.value)} placeholder="acme-gym" />
      </Field>
      <Field label="Premise type" hint="Drives the starter plan template + access patterns.">
        <select value={premiseType} onChange={(e) => setPremiseType(e.target.value)}>
          {premiseTypes.map((p: any) => <option key={p.key} value={p.key}>{p.label}</option>)}
        </select>
        {selected && <div className="muted small" style={{ marginTop: 4 }}>{selected.description}</div>}
      </Field>
      <div className="form-row">
        <Field label="Timezone"><input value={timezone} onChange={(e) => setTimezone(e.target.value)} /></Field>
        <Field label="Contact email"><input type="email" value={contactEmail} onChange={(e) => setContactEmail(e.target.value)} /></Field>
      </div>
      <div className="form-row">
        <Field label="Contact phone"><input value={contactPhone} onChange={(e) => setContactPhone(e.target.value)} /></Field>
        <Field label="Address"><input value={address} onChange={(e) => setAddress(e.target.value)} /></Field>
      </div>
      <Field label="Install preset plans" hint={`Creates ${selected?.presets?.length || 0} starter plan(s) for ${selected?.label || 'this premise'}. Fully editable later.`}>
        <label className="toggle">
          <input type="checkbox" checked={installPresets} onChange={(e) => setInstallPresets(e.target.checked)} />
          <span>{installPresets ? 'yes — bootstrap plans' : 'no — leave empty'}</span>
        </label>
      </Field>
      {err && <div className="err">{err}</div>}
      <div className="form-actions">
        <button className="btn-ghost" onClick={onClose}>Cancel</button>
        <button className="btn-primary" onClick={submit} disabled={!name || busy}>{busy ? 'Creating…' : 'Create tenant'}</button>
      </div>
    </Modal>
  )
}

function AccessLogTab() {
  const [rows, setRows] = useState<any[]>([])
  const [err, setErr] = useState('')
  const load = () => api.accessLog(300).then((r) => setRows(r || [])).catch((e) => setErr(String(e)))
  useEffect(() => {
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [])
  return (
    <>
      <div className="page-toolbar">
        <h1 className="page-title">Access log <span className="muted">· {rows.length}</span></h1>
        <div className="toolbar-right"><button className="btn-ghost" onClick={load}>↻ Refresh</button></div>
      </div>
      {err && <div className="err">{err}</div>}
      <Card>
        {rows.length === 0
          ? <div className="empty">No access decisions yet. Face matches arriving from your devices will appear here.</div>
          : (
            <div className="table-wrap">
              <table className="data-table">
                <thead><tr><th>When</th><th>Decision</th><th>Person</th><th>Device</th><th>Direction</th><th>Reason</th></tr></thead>
                <tbody>
                  {rows.map((r: any) => (
                    <tr key={r.id}>
                      <td className="muted small">{new Date(r.createdAt).toLocaleString()}</td>
                      <td><span className={`badge ${r.decision === 'allow' ? 'ok' : r.decision === 'deny' ? 'err' : ''}`}>{r.decision}</span></td>
                      <td><strong>{r.employeeNo || '—'}</strong></td>
                      <td className="mono small">{r.deviceId || '—'}</td>
                      <td className="muted small">{r.direction || ''}</td>
                      <td className="muted small">{r.reason}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
      </Card>
    </>
  )
}
