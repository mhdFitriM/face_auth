const API = (import.meta.env.VITE_API_URL as string) || 'http://localhost:8080'

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
