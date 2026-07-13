# QR-only access

QR-only access makes FaceAuth the authentication authority and uses Hikvision only as the physical door relay.

## Two separate access paths

Do not combine FaceAuth QR-only with Hikvision QR/card authentication for the same scan workflow.

### FaceAuth QR-only

- The QR is issued and verified by FaceAuth.
- The USB scanner is connected to the FaceAuth agent PC.
- Hikvision receives only the final `OpenDoor` relay command after FaceAuth approves access.
- The QR must not be pushed to the Hikvision camera or treated as a Hikvision card number.

### Hikvision QR/card

- The card number or card-QR credential is enrolled on the selected Hikvision device.
- The Hikvision reader/camera receives and verifies the credential.
- FaceAuth keeps the member and device enrollment records but does not use the QR-only direct-open path for that scan.
- Existing Hikvision card enrollment, Card QR generation, device sync, device-native QR APIs, and manual open-door controls remain available.
- FaceAuth generates the Card QR with the enrolled Hikvision `cardNo` as its exact payload.
- The connected device advertises `cardNo` length `1..20`; FaceAuth rejects longer values before enrollment or QR generation.

## Per-device access mode

Choose the authoritative path under **Settings → Per-device access mode**. The setting is merchant-scoped and stored on the device.

- **Normal QR (FaceAuth relay)** maps linked scanner stations to `faceauth_qr`. FaceAuth accepts only its own static/dynamic token, evaluates access, and sends `OpenDoor`. Native Hikvision users are kept locked so face, card, and built-in camera QR cannot bypass FaceAuth.
- **Hikvision QR / face / card** maps linked stations to `hikvision`, blocks the third-party FaceAuth direct-open path, and applies `cardOrFace` to enrolled users. Physical card and a QR containing that same enrolled `cardNo` are the same native credential.

Changing the mode updates all access points targeting that device and reapplies the matching user verification baseline. It never enables a disabled station and never opens a door.

## Access flow

1. A member presents a FaceAuth-issued QR to the USB scanner connected to the agent PC.
2. The QR watcher sends the scanned value to the local agent.
3. The agent authenticates itself to the FaceAuth backend and submits the QR, agent ID, and target device ID.
4. FaceAuth resolves the member and checks tenant ownership, member validity, access plan, schedule, credits, and re-entry rules.
5. If access is allowed, FaceAuth records the member decision and sends Hikvision only an `OpenDoor` command.
6. Hikvision does not perform face, card, fingerprint, or QR verification in this mode.

Hikvision records the physical command as **Remote Open** because the device received a remote relay command. The authoritative member-authentication record is the FaceAuth **Access Log**, where the method is recorded as `qr`.

## Choose QR mode when adding a member

Open **Persons → Add person → Access path** and first choose **FaceAuth QR-only**. Then choose one FaceAuth QR type.

### Static QR

- FaceAuth generates one permanent random QR token when the member is created.
- The token remains valid until an administrator rotates it.
- Use **Rotate token** in the member's QR Access section to invalidate the old QR and issue a new one.
- Static QR is appropriate for permanent members carrying a printed badge.

### Dynamic QR

- FaceAuth generates a signed QR tied to the member and the current server time window.
- Choose a rotation interval when adding the member: 1, 3, 5, or 10 minutes.
- For a 3-minute setting, the displayed QR automatically changes every 180 seconds.
- The previous QR stops working as soon as its time window ends.
- The member detail screen refreshes the QR automatically at the exact expiry boundary.
- **Rotate secret now** immediately invalidates the current QR series and starts a new one.
- Dynamic QR is appropriate for mobile member passes and reduces copied-QR reuse.

Dynamic QR validation is performed entirely by FaceAuth using a per-member private signing secret. The secret is never placed in the displayed QR or returned to the browser.

## Agent configuration

Open the agent dashboard at `http://127.0.0.1:7780` and configure:

- **FaceAuth QR-only**: enabled
- **Target device ID**: the Hikvision door device controlled by this agent
- **QR listen port**: normally `7771`

The target device must belong to the same tenant and be assigned to the connected agent.

## Multi-merchant, multi-outlet, and multi-agent topology

- Every access point belongs to one merchant and one outlet.
- One outlet may own multiple agents.
- One agent may expose multiple scanner stations on different ports.
- Each scanner station maps to one access point, Hikvision device, door number, and direction.
- Cross-merchant and cross-outlet agent/device assignments are rejected.
- Duplicate scanner ports or scanner IDs on the same agent are rejected.

Configure this under **Access points** in the admin UI. New access points default to disabled until explicitly enabled.

Example:

```text
Outlet A
├── Agent 1 :7771 → Front entry → Device A / Door 1 / in
├── Agent 1 :7772 → Main exit  → Device B / Door 1 / out
└── Agent 2 :7771 → Rear entry  → Device C / Door 1 / in
```

## API, logs, and reports

Versioned API-key endpoints:

- `GET/POST /api/v1/access-points`
- `PUT/DELETE /api/v1/access-points/:id`
- `GET /api/v1/access-log`
- `GET /api/v1/reports/access-summary`

API keys are merchant-scoped. Access logs include outlet, access point, agent, scanner, device, door, member snapshot, QR mode, direction, decision, reason, door result, and correlation ID. The admin Reports page provides entry, exit, denial, occupancy, and unique-member totals plus CSV export.

## Scanner and diagnostics

### Multiple scanners on one Windows agent

The Windows agent uses a native hybrid reader for device-bound scanner routing.
It prefers a scanner's HID POS interface (`MI_01`, usage page `0x8C`) and reads
decoded barcode reports directly. Long values are reassembled using the HID POS
`Decode Data Continued` flag. If HID POS is unavailable, the agent falls back
to Windows Raw Input on the scanner's keyboard interface (`MI_00`). Open
the agent dashboard at `http://127.0.0.1:7780`, select **QR scanner**, then:

1. Connect the USB scanners and click **Refresh devices**.
2. Click **Identify scanner** and scan a test QR with the intended physical scanner.
3. Assign that detected hardware device to an enabled FaceAuth QR access point.
4. Repeat for every scanner. Each access point already defines its local port,
   Hikvision device, door number, and direction.

The dashboard labels **HID POS direct** as preferred and **Raw Input fallback**
for keyboard-only devices. The preferred identity is the scanner's USB serial number together with its
vendor and product IDs. When the scanner exposes no serial number, Windows uses
its full HID device-instance path; moving it to another USB socket can require
rebinding. Unknown or unassigned keyboards and scanners are never routed to a
door. Once a native binding exists, the agent stops the legacy global
AutoHotkey watcher to prevent the same scan being submitted twice.

To replace hardware, unbind the disconnected scanner, identify the replacement,
and bind it to the same scanner/access-point ID. The port, target Hikvision
device, door, direction, and QR policy remain unchanged.

The local agent dashboard also provides **Scanner stations** for direct local
configuration. Each station requires a unique local HTTP port (for example,
`7771` and `7772`), Hikvision device ID, door number, and direction. Under
**Bind Windows USB port to station**, select the detected HID POS or Raw Input
device and bind it to the station. The resulting route is:

`Windows USB/HID device path -> station -> local HTTP port -> device and door`.

Each agent PC supports a maximum of four scanner stations.

Scanner stations are exclusively for third-party USB/HID QR or barcode
scanners connected to the agent PC. They are stored as
`scanner_source: third_party_usb`. Never add a Hikvision terminal's built-in
QR/card scanner as an agent scanner station. Hikvision-native QR/card remains a
separate `hikvision` access-point mode: the terminal performs its own credential
verification and the agent scanner watcher, local HTTP ports, HID POS reader,
and Raw Input reader are not involved.

Local ports must be unique on the agent, must be between 1 and 65535, and cannot
use the agent dashboard port `7780`. When a station is deleted or its port is
changed, the previous listener immediately fails closed and returns scanner not
ready rather than retaining the former door route.

- Scanner submission endpoint: `POST http://127.0.0.1:7771/scan`
- QR Debug page: `http://127.0.0.1:7771/qrdebug`
- Agent dashboard: `http://127.0.0.1:7780`

QR Debug shows the scanned QR preview, resolved member name, HTTP result, door-open result, and FaceAuth response.

The agent PC plays a double rejection beep when FaceAuth returns an expired QR, unknown QR, access denial, or other failed scan. Successful scans do not play this error sound. USB-HID scanners do not expose a universal command for controlling their built-in buzzer, so the rejection sound comes from the agent PC's audio output.

Common failures:

- `unknown face_auth QR`: the value is not assigned by FaceAuth, the static token was rotated, or the dynamic QR expired.
- `access expired`: the member's validity end time has passed.
- `access not started`: the member's validity start time has not arrived.
- `access denied`: the member's plan, schedule, credit, or re-entry rule denied access.
- `device is not assigned to this agent`: the configured target device and connected agent do not match.
- `QR person and target device are not in the same tenant`: the member and door belong to different tenants.

## Security notes

- Do not use Hikvision card numbers or arbitrary test QR values for QR-only access.
- Only QR values issued by FaceAuth are accepted.
- Agent credentials are verified before direct-open requests are processed.
- Dynamic QR codes are time-bound and cryptographically signed.
- FaceAuth access policy is checked before every `OpenDoor` command.
