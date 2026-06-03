import { useEffect, useState, useRef, useMemo } from 'react'
import { api, apiUrl, subscribeEvents, DeviceForm, PersonForm } from './api'

type Tab = 'devices' | 'live' | 'persons' | 'enrol' | 'events' | 'qr-auth' | 'agents' | 'console' | 'test' | 'settings' | 'guide' | 'api-docs'

export default function App() {
  const [tab, setTab] = useState<Tab>('devices')
  const [status, setStatus] = useState<any>(null)
  const [theme, setTheme] = useState<'light' | 'dark'>(
    (localStorage.getItem('face_auth.theme') as 'light' | 'dark') || 'dark'
  )
  const [navOpen, setNavOpen] = useState(false)

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
    localStorage.setItem('face_auth.theme', theme)
  }, [theme])

  useEffect(() => {
    const tick = () => api.status().then(setStatus).catch(() => setStatus(null))
    tick()
    const t = setInterval(tick, 5000)
    return () => clearInterval(t)
  }, [])

  const tabs: { id: Tab; label: string }[] = [
    { id: 'devices', label: 'Devices' },
    { id: 'live',    label: 'Live' },
    { id: 'persons', label: 'Persons' },
    { id: 'enrol',   label: 'Enrol' },
    { id: 'events',  label: 'Events' },
    { id: 'qr-auth', label: 'QR Auth' },
    { id: 'agents',  label: 'Agents' },
    { id: 'console', label: 'ISAPI' },
    { id: 'test',    label: 'Test' },
    { id: 'settings',label: 'Settings' },
    { id: 'guide',   label: 'Setup Guide' },
    { id: 'api-docs',label: 'API Docs' },
  ]

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
          {tabs.map((t) => (
            <button
              key={t.id}
              className={`sidebar-item ${tab === t.id ? 'active' : ''}`}
              onClick={() => { setTab(t.id); setNavOpen(false) }}
            >{t.label}</button>
          ))}
        </nav>
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
          {tab === 'persons' && <PersonsTab />}
          {tab === 'enrol' && <EnrolTab />}
          {tab === 'events' && <EventsTab />}
          {tab === 'qr-auth' && <QRAuthTab />}
          {tab === 'agents' && <AgentsTab />}
          {tab === 'console' && <ConsoleTab />}
          {tab === 'test' && <TestTab />}
          {tab === 'settings' && <SettingsTab />}
          {tab === 'guide' && <GuideTab />}
          {tab === 'api-docs' && <ApiDocsTab />}
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
                    {d.ip
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
                      <button className="btn-ghost" onClick={() => probe(d.deviceID)}>Probe</button>
                      <button className="btn-ghost" onClick={() => setupAlarm(d.deviceID)}>Events</button>
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
    </>
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
  } : {
    deviceId: '', name: '', ip: '', port: 80, useHttps: false,
    isapiUsername: 'admin', isapiPassword: '',
    fdid: '1', faceLibType: 'blackFD', agentId: '',
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
      if (r.probe?.reachable || editing) {
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
        <div className="form-row">
          <Field label="Device IP" grow={3}>
            <input value={form.ip} onChange={(e) => setForm({ ...form, ip: e.target.value })} placeholder="192.168.100.64" required />
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
        <Field label="Reach via" hint="blank = direct LAN; pick an agent for cloud→LAN bridging">
          <select value={form.agentId || ''} onChange={(e) => setForm({ ...form, agentId: e.target.value })}>
            <option value="">Direct ISAPI</option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>{a.name || a.id} {a.online ? '· online' : '· offline'}</option>
            ))}
          </select>
        </Field>
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

function PersonsTab() {
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
    api.listDevices().then((d) => setDevices(d || [])).catch(() => setDevices([]))
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
          <button className="btn-ghost" onClick={sync} disabled={syncing || !deviceFilter}>{syncing ? 'Syncing…' : 'Sync from device'}</button>
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
      {viewing && <PersonDetailModal personId={viewing.id} onClose={() => setViewing(null)} onDeleted={() => { setViewing(null); load() }} />}
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

function PersonDetailModal({ personId, onClose, onDeleted }: { personId: string; onClose: () => void; onDeleted: () => void }) {
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
                  <img className="snap-img" src={apiUrl(`/api/images/${f.imageKey}`)} alt="" />
                </div>
              ))
          }
        </div>
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

function EnrolTab() {
  const [devices, setDevices] = useState<any[]>([])
  const [persons, setPersons] = useState<any[]>([])
  const [deviceId, setDeviceId] = useState('')
  const [personId, setPersonId] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [FDID, setFDID] = useState('1')
  const [faceLibType, setFaceLibType] = useState('blackFD')
  const [preview, setPreview] = useState<string | null>(null)
  const [result, setResult] = useState<any>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [cameraOpen, setCameraOpen] = useState(false)

  useEffect(() => {
    api.listDevices().then((d) => setDevices(d || [])).catch(() => setDevices([]))
    api.listPersons().then((p) => setPersons(p || [])).catch(() => setPersons([]))
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
            {e.imageKey && <img src={apiUrl(`/api/images/${e.imageKey}`)} alt="" />}
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
                <li>Download the watcher script: <a href={apiUrl('/api/agents/scripts/qr-watcher.ahk')} download>qr-watcher.ahk</a></li>
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
          <a className="download-tile" href={apiUrl('/api/agents/scripts/qr-watcher.ahk')} download>
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

  const save = async () => {
    if (!settings) return
    setBusy(true); setErr('')
    try {
      const next = await api.saveSettings(settings)
      setSettings(next)
    } catch (e: any) { setErr(String(e)) } finally { setBusy(false) }
  }

  const setDeviceOverride = async (deviceId: string, value: boolean | null) => {
    try {
      await api.setDeviceRequireQR(deviceId, value)
      load()
    } catch (e: any) { setErr(String(e)) }
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
        <div className="form-actions">
          <button className="btn-primary" disabled={busy} onClick={save}>{busy ? 'Saving…' : 'Save policy'}</button>
        </div>
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
                      <select value={current} onChange={(e) => {
                        const v = e.target.value
                        setDeviceOverride(d.deviceID, v === 'inherit' ? null : (v === 'require'))
                      }}>
                        <option value="inherit">Inherit global</option>
                        <option value="require">Force require QR</option>
                        <option value="face-only">Force face-only</option>
                      </select>
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
                  <a key={d.file} className="download-tile" href={apiUrl(`/api/agents/downloads/${encodeURIComponent(d.file)}`)} download>
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
            <li>Download the watcher: <a href={apiUrl('/api/agents/scripts/qr-watcher.ahk')} download><code>qr-watcher.ahk</code></a>.</li>
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
              <a className="download-tile" href={apiUrl('/api/agents/scripts/qr-watcher.ahk')} download>
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
  auth: 'v1' | 'admin'
  params?: { name: string; in: 'path' | 'query' | 'body' | 'form'; type?: string; required?: boolean; description?: string }[]
  bodyExample?: any
  responseType?: 'json' | 'image' | 'mjpeg' | 'sse' | 'text'
}

const ENDPOINTS: EndpointDef[] = [
  // v1 PUBLIC
  { method: 'GET', path: '/api/v1/ping', group: 'v1 — Health', summary: 'Liveness check', auth: 'v1' },
  { method: 'GET', path: '/api/v1/devices', group: 'v1 — Devices', summary: 'List devices (with effective QR mode)', auth: 'v1' },
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

  { method: 'POST', path: '/api/v1/auth/face/start', group: 'v1 — Face Auth', summary: 'Open a face-auth session ★', auth: 'v1', description: 'Behavior depends on the device requireQR2FA toggle. Provide qrToken / personId / employeeNo to identify a user. With face-only mode all three can be omitted.', bodyExample: { deviceId: 'lobby-1' } },
  { method: 'GET', path: '/api/v1/auth/face/:id', group: 'v1 — Face Auth', summary: 'Get session status', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'POST', path: '/api/v1/auth/face/:id/cancel', group: 'v1 — Face Auth', summary: 'Cancel an open session', auth: 'v1', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'GET', path: '/api/v1/auth/face/stream', group: 'v1 — Face Auth', summary: 'SSE stream of every face match', auth: 'v1', responseType: 'sse' },
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
  { method: 'GET', path: '/api/agents', group: 'admin — Agents', summary: 'List agents', auth: 'admin' },
  { method: 'POST', path: '/api/agents', group: 'admin — Agents', summary: 'Register agent (token shown once)', auth: 'admin', bodyExample: { id: 'lobby-agent', name: 'Lobby Agent' } },
  { method: 'DELETE', path: '/api/agents/:id', group: 'admin — Agents', summary: 'Delete an agent', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'POST', path: '/api/agents/:id/regen-token', group: 'admin — Agents', summary: 'Rotate agent token', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'GET', path: '/api/agents/downloads', group: 'admin — Agents', summary: 'List available agent binaries', auth: 'admin' },
  { method: 'GET', path: '/api/qr-auth/sessions', group: 'admin — QR Auth', summary: 'Active + historical sessions', auth: 'admin' },
  { method: 'POST', path: '/api/qr-auth/scan', group: 'admin — QR Auth', summary: 'Agent-style QR scan (admin)', auth: 'admin', bodyExample: { qrToken: 'paste-here', agentId: '' } },
  { method: 'POST', path: '/api/devices/:id/lock-all-users', group: 'admin — QR Auth', summary: 'Lock every device user into baseline', auth: 'admin', params: [{ name: 'id', in: 'path', required: true }] },
  { method: 'GET', path: '/api/events', group: 'admin — Events', summary: 'List events', auth: 'admin' },
  { method: 'GET', path: '/api/events/stream', group: 'admin — Events', summary: 'SSE stream', auth: 'admin', responseType: 'sse' },
]

function ApiDocsTab() {
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

  return (
    <>
      <div className="page-toolbar">
        <div className="toolbar-left">
          <h1 className="page-title">API docs <span className="muted">· {ENDPOINTS.length} endpoints</span></h1>
        </div>
        <div className="toolbar-right" style={{ gap: 8 }}>
          <select value={groupFilter} onChange={(e) => setGroupFilter(e.target.value as any)}>
            <option value="all">All</option>
            <option value="v1">v1 (third-party)</option>
            <option value="admin">admin (UI)</option>
          </select>
          <input className="search" placeholder="Filter…" value={filter} onChange={(e) => setFilter(e.target.value)} />
        </div>
      </div>

      <Card title="Authentication">
        <p className="muted small" style={{ marginBottom: 8 }}>
          v1 endpoints require an API key. Admin endpoints share this browser session. Paste a key once — it's used for every Try-it below.
        </p>
        <Field label="API key (used by Try it)">
          <input type="password" placeholder="fa_xxxxxxxxxxxx" value={apiKey} onChange={(e) => setApiKey(e.target.value)} />
        </Field>
      </Card>

      {Object.keys(grouped).length === 0
        ? <div className="empty">No endpoints match.</div>
        : Object.entries(grouped).map(([group, items]) => (
          <Card key={group} title={group}>
            <div className="endpoint-list" style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              {items.map((e) => <EndpointRow key={e.method + e.path} ep={e} apiKey={apiKey} />)}
            </div>
          </Card>
        ))}
    </>
  )
}

function EndpointRow({ ep, apiKey }: { ep: EndpointDef; apiKey: string }) {
  const [open, setOpen] = useState(false)
  const [pathParams, setPathParams] = useState<Record<string, string>>({})
  const [queryParams, setQueryParams] = useState<Record<string, string>>({})
  const [bodyText, setBodyText] = useState<string>(() => ep.bodyExample ? JSON.stringify(ep.bodyExample, null, 2) : '')
  const [formFile, setFormFile] = useState<File | null>(null)
  const [formFields, setFormFields] = useState<Record<string, string>>({})
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<any | null>(null)

  const isMultipart = (ep.params || []).some((p) => p.in === 'form')

  const buildPath = (): string => {
    let path = ep.path
    Object.entries(pathParams).forEach(([k, v]) => { path = path.replace(`:${k}`, encodeURIComponent(v)) })
    const qp = Object.entries(queryParams).filter(([_, v]) => v !== '').map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`).join('&')
    if (qp) path += (path.includes('?') ? '&' : '?') + qp
    return path
  }

  const run = async () => {
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

  const methodColor: Record<string, string> = { GET: '#3b82f6', POST: '#10b981', PUT: '#f59e0b', DELETE: '#ef4444' }

  return (
    <div className={`endpoint-row ${open ? 'open' : ''}`} style={{ border: '1px solid var(--border, #2a2a2a)', borderRadius: 8 }}>
      <div className="endpoint-head" onClick={() => setOpen(!open)} style={{ cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 10, padding: '8px 12px' }}>
        <span style={{ background: methodColor[ep.method], color: '#fff', padding: '2px 8px', borderRadius: 4, fontFamily: 'monospace', fontSize: 12, minWidth: 56, textAlign: 'center' }}>{ep.method}</span>
        <code style={{ flex: '0 0 auto' }}>{ep.path}</code>
        <span className="endpoint-summary muted" style={{ flex: 1, fontSize: 13 }}>{ep.summary}</span>
        {ep.auth === 'v1' && <span className="badge ok">v1</span>}
        <span style={{ width: 16, textAlign: 'center', opacity: 0.6 }}>{open ? '▾' : '▸'}</span>
      </div>
      {open && (
        <div className="endpoint-body" style={{ padding: '8px 12px 12px', borderTop: '1px solid var(--border, #2a2a2a)' }}>
          {ep.description && <p>{ep.description}</p>}
          {(ep.params && ep.params.length > 0) && (
            <div className="params">
              <h4>Parameters</h4>
              <div className="table-wrap">
                <table className="data-table">
                  <thead><tr><th>Name</th><th>In</th><th>Type</th><th>Req?</th><th>Description</th><th>Value</th></tr></thead>
                  <tbody>
                    {ep.params.map((p) => (
                      <tr key={p.name + p.in}>
                        <td><code>{p.name}</code></td>
                        <td>{p.in}</td>
                        <td>{p.type || 'string'}</td>
                        <td>{p.required ? <span className="badge err">yes</span> : <span className="muted small">no</span>}</td>
                        <td className="muted small">{p.description}</td>
                        <td>
                          {p.in === 'path' && <input value={pathParams[p.name] || ''} onChange={(e) => setPathParams({ ...pathParams, [p.name]: e.target.value })} />}
                          {p.in === 'query' && <input value={queryParams[p.name] || ''} onChange={(e) => setQueryParams({ ...queryParams, [p.name]: e.target.value })} />}
                          {p.in === 'form' && p.name === 'file' && <input type="file" onChange={(e) => setFormFile(e.target.files?.[0] || null)} />}
                          {p.in === 'form' && p.name !== 'file' && <input value={formFields[p.name] || ''} onChange={(e) => setFormFields({ ...formFields, [p.name]: e.target.value })} />}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
          {ep.method !== 'GET' && !isMultipart && (
            <Field label="Request body (JSON)">
              <textarea value={bodyText} onChange={(e) => setBodyText(e.target.value)} rows={Math.min(10, Math.max(3, (bodyText.match(/\n/g) || []).length + 2))} className="mono small" style={{ width: '100%', fontFamily: 'monospace' }} />
            </Field>
          )}
          <div className="form-actions" style={{ alignItems: 'center', gap: 12 }}>
            <button className="btn-primary" onClick={run} disabled={running}>{running ? 'Running…' : 'Try it'}</button>
            {ep.responseType === 'image' && (
              <a className="btn-ghost" href={apiUrl(buildPath()) + (ep.auth === 'v1' && apiKey ? (buildPath().includes('?') ? '&' : '?') + 'apiKey=' + encodeURIComponent(apiKey) : '')} target="_blank" rel="noreferrer">Open image</a>
            )}
            {ep.responseType === 'mjpeg' && (
              <a className="btn-ghost" href={apiUrl(buildPath()) + (ep.auth === 'v1' && apiKey ? (buildPath().includes('?') ? '&' : '?') + 'apiKey=' + encodeURIComponent(apiKey) : '')} target="_blank" rel="noreferrer">Open MJPEG</a>
            )}
          </div>
          <details style={{ marginTop: 8 }}>
            <summary className="muted small">Show curl example</summary>
            <pre className="result mono small">{`curl -X ${ep.method} ${ep.auth === 'v1' ? `-H "X-API-Key: $KEY" ` : ''}${ep.method !== 'GET' && !isMultipart ? `-H 'Content-Type: application/json' ` : ''}${ep.method !== 'GET' && !isMultipart && bodyText.trim() ? `-d '${bodyText.replace(/\n/g, ' ').replace(/\s+/g, ' ')}' ` : ''}${typeof location !== 'undefined' ? location.origin : ''}${buildPath()}`}</pre>
          </details>
          {result && (
            <div className="result-block" style={{ marginTop: 8 }}>
              <div><strong>Status:</strong> <code className={result.ok ? 'ok' : 'err'}>{result.status}</code> <span className="muted small">{result.contentType}</span></div>
              <pre className="result">{typeof result.body === 'string' ? result.body : JSON.stringify(result.body, null, 2)}</pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
