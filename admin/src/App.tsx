import { useEffect, useState, useRef, useMemo } from 'react'
import { api, apiUrl, subscribeEvents, DeviceForm, PersonForm } from './api'

type Tab = 'devices' | 'live' | 'persons' | 'enrol' | 'events' | 'agents' | 'console'

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
    { id: 'agents',  label: 'Agents' },
    { id: 'console', label: 'ISAPI' },
  ]

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <span className="logo-dot" />
          <span className="brand-text">face_auth</span>
        </div>

        <button className="nav-toggle" onClick={() => setNavOpen((v) => !v)} aria-label="menu">
          <span /><span /><span />
        </button>

        <nav className={`nav ${navOpen ? 'open' : ''}`}>
          {tabs.map((t) => (
            <button
              key={t.id}
              className={`tab-btn ${tab === t.id ? 'active' : ''}`}
              onClick={() => { setTab(t.id); setNavOpen(false) }}
            >{t.label}</button>
          ))}
        </nav>

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
        {tab === 'agents' && <AgentsTab />}
        {tab === 'console' && <ConsoleTab />}
      </main>
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
  useEffect(() => {
    const load = () => api.listDevices().then((d) => setDevices(d || [])).catch(() => {})
    load(); const t = setInterval(load, 10_000); return () => clearInterval(t)
  }, [])
  const online = devices.filter((d) => d.online)
  return (
    <>
      <div className="page-toolbar">
        <h1 className="page-title">Live preview <span className="muted">· {online.length} online</span></h1>
      </div>
      {online.length === 0 ? (
        <div className="empty">No online devices to preview. Add and probe a device first.</div>
      ) : (
        <div className="live-grid">
          {online.map((d) => (
            <div key={d.deviceID} className="live-tile">
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

  useEffect(() => {
    api.getPerson(personId).then(setData).catch((e) => setErr(String(e)))
  }, [personId])

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
