# face_auth — feature & ops reference

Hikvision face-access platform: Go (Fiber) backend, React/TS (Vite) admin, Postgres + Redis + MinIO, deployed with Docker Compose.

This README tracks the features added on top of the base app and how to run/verify them. See also [API.md](API.md) and [design.md](design.md).

### Feature status

| Area | Built | Hardware-tested |
|---|---|---|
| **Phase 1** — OTAP push transport (agent-free) | ✅ | ⏳ device offline |
| **Phase 2** — Better enrolment (capture-at-device, cards, fingerprints) | ✅ | ⏳ |
| **Phase 3** — Health & access schedules | ✅ | ⏳ |
| **Phase 4** — Live video & intercom (live view already existed; +snapshot-on-event, +intercom control) | ✅ | ⏳ |
| QR-via-camera (device-native) | ✅ | ⏳ likely `notSupport` on V4.38.0 |

All code compiles and is deployed; every new ISAPI call returns the device's **raw response** so behavior is honest when run against the live device. Nothing below is verified on hardware yet — the test device has been offline. Field-name mismatches per firmware are possible; bring the device online and exercise each feature to confirm.

Every feature is exposed two ways: the **admin UI** (`/api/*`, session auth) and the **third-party API** (`/api/v1/*`, API key) — see [API.md](API.md) §4.15–4.19. Setup steps are in [SETUP.md](SETUP.md); a visual walkthrough is the **User Guide** at [user-docs/user-guide.html](user-docs/user-guide.html) (self-contained — open it in any browser).

---

## Command transports — how the platform reaches a device

A device can be commanded three ways. This is set per device via the **Reach via** selector in the admin Devices form, and stored in `devices.reach`.

| Reach (`devices.reach`) | How it works | When to use |
|---|---|---|
| `direct` (default) | Backend calls the device's ISAPI over HTTP Digest on the LAN | Backend shares the device's LAN |
| `agent` | An on-prem agent holds an outbound WebSocket to the backend hub, which proxies ISAPI calls (`agent_id` set) | Device behind NAT, agent on its LAN |
| `otap` | **Device dials out** to our push listener; commands ride the OTAP push queue — no agent or inbound reach needed | Cloud deploys, no LAN presence |

All command methods funnel through `ISAPIClient.Do()` in [backend/internal/isapi.go](backend/internal/isapi.go), which picks the route. New ISAPI methods inherit all three transports automatically.

---

## Phase 1 — OTAP push transport (agent-free commanding)

**Problem solved:** the OTAP PUSHSDK *server* (device dial-in: AuthInfo / Login / CommandRequest / CommandResult / Event) was already implemented in [backend/internal/push.go](backend/internal/push.go), but the command layer never delivered commands through that queue — so the device could dial in, but we couldn't command it over OTAP. Phase 1 closes that gap.

**What changed**
- [backend/internal/isapi.go](backend/internal/isapi.go) — new `doViaOTAP()`: enqueues the ISAPI request (`store.EnqueueCommand`) and blocks on `store.AwaitCommandResult` (both already existed in [store.go](backend/internal/store.go)). `Do()` routes OTAP → agent → direct. `ISAPIClient` gained `OTAP`, `DeviceID`, `store`.
- [backend/internal/agent_hub.go](backend/internal/agent_hub.go) — `AgentHub` now carries `*Store` (wired in [cmd/server/main.go](backend/cmd/server/main.go)), so every existing `NewISAPIClientForDevice(d, hub)` call site gets OTAP for free.
- [backend/internal/models.go](backend/internal/models.go) + [store.go](backend/internal/store.go) — `Device.Reach` field, `reach` column migration, read/write SQL.
- [backend/internal/config.go](backend/internal/config.go) — default `COMMAND_INTERVAL` lowered **5s → 2s** so OTAP round-trips feel responsive (the device polls this often).
- Admin: **Reach via** selector in the device form ([admin/src/App.tsx](admin/src/App.tsx)), `reach` on `DeviceForm` ([admin/src/api.ts](admin/src/api.ts)), and an "OTAP push" badge in the device list.

**How to enable OTAP on a device**
1. On the device webpage, set **PUSHCfg / platform access** to dial this server's push host on `:7660` (plain) or `:7670` (TLS).
2. Confirm dial-in: `docker compose logs -f backend | grep -iE "PUSH|AuthInfo|Login|/iot/"`. You want `AuthInfo` → `Login` → periodic `CommandRequest`.
3. In admin, set the device's **Reach via → OTAP push**.
4. Trigger any action (e.g. open door) — it round-trips `EnqueueCommand → CommandRequest → CommandResult → AwaitCommandResult` with no agent.

> ⚠️ **Firmware caveat:** the test device reports **V4.38.0**; the OTAP demo targets **v4.48.0**. If the device never dials in (step 2), OTAP isn't usable on that firmware — Direct/Agent remain the fallback and the OTAP code stays dormant/harmless.

---

## Phase 2 — Better enrolment (capture-at-device + cards + fingerprints)

**Goal:** enrol credentials by capturing them *live at the reader* (face shown / card swiped / finger pressed), not just by uploading a photo — plus write cards and fingerprints.

**Backend ISAPI methods** — [backend/internal/isapi.go](backend/internal/isapi.go) (all route via OTAP/agent/direct):

| Method | ISAPI endpoint |
|---|---|
| `CaptureFaceData(infrared)` | `POST /ISAPI/AccessControl/CaptureFaceData` |
| `CaptureCardInfo()` | `POST /ISAPI/AccessControl/CaptureCardInfo` |
| `CaptureFingerPrint(fingerNo)` | `POST /ISAPI/AccessControl/CaptureFingerPrint` |
| `SetCardInfo(emp, cardNo, type, mode)` | `POST/PUT /ISAPI/AccessControl/CardInfo/{Record,Modify}` |
| `DeleteCard(cardNo)` | `PUT /ISAPI/AccessControl/CardInfo/Delete` |
| `SetFingerPrint(emp, id, base64)` | `POST /ISAPI/AccessControl/FingerPrintCfg` |
| `DeleteFingerPrint(emp, id)` | `PUT /ISAPI/AccessControl/FingerPrintDelete` |

**HTTP routes** — [backend/internal/api.go](backend/internal/api.go) (admin-auth; a shared `reachable()` guard allows OTAP devices that have no IP):
```
POST   /api/devices/:id/capture/face[?infrared=true]
POST   /api/devices/:id/capture/card
POST   /api/devices/:id/capture/fingerprint[?finger=N]
POST   /api/devices/:id/cards                 {employeeNo, cardNo, cardType?, mode?}
DELETE /api/devices/:id/cards/:cardNo
POST   /api/devices/:id/fingerprints          {employeeNo, fingerPrintID?, fingerData}
DELETE /api/devices/:id/fingerprints/:employeeNo[?finger=N]
```

**Admin UI** — [admin/src/App.tsx](admin/src/App.tsx) EnrolTab now has a **"Capture at the device (reader)"** card: buttons for Capture face / card / fingerprint, plus a "Bind card to person" field. API client methods are in [admin/src/api.ts](admin/src/api.ts) (`captureFace`, `captureCard`, `captureFingerprint`, `setCard`, `deleteCard`, `setFingerprint`, `deleteFingerprint`).

> ⚠️ Exact ISAPI request/response shapes vary by firmware. These follow the ISAPI Access Control spec and return the device's **raw response** so you can see exactly what came back. Verify against the live device and adjust field names if a firmware rejects a body. Not yet tested on hardware (device was offline).

---

## Phase 4 — Live video & intercom

**Goal:** live preview, snapshots-on-event, and operator intercom.

**Already present (base app):** live view via the MJPEG re-multiplexer — `GET /api/devices/:id/stream.mjpg` and `/snapshot`, built on `GetSnapshot`, surfaced by `api.mjpegUrl` / `api.snapshotUrl` and the `SnapshotImg` component.

**Added in Phase 4:**
- **Snapshot-on-event** — [backend/internal/push.go](backend/internal/push.go) `captureSnapshotForEvent`: when an event arrives with no embedded image, a live snapshot is pulled **asynchronously** and attached (`store.UpdateEventImage`). Only for direct/agent reach — OTAP's command queue can't carry binary frames. `NewPushServer` now takes the `*AgentHub`.
- **Intercom (two-way audio) control plane** — [backend/internal/isapi.go](backend/internal/isapi.go): `GetTwoWayAudioChannels`, `OpenTwoWayAudio`, `CloseTwoWayAudio`.
  ```
  GET  /api/devices/:id/intercom/channels
  POST /api/devices/:id/intercom/open[?channel=N]
  POST /api/devices/:id/intercom/close[?channel=N]
  ```
- **Admin** — a **Live** button on each device row opens `LiveViewModal` ([admin/src/App.tsx](admin/src/App.tsx)): MJPEG live frame, "Open snapshot", and **Intercom call / Hang up** (auto-closes the channel on unmount). Client methods `api.intercomChannels/Open/Close`.

> ⚠️ Intercom here is the **control plane** — it opens/closes the device's audio channel. Carrying microphone/speaker audio (G.711) into the browser is a follow-up that needs the device online to wire the media pipeline (`/ISAPI/System/TwoWayAudio/channels/<id>/audioData`). Full H.264 WebSocket live view (vs MJPEG) is likewise a firmware-dependent follow-up. Not yet tested on hardware.

---

## Phase 3 — Health & access schedules

**Goal:** surface device health, and control *when* a person is allowed through (replacing the year-2000/2037 validity-window hack with real week-plan templates).

**Backend ISAPI methods** — [backend/internal/isapi.go](backend/internal/isapi.go):

| Method | ISAPI endpoint |
|---|---|
| `GetAcsWorkStatus()` | `GET /ISAPI/AccessControl/AcsWorkStatus` |
| `SetWeekPlan(planNo, days)` / `GetWeekPlan(planNo)` | `PUT/GET /ISAPI/AccessControl/UserRightWeekPlanCfg/<planNo>` |
| `SetPlanTemplate(tplNo, name, weekPlanNo)` / `GetPlanTemplate(tplNo)` | `PUT/GET /ISAPI/AccessControl/UserRightPlanTemplate/<tplNo>` |

**HTTP routes** — [backend/internal/api.go](backend/internal/api.go):
```
GET  /api/devices/:id/work-status
GET  /api/devices/:id/week-plan/:planNo
PUT  /api/devices/:id/week-plan/:planNo        {days: [{week, enable, begin, end}, ...]}
PUT  /api/devices/:id/plan-template/:tplNo     {name?, weekPlanNo}
```

**Admin UI** — [admin/src/App.tsx](admin/src/App.tsx), buttons on each device row:
- **Health** (`HealthModal`) — polls `AcsWorkStatus`, shows door / magnetic / tamper / battery / capacity, plus raw JSON.
- **Schedule** (`ScheduleModal`) — a per-weekday allow-window editor that writes a week plan + plan template to the device. Default 09:00–18:00 weekdays.

**How schedules enforce access:** a *week plan* defines per-day time windows → a *plan template* references the week plan → a *person's plan template* (the `PlanTemplate` field on `HikUserInfo`) points at that template. Outside the window the device itself denies entry.

> ⚠️ Field shapes are firmware-dependent; methods return the device's raw response. The schedule editor writes one time segment per weekday (segment id 1). The legacy validity-window mechanism in [qr_auth.go](backend/internal/qr_auth.go) is left intact — migrate persons to plan templates incrementally. Not yet tested on hardware (device offline).

---

## QR-via-camera (device-native) — implemented, pending hardware verification

**Goal:** use the **Hikvision device's own camera** to read the user's QR (replacing the third-party USB scanner). The user presents their QR to the terminal and enters.

**Backend ISAPI methods** — [backend/internal/isapi.go](backend/internal/isapi.go):

| Method | ISAPI endpoint |
|---|---|
| `GetAccessControlCapabilities()` | `GET /ISAPI/AccessControl/capabilities` |
| `SupportsCameraQR()` | parses caps for `isSupportScanQRCode` / `isSupportQRCode` / `QRCode` flags → `(supported, raw)` |
| `SetQRScanEnabled(enable)` | `PUT /ISAPI/AccessControl/QRCodeCfg` |

**HTTP routes** — [backend/internal/api.go](backend/internal/api.go):
```
GET  /api/devices/:id/qr-capability    -> {supported, raw}
POST /api/devices/:id/qr-scan          {enable: true|false}
```

**Admin UI** — a **"Camera QR"** button on each device row opens a modal ([admin/src/App.tsx](admin/src/App.tsx) `QRCameraModal`) that probes firmware support on open, shows supported / not-supported, and lets you enable/disable scanning. Client methods: `api.qrCapability`, `api.setQrScan`.

> ⚠️ **Hardware-gated.** An earlier probe found this device reports `QRCode = notSupport` — the V4.38.0 firmware likely can't read QR via its camera. The modal probes capability and shows the result; **Enable** is disabled when the firmware reports no support. The `SetQRScanEnabled` endpoint shape is firmware-dependent and returns the device's raw response. Verify on the live device:
> ```bash
> curl -s "http://localhost:8080/api/devices/GM5989885/qr-capability" \
>   -H "Authorization: Bearer $TOKEN" -H "X-Tenant-Id: ten_default"
> ```
> If `supported:false`, fall back to a web/tablet kiosk camera (jsQR → existing `/api/qr-auth/scan`) or the USB scanner. **After enabling, the user's QR must encode their employee number** so the device matches it to the enrolled user.

**Existing scanner QR flow (still available):** third-party USB HID scanner → agent → `/api/qr-auth/scan` → `qrAuth.Scan()` flips the person's validity window to allow → user then verifies by face (QR + face 2FA). See [backend/internal/qr_auth.go](backend/internal/qr_auth.go).

---

## Running locally

```bash
cd "Hkvisson face app/face_auth"
docker compose up -d                          # postgres, redis, minio, backend, admin
docker compose up -d --build backend admin    # rebuild after code changes
docker compose logs -f backend                # tail backend
```

Services: backend API `:8080`, admin `:5173` (Vite), device push `:7660` (HTTP) / `:7670` (TLS), raw TCP dump `:7661`.

**Database** (compose): `postgres://hik:hikpush@postgres:5432/hikpush`
```bash
docker compose exec postgres psql -U hik -d hikpush -c "SELECT device_id, reach, online FROM devices;"
```

**Admin login** (seeded HQ admin): `hq@faceauth.local` / `changeme`. HQ endpoints need a tenant scope — pass `?tenantId=ten_default` or header `X-Tenant-Id: ten_default`.
```bash
TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"hq@faceauth.local","password":"changeme"}' \
  | sed -E 's/.*"token":"([^"]+)".*/\1/')
```

**On-prem agent** (for `agent` reach; must run on the device's LAN):

The easiest path is the **Agents** tab in the admin: click **Add agent** to generate an id + token, then download the matching binary from the **Agent binaries** card (one per platform: macOS Intel/ARM, Linux x64/ARM64/ARMv7, Windows) and run it with the env vars shown. The binaries are cross-compiled into `/app/agents` by [backend/Dockerfile](backend/Dockerfile) and served by `GET /api/agents/downloads/:file`.

> The agent binaries and companion scripts are served **without auth** (`/api/agents/downloads*` and `/api/agents/scripts/*` are public in the auth middleware) — they're generic, secret-free files; the per-agent token is what's secret and is generated separately. All other `/api/*` routes remain session-gated.

Manual build (e.g. for this Mac):
```bash
cd backend && go build -o /tmp/wf_agent ./cmd/agent
CLOUD_URL=ws://<backend-host>:8080 AGENT_ID=mac-lan AGENT_TOKEN=<token from agents table> \
  nohup /tmp/wf_agent >/tmp/wf_agent.log 2>&1 &
```
Run with `nohup` (not a harness background task — those get torn down). The device target/credentials come per-request from the backend, so the agent only needs the cloud URL + its own id/token.

### Verify (build only — no device side effects)
```bash
cd backend && go build ./... && go vet ./internal/...
cd ../admin && node node_modules/typescript/bin/tsc --noEmit
```

---

## ⚠️ Safety constraints

- **Destructive operations** (deleting devices, faces, cards, fingerprints; clearing faces on a live door) require **explicit user consent** — they are irreversible on shared hardware. Confirm scope first.
- After redeploying the backend, the agent's WebSocket drops and auto-reconnects in ~8s. Wait ~10s before expecting door/command actions to work.
