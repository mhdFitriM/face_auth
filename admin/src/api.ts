// In dev (docker compose locally), VITE_API_URL points at the backend's exposed port.
// In prod behind the shared reverse proxy, leave it empty — same-origin requests work fine.
const API = (import.meta.env.VITE_API_URL as string) ?? ''

export const apiUrl = (path: string) => `${API}${path}`

async function req<T = any>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(apiUrl(path), init)
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
