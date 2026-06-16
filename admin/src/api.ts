// In dev (docker compose locally), VITE_API_URL points at the backend's exposed port.
// In prod behind the shared reverse proxy, leave it empty — same-origin requests work fine.
const API = (import.meta.env.VITE_API_URL as string) ?? ''

export const apiUrl = (path: string) => `${API}${path}`

// ----- Session token + tenant context plumbing -----
// Token is stored in localStorage; it's added to every request below.
// HQ users may pick an "active tenant" — that value travels in X-Tenant-Id
// so tenant-scoped endpoints know which tenant to operate on.
export const auth = {
  get token() { return localStorage.getItem('face_auth.session') || '' },
  set token(v: string) { v ? localStorage.setItem('face_auth.session', v) : localStorage.removeItem('face_auth.session') },
  get activeTenantId() { return localStorage.getItem('face_auth.activeTenant') || '' },
  set activeTenantId(v: string) { v ? localStorage.setItem('face_auth.activeTenant', v) : localStorage.removeItem('face_auth.activeTenant') },
  clear() { localStorage.removeItem('face_auth.session'); localStorage.removeItem('face_auth.activeTenant') },
}

async function req<T = any>(path: string, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = {
    ...((init?.headers as Record<string, string>) || {}),
  }
  if (auth.token) headers['Authorization'] = `Bearer ${auth.token}`
  // Don't leak tenant context onto unauthenticated endpoints — strips a
  // common cause of "silent CORS failure" when a stale localStorage value
  // would otherwise add an unexpected header to /api/auth/login.
  if (auth.activeTenantId && !path.startsWith('/api/auth/')) {
    headers['X-Tenant-Id'] = auth.activeTenantId
  }
  let res: Response
  try {
    res = await fetch(apiUrl(path), { ...init, headers })
  } catch (e: any) {
    // Network failure, CORS rejection, DNS error, server down — fetch rejects
    // with a TypeError. Surface a real, readable message instead of letting
    // the UI hang on "Signing in…".
    throw new Error(
      `Cannot reach the backend at ${apiUrl(path)}. ${String(e?.message || e)}`
    )
  }
  if (res.status === 401 && path !== '/api/auth/login') {
    auth.clear()
    window.dispatchEvent(new Event('face_auth:unauthorized'))
  }
  if (!res.ok) {
    let msg = res.statusText
    try {
      const body = await res.json()
      msg = body.error || JSON.stringify(body)
    } catch {}
    throw new Error(`${res.status}: ${msg}`)
  }
  const ct = res.headers.get('content-type') || ''
  return ct.includes('json') ? res.json() : (res.text() as any)
}

export interface PersonForm {
  name: string
  employeeNo?: string
  gender?: 'male' | 'female' | 'unknown'
  personType?: 'normal' | 'visitor' | 'blackList'
  personRole?: 'basic' | 'administrator' | 'operator'
  longTerm?: boolean
  attendanceOnly?: boolean
  doorRight?: string
  planTemplate?: string
  validBegin?: string
  validEnd?: string
}

export interface DeviceForm {
  deviceId: string
  name?: string
  password?: string
  ip?: string
  port?: number
  useHttps?: boolean
  isapiUsername?: string
  isapiPassword?: string
  fdid?: string
  faceLibType?: string
  agentId?: string
}

export const api = {
  status: () => req('/api/status'),
  listDevices: () => req('/api/devices'),
  registerDevice: (body: DeviceForm) =>
    req('/api/devices', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  deleteDevice: (id: string) => req(`/api/devices/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  probeDevice: (id: string) => req(`/api/devices/${encodeURIComponent(id)}/probe`, { method: 'POST' }),
  probeAll: () => req('/api/devices/probe-all', { method: 'POST' }),
  setupAlarmHost: (id: string, hostIp?: string, hostPort?: number) =>
    req(`/api/devices/${encodeURIComponent(id)}/setup-alarm-host`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ hostIp, hostPort }),
    }),
  deviceFaceLib: (id: string) => req(`/api/devices/${encodeURIComponent(id)}/face-lib`),
  rawIsapi: (id: string, method: string, path: string, body: string) =>
    req(`/api/devices/${encodeURIComponent(id)}/isapi`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ method, path, body }),
    }),
  deleteFace: (deviceId: string, personId: string) =>
    req(`/api/devices/${encodeURIComponent(deviceId)}/faces/${encodeURIComponent(personId)}`, { method: 'DELETE' }),

  listPersons: () => req('/api/persons'),
  createPerson: (body: PersonForm) =>
    req('/api/persons', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  deletePerson: (id: string, syncDeviceId?: string) => {
    const q = syncDeviceId ? `?syncDevice=${encodeURIComponent(syncDeviceId)}` : ''
    return req(`/api/persons/${encodeURIComponent(id)}${q}`, { method: 'DELETE' })
  },
  getPerson: (id: string) => req(`/api/persons/${encodeURIComponent(id)}`),
  syncPersons: (deviceId: string) => req(`/api/devices/${encodeURIComponent(deviceId)}/sync-persons`, { method: 'POST' }),
  openDoor: (deviceId: string, door = 1) => req(`/api/devices/${encodeURIComponent(deviceId)}/open-door?door=${door}`, { method: 'POST' }),
  snapshotUrl: (deviceId: string) => apiUrl(`/api/devices/${encodeURIComponent(deviceId)}/snapshot`),

  enrolFace: async (deviceId: string, personId: string, file: File, opts?: { name?: string; FDID?: string; faceLibType?: string }) => {
    const fd = new FormData()
    fd.append('file', file)
    if (personId) fd.append('personId', personId)
    if (opts?.name) fd.append('name', opts.name)
    if (opts?.FDID) fd.append('FDID', opts.FDID)
    if (opts?.faceLibType) fd.append('faceLibType', opts.faceLibType)
    return req(`/api/devices/${encodeURIComponent(deviceId)}/faces`, { method: 'POST', body: fd })
  },

  listFaces: (deviceId?: string, personId?: string) => {
    const params = new URLSearchParams()
    if (personId) params.set('personId', personId)
    const dev = deviceId ? `/devices/${encodeURIComponent(deviceId)}/faces` : `/devices//faces`
    return req(`/api${dev}?${params.toString()}`)
  },

  listEvents: (deviceId?: string, limit = 100) => {
    const params = new URLSearchParams({ limit: String(limit) })
    if (deviceId) params.set('deviceId', deviceId)
    return req(`/api/events?${params.toString()}`)
  },
  purgeEmptyEvents: () => req('/api/events/purge-empty', { method: 'POST' }),
  bridgeInfo: () => req('/api/bridge/info'),
  bridgeLog: () => req('/api/bridge/log'),
  bridgeClearLog: () => req('/api/bridge/log/clear', { method: 'POST' }),
  listAgents: () => req('/api/agents'),
  createAgent: (id: string, name: string) =>
    req('/api/agents', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id, name }),
    }),
  deleteAgent: (id: string) => req(`/api/agents/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  regenAgentToken: (id: string) => req(`/api/agents/${encodeURIComponent(id)}/regen-token`, { method: 'POST' }),
  agentDownloads: () => req('/api/agents/downloads'),
  agentDownloadUrl: (file: string) => apiUrl(`/api/agents/downloads/${encodeURIComponent(file)}`),
  rotateQR: (personId: string) => req(`/api/persons/${encodeURIComponent(personId)}/qr/rotate`, { method: 'POST' }),
  qrImageUrl: (personId: string, size = 256) => apiUrl(`/api/persons/${encodeURIComponent(personId)}/qr.png?size=${size}`),
  qrAuthSessions: () => req('/api/qr-auth/sessions'),
  qrAuthScan: (qrToken: string, agentId?: string) =>
    req('/api/qr-auth/scan', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ qrToken, agentId }),
    }),
  lockAllUsers: (deviceId: string) => req(`/api/devices/${encodeURIComponent(deviceId)}/lock-all-users`, { method: 'POST' }),

  // Settings (admin) — controls global QR-2FA toggle + face-auth window
  getSettings: () => req('/api/settings'),
  saveSettings: (body: any) =>
    req('/api/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  getDeviceRequireQR: (deviceId: string) =>
    req(`/api/devices/${encodeURIComponent(deviceId)}/require-qr`),
  setDeviceRequireQR: (deviceId: string, value: boolean | null) =>
    req(`/api/devices/${encodeURIComponent(deviceId)}/require-qr`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ value }),
    }),

  applyDeviceMode: (deviceId: string) =>
    req(`/api/devices/${encodeURIComponent(deviceId)}/apply-mode`, { method: 'POST' }),
  applyAllDeviceModes: () => req('/api/settings/apply-all', { method: 'POST' }),

  // API keys (admin)
  listAPIKeys: () => req('/api/api-keys'),
  createAPIKey: (name: string) =>
    req('/api/api-keys', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }),
  deleteAPIKey: (id: string) => req(`/api/api-keys/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  // Test face-auth from the admin UI (admin endpoint mirrors v1 logic).
  // We just call the public v1 start endpoint with an api key passed by the user.
  startFaceAuth: (apiKey: string, body: { deviceId: string; personId?: string; employeeNo?: string; qrToken?: string }) =>
    req('/api/v1/auth/face/start', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-API-Key': apiKey },
      body: JSON.stringify(body),
    }),
  getFaceAuthSession: (apiKey: string, sessionId: string) =>
    req(`/api/v1/auth/face/${encodeURIComponent(sessionId)}`, {
      headers: { 'X-API-Key': apiKey },
    }),
  cancelFaceAuthSession: (apiKey: string, sessionId: string) =>
    req(`/api/v1/auth/face/${encodeURIComponent(sessionId)}/cancel`, {
      method: 'POST',
      headers: { 'X-API-Key': apiKey },
    }),

  mjpegUrl: (deviceId: string, fps = 4) =>
    apiUrl(`/api/devices/${encodeURIComponent(deviceId)}/stream.mjpg?fps=${fps}`),

  // ---- FaceApp plugin ----
  faceappGetConfig: () => req('/api/plugins/faceapp/config'),
  faceappSaveConfig: (body: any) =>
    req('/api/plugins/faceapp/config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  faceappHealth: () => req('/api/plugins/faceapp/health'),
  faceappDeviceStatus: () => req('/api/plugins/faceapp/device-status'),
  faceappPeople: () => req('/api/plugins/faceapp/people'),
  faceappEnroll: (body: any) =>
    req('/api/plugins/faceapp/enroll', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  faceappBulkEnroll: (people: any[]) =>
    req('/api/plugins/faceapp/enroll/bulk', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ people }),
    }),
  faceappOpenGate: (body?: any) =>
    req('/api/plugins/faceapp/open-gate', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body || {}),
    }),
  faceappEnrollmentStatus: (publicId: string) =>
    req(`/api/plugins/faceapp/enrollments/${encodeURIComponent(publicId)}`),

  healthz: () => req('/healthz'),

  // ---- Auth / multi-tenant ----
  login: (email: string, password: string) =>
    req('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password }),
    }),
  logout: () => req('/api/auth/logout', { method: 'POST' }),
  me: () => req('/api/auth/me'),

  // ---- HQ ----
  hqListTenants: () => req('/api/hq/tenants'),
  hqCreateTenant: (body: { name: string; slug?: string; premiseType?: string; timezone?: string; contactEmail?: string; contactPhone?: string; address?: string; installPresets?: boolean }) =>
    req('/api/hq/tenants', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  hqUpdateTenant: (id: string, body: any) =>
    req(`/api/hq/tenants/${encodeURIComponent(id)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  premiseTypes: () => req('/api/premise-types'),
  installPresets: (premiseType?: string) =>
    req('/api/plans/install-presets', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ premiseType: premiseType || '' }),
    }),
  hqDeleteTenant: (id: string) => req(`/api/hq/tenants/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  hqListUsers: (tenantId?: string) => req(`/api/hq/users${tenantId ? `?tenantId=${encodeURIComponent(tenantId)}` : ''}`),
  hqCreateUser: (body: any) =>
    req('/api/hq/users', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  hqDeleteUser: (id: string) => req(`/api/hq/users/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  // ---- Plans ----
  listPlans: () => req('/api/plans'),
  createPlan: (body: any) =>
    req('/api/plans', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  updatePlan: (id: string, body: any) =>
    req(`/api/plans/${encodeURIComponent(id)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  deletePlan: (id: string) => req(`/api/plans/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  assignPersonPlan: (personId: string, planId: string, credits?: number) =>
    req(`/api/persons/${encodeURIComponent(personId)}/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ planId, credits }),
    }),
  getPersonPlan: (personId: string) => req(`/api/persons/${encodeURIComponent(personId)}/plan`),
  listDevicePlans: (deviceId: string) => req(`/api/devices/${encodeURIComponent(deviceId)}/plans`),
  assignDevicePlan: (deviceId: string, planId: string) =>
    req(`/api/devices/${encodeURIComponent(deviceId)}/plans/${encodeURIComponent(planId)}`, { method: 'POST' }),
  unassignDevicePlan: (deviceId: string, planId: string) =>
    req(`/api/devices/${encodeURIComponent(deviceId)}/plans/${encodeURIComponent(planId)}`, { method: 'DELETE' }),
  accessLog: (limit = 200) => req(`/api/access-log?limit=${limit}`),

  // Generic raw caller for API Docs "Try it" feature
  raw: async (method: string, path: string, opts?: { apiKey?: string; body?: any; contentType?: string }) => {
    const headers: Record<string, string> = {}
    if (opts?.apiKey) headers['X-API-Key'] = opts.apiKey
    let bodyToSend: any = undefined
    if (opts?.body !== undefined && opts?.body !== null) {
      if (opts.contentType === 'multipart/form-data' && opts.body instanceof FormData) {
        bodyToSend = opts.body
      } else {
        headers['Content-Type'] = opts.contentType || 'application/json'
        bodyToSend = typeof opts.body === 'string' ? opts.body : JSON.stringify(opts.body)
      }
    }
    const res = await fetch(apiUrl(path), { method, headers, body: bodyToSend })
    const ct = res.headers.get('content-type') || ''
    let parsed: any
    if (ct.includes('json')) {
      try { parsed = await res.json() } catch { parsed = null }
    } else if (ct.startsWith('image/')) {
      parsed = `(${ct}, ${res.headers.get('content-length') || '?'} bytes — open in new tab to view)`
    } else {
      parsed = await res.text()
    }
    return { status: res.status, ok: res.ok, body: parsed, contentType: ct }
  },
}

export function subscribeEvents(onEvent: (e: any) => void) {
  const es = new EventSource(apiUrl('/api/events/stream'))
  es.addEventListener('event', (m: MessageEvent) => {
    try {
      onEvent(JSON.parse(m.data))
    } catch {}
  })
  return () => es.close()
}
