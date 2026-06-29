# face_auth — Third-party API

Public, versioned HTTP API for triggering face authentication on Hikvision
cameras enrolled in the face_auth system.

All third-party endpoints live under `/api/v1/*` and require an API key.
The admin UI (under `/api/*`) is separate and is not covered here — those
endpoints are for the operator console only.

---

## 1. Concept

```
┌──────────────────┐    1. POST /api/v1/auth/face/start    ┌──────────────────┐
│ Your application │ ────────────────────────────────────► │   face_auth API  │
│ (kiosk, ERP,     │                                       │   (this system)  │
│  POS, etc.)      │ ◄──── { sessionId, expiresAt } ────── │                  │
└──────────────────┘                                       └────────┬─────────┘
        │                                                           │
        │ 2. poll  GET /api/v1/auth/face/{sessionId}                │ 3. unlocks face mode
        │   (or subscribe SSE)                                      │    on the device
        │                                                           ▼
        │                                                  ┌──────────────────┐
        │                                                  │ Hikvision camera │
        │                                                  │  (LAN or via     │
        │                                                  │   face_auth      │
        │                                                  │   agent)         │
        │                                                  └────────┬─────────┘
        │                                                           │
        │ 4. result: status=face_matched / timed_out                │ user presents face
        │ ◄─────────────────────────────────────────────────────────┘
```

Each "session" is a short-lived window (default 10s, configurable in Settings)
during which the device is armed to accept a face match. When the user shows
their face, the device fires an event back to face_auth, which closes the
session with `status=face_matched`.

---

## 2. Authentication

Every request to `/api/v1/*` must carry an API key. Create one in the admin UI
(**Settings → API keys**); the plaintext value is shown **once** at creation.

Pass it in any one of these three ways:

| Style              | Example                                              |
|--------------------|------------------------------------------------------|
| Bearer header      | `Authorization: Bearer fa_xxxxxxxxxxxxxxxxxxxxxxxx`  |
| Custom header      | `X-API-Key: fa_xxxxxxxxxxxxxxxxxxxxxxxx`             |
| Query string       | `?apiKey=fa_xxxxxxxxxxxxxxxxxxxxxxxx`                |

Missing / invalid keys return `401 { "error": "..." }`.

Public API can be globally disabled in **Settings → Public /api/v1 enabled**.
While disabled, `/api/v1/auth/face/start` returns `503`.

---

## 3. QR-2FA toggle

The system supports two operating modes per device:

| Mode             | Toggle               | Behavior                                                                                                |
|------------------|----------------------|---------------------------------------------------------------------------------------------------------|
| **QR required**  | `requireQR2FA: true` | A QR scan (mapped to a person) must precede face matching. The device stays locked until a QR is shown. |
| **Face only**    | `requireQR2FA:false` | The device is always armed. Walking up and presenting a face authenticates the user.                    |

The global default lives in **Settings**. Each device may override it
(**Settings → Per-device overrides**).

When you call `/api/v1/auth/face/start`:

- Supplying a `qrToken`, `personId`, or `employeeNo` identifies the user
  explicitly. Works in both modes.
- Supplying only a `deviceId` is **face-any** mode — succeeds only if the device's
  effective `requireQR2FA` is `false`. If `true`, the call returns `409` with
  `error: "qr_required"`.

---

## 4. Endpoints

### 4.1  `GET /api/v1/ping`

Liveness check.

```http
GET /api/v1/ping HTTP/1.1
Authorization: Bearer fa_xxx
```

```json
{ "ok": true, "service": "face_auth", "time": "2026-05-29T08:00:00Z" }
```

---

### 4.2  `GET /api/v1/devices`

List devices visible to the caller, with their effective QR-2FA mode.

```json
[
  {
    "deviceId": "NEC900196448",
    "name":     "Lobby entry",
    "model":    "DS-K1T804AEF",
    "online":   true,
    "requireQR2FA": false,
    "agentId":  "lobby-agent"
  }
]
```

---

### 4.3  `GET /api/v1/persons`

List people enrolled in the system.

```json
[
  { "id": "p_1", "name": "Alice",   "employeeNo": "1001", "hasQR": true  },
  { "id": "p_2", "name": "Bob",     "employeeNo": "1002", "hasQR": false }
]
```

---

### 4.4  `POST /api/v1/auth/face/start`  ⭐ **the main endpoint**

Open a face-auth window on a device.

**Request**

```json
{
  "deviceId":   "NEC900196448",
  "personId":   "p_1",          // optional — identify user by person id
  "employeeNo": "1001",         // optional — identify user by employee no
  "qrToken":    "raw-qr-text"   // optional — identify user via their QR
}
```

One of `personId`, `employeeNo`, `qrToken` is required when the device has
`requireQR2FA: true`. All three are optional when the device is `face-only`.

**Response (200)**

```json
{
  "id": "fa-9c3f1b8d04",
  "personId": "p_1",
  "employeeNo": "1001",
  "name": "Alice",
  "deviceId": "NEC900196448",
  "openedAt":  "2026-05-29T08:00:00Z",
  "expiresAt": "2026-05-29T08:00:10Z",
  "mode":      "face-only",
  "status":    "open",
  "source":    "api"
}
```

**Errors**

| Status | Body                                            | Meaning                                                       |
|--------|-------------------------------------------------|---------------------------------------------------------------|
| 400    | `{"error": "deviceId required"}`                | Bad request                                                   |
| 400    | `{"error": "unknown QR token"}`                 | QR doesn't match any enrolled person                          |
| 404    | `{"error": "device not found"}`                 | DeviceId is wrong                                             |
| 409    | `{"error": "qr_required", "detail": "..."}`     | This device requires QR; supply qrToken/personId/employeeNo   |
| 503    | `{"error": "public api disabled"}`              | Operator turned off the public API in Settings                |

---

### 4.5  `GET /api/v1/auth/face/{sessionId}`

Poll for session status.

```json
{
  "id": "fa-9c3f1b8d04",
  "status": "face_matched",
  "matchedEmployeeNo": "1001",
  "openedAt":  "2026-05-29T08:00:00Z",
  "expiresAt": "2026-05-29T08:00:10Z",
  ...
}
```

`status` is one of:

| Value           | Meaning                                                          |
|-----------------|------------------------------------------------------------------|
| `open`          | Session is still waiting for a face. Keep polling.               |
| `face_matched`  | The user authenticated successfully. `matchedEmployeeNo` is set. |
| `timed_out`     | Window closed; no face was matched.                              |
| `cancelled`     | Cancelled by the caller or by an admin action.                   |

Sessions are retained in history for ~200 entries. After that, the endpoint
returns `404`.

---

### 4.6  `POST /api/v1/auth/face/{sessionId}/cancel`

Abort an open session early (e.g. user closed your UI tab).

```json
{ "ok": true }
```

`ok` is `false` if the session was already closed.

---

### 4.7  `GET /api/v1/auth/face/stream`

Server-Sent Events feed of every face match the system observes. Useful for
attendance dashboards or door-open hooks.

```
GET /api/v1/auth/face/stream
X-API-Key: fa_xxx
```

Events:

```
event: face_match
data: {"deviceId":"NEC900196448","employeeNo":"1001","receivedAt":"2026-05-29T08:00:08Z"}
```

---

### 4.8  `POST /api/v1/devices/{id}/open-door`

Open the door manually. Useful for "Buzz me in" UX after some other check has
already happened in your app.

Query parameter `?door=N` (default 1) picks which door.

```json
{ "ok": true, "response": "..." }
```

---

### 4.9  `GET /api/v1/devices/{id}/snapshot`

Still JPEG of the current camera frame.

```
Content-Type: image/jpeg
```

Use this if you want a one-shot preview without holding open a stream.

---

### 4.10  Persons CRUD

| Endpoint                                          | Method | Purpose                                                    |
|---------------------------------------------------|--------|------------------------------------------------------------|
| `/api/v1/persons`                                 | GET    | List people                                                |
| `/api/v1/persons`                                 | POST   | Create a person (body: `Person` JSON; name required)       |
| `/api/v1/persons/{id}`                            | GET    | Get a person + enrolled faces                              |
| `/api/v1/persons/{id}`                            | DELETE | Delete a person (`?syncDevice=ID` to also delete on device)|
| `/api/v1/persons/{id}/qr/rotate`                  | POST   | Generate/rotate QR token (returns `{qrToken}`)             |
| `/api/v1/persons/{id}/qr.png?size=256`            | GET    | Render that token as a PNG QR code                         |

Body example for POST `/api/v1/persons`:

```json
{
  "name":       "Alice Tan",
  "employeeNo": "1001",
  "personType": "normal",
  "personRole": "basic",
  "longTerm":   true
}
```

### 4.11  Faces

| Endpoint                                                 | Method | Purpose                                              |
|----------------------------------------------------------|--------|------------------------------------------------------|
| `/api/v1/devices/{id}/faces`                             | GET    | List enrolled faces (local DB)                       |
| `/api/v1/devices/{id}/faces`                             | POST   | Enrol a face. Multipart: `file=<jpeg>`, `personId`   |
| `/api/v1/devices/{id}/faces/{personId}`                  | DELETE | Delete the face from the device                      |

Enrol with curl:

```bash
curl -X POST "$API/api/v1/devices/lobby-1/faces" \
  -H "X-API-Key: $KEY" \
  -F "file=@alice.jpg" \
  -F "personId=p_alice" \
  -F "name=Alice"
```

### 4.12  Device ops

| Endpoint                                  | Method | Purpose                                           |
|-------------------------------------------|--------|---------------------------------------------------|
| `/api/v1/devices/{id}/probe`              | POST   | Test ISAPI reachability                           |
| `/api/v1/devices/{id}/sync-persons`       | POST   | Pull users from the device into local DB          |
| `/api/v1/devices/{id}/face-lib`           | GET    | List faces stored on the device                   |
| `/api/v1/devices/{id}/isapi`              | POST   | Raw ISAPI passthrough — `{method, path, body}`    |

### 4.13  Events

| Endpoint                       | Method | Purpose                                       |
|--------------------------------|--------|-----------------------------------------------|
| `/api/v1/events?limit=100`     | GET    | Recent device events                          |
| `/api/v1/events/stream`        | GET    | SSE stream of every event                     |
| `/api/v1/qr-auth/scan`         | POST   | Submit a QR token (third-party emulating an agent) |

### 4.14  `GET /api/v1/devices/{id}/stream.mjpg`

Continuous MJPEG (multipart/x-mixed-replace) stream. Works in any `<img>`
tag and in `<video>` is not required.

Query parameters:

| Name      | Default | Range  | Meaning                                                |
|-----------|---------|--------|--------------------------------------------------------|
| `fps`     | `4`     | 1..15  | Snapshot-poll rate the server uses to build the stream |
| `seconds` | `0`     | 0..    | Auto-close after N seconds. `0` = stream forever       |

```html
<img src="https://face_auth.example.com/api/v1/devices/NEC900196448/stream.mjpg?fps=8&apiKey=fa_xxx" />
```

> ⚠️ When the API key is passed in the URL it ends up in proxy access logs and
> the browser history. For embedded views, prefer `X-API-Key` via a thin
> server-side proxy.

### 4.15  Enrolment — capture at the device

Ask the reader to acquire a credential live (user presents face / swipes card /
presses finger). All return the device's raw response in `response`.

| Endpoint                                       | Method | Purpose                              |
|------------------------------------------------|--------|--------------------------------------|
| `/api/v1/devices/{id}/capture/face`            | POST   | Capture a live face (`?infrared=true` to also grab IR) |
| `/api/v1/devices/{id}/capture/card`            | POST   | Wait for a card swipe, return its number |
| `/api/v1/devices/{id}/capture/fingerprint`     | POST   | Capture a fingerprint (`?finger=N`)  |

### 4.16  Cards & fingerprints

| Endpoint                                          | Method | Body / params                                   |
|---------------------------------------------------|--------|-------------------------------------------------|
| `/api/v1/devices/{id}/cards`                      | POST   | `{employeeNo, cardNo, cardType?, mode?}` (`mode:"modify"` to update) |
| `/api/v1/devices/{id}/cards/{cardNo}`             | DELETE | —                                               |
| `/api/v1/devices/{id}/fingerprints`               | POST   | `{employeeNo, fingerPrintID?, fingerData}` (base64 template) |
| `/api/v1/devices/{id}/fingerprints/{employeeNo}`  | DELETE | `?finger=N` for one print, omit for all         |

### 4.17  Health & access schedules

| Endpoint                                       | Method | Purpose                                          |
|------------------------------------------------|--------|--------------------------------------------------|
| `/api/v1/devices/{id}/work-status`             | GET    | Door / lock / tamper / battery / capacity (`raw`) |
| `/api/v1/devices/{id}/week-plan/{planNo}`      | PUT    | `{days:[{week,enable,begin,end}, ...]}` per-weekday allow windows |
| `/api/v1/devices/{id}/plan-template/{tplNo}`   | PUT    | `{name?, weekPlanNo}` — bind a template to a week plan |

A week plan defines time windows → a plan template references it → a person's
plan template points at the template. Outside the window the device denies entry.

### 4.18  QR-via-camera (device-native)

| Endpoint                                  | Method | Purpose                                              |
|-------------------------------------------|--------|------------------------------------------------------|
| `/api/v1/devices/{id}/qr-capability`      | GET    | `{supported, raw}` — does the camera read QR?        |
| `/api/v1/devices/{id}/qr-scan`            | POST   | `{enable}` — toggle camera QR scanning               |

> Hardware-gated: many terminals report `supported:false`. When enabled, the
> user's QR must encode their employee number so the device matches it.

### 4.19  Intercom (two-way audio)

| Endpoint                                     | Method | Purpose                                  |
|----------------------------------------------|--------|------------------------------------------|
| `/api/v1/devices/{id}/intercom/channels`     | GET    | Two-way audio channel capabilities       |
| `/api/v1/devices/{id}/intercom/open`         | POST   | Open the intercom channel (`?channel=N`) |
| `/api/v1/devices/{id}/intercom/close`        | POST   | Close the intercom channel               |

> Control plane only: this opens/closes the device's audio channel. Carrying
> microphone/speaker audio in the browser is a separate media-pipeline follow-up.

> ⚠️ All of §4.15–4.19 return the device's **raw** response and depend on the
> device firmware; field names vary. Endpoints work over any reach mode
> (direct / agent / OTAP), though binary pulls (snapshot) need direct/agent.

---

## 5. End-to-end example (face-only mode)

```bash
API="https://face_auth.example.com"
KEY="fa_xxxxxxxxxxxxxxxxxxxxxxxx"

# 1. Open a session — no identifier means "any enrolled user on this device"
SESSION_ID=$(
  curl -s -X POST "$API/api/v1/auth/face/start" \
    -H "X-API-Key: $KEY" \
    -H "Content-Type: application/json" \
    -d '{"deviceId":"NEC900196448"}' | jq -r .id
)

# 2. Poll until it closes
while true; do
  STATUS=$(curl -s "$API/api/v1/auth/face/$SESSION_ID" -H "X-API-Key: $KEY" | jq -r .status)
  [ "$STATUS" != "open" ] && break
  sleep 1
done

# 3. Check the outcome
curl -s "$API/api/v1/auth/face/$SESSION_ID" -H "X-API-Key: $KEY"
```

## 6. End-to-end example (QR + face)

```bash
# 1. Your app reads a QR off a printed lanyard (or in-app QR view).
QR_TOKEN="abc123xyz..."

# 2. Start a session, supplying the QR token. face_auth resolves it → person.
curl -X POST "$API/api/v1/auth/face/start" \
  -H "X-API-Key: $KEY" \
  -H "Content-Type: application/json" \
  -d "{\"deviceId\":\"NEC900196448\",\"qrToken\":\"$QR_TOKEN\"}"

# 3. Poll as in §5.
```

The QR token is the value encoded in `/api/persons/{id}/qr.png` — your
application typically displays this QR to the user, the user scans it at a
kiosk, and your kiosk software POSTs the decoded text here.

---

## 7. Recommended integration patterns

- **Kiosk / single-purpose terminal**: open a session, embed the MJPEG `<img>`,
  poll `/auth/face/{id}` every second. Drop the camera view as soon as
  `status != "open"`.
- **Backend-to-backend**: start a session, subscribe to
  `GET /auth/face/stream` once on process startup, and correlate `face_match`
  events to your session ids. Avoids per-session polling.
- **Door buzzer**: in `face-only` mode, leave the device armed and just listen
  to the SSE stream. No session needed — every face match is an event.

---

## 8. Operational notes

- The face-auth window length is configurable globally in **Settings**
  (default 10 s, max 120 s). Don't set it too long — while a session is open
  for a specific person, that user's verify-mode is `face`, meaning anyone
  presenting their face can authenticate as them.
- API keys are stored hashed... actually they're stored plaintext today (it's
  a token-style key, not a password). Treat them like secrets.
- The shared agent at `/agent/ws` is unrelated to API keys — it uses a separate
  agent token. See the Agents tab in the admin UI for details.

