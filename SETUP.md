# face_auth — Setup Guide

Step-by-step setup for operators: bring the stack up, log in, connect a device, and enrol your first user. For the feature reference see [README.md](README.md); for the third-party API see [API.md](API.md).

---

## 1. Prerequisites

- **Docker** + Docker Compose (Docker Desktop on Mac/Windows).
- A **Hikvision face access terminal** (e.g. DS-K1T-series) on a network you can reach.
- The terminal's **IP, ISAPI username and password** (default user is usually `admin`).

---

## 2. Start the stack

```bash
cd "Hkvisson face app/face_auth"
docker compose up -d            # postgres, redis, minio, backend, admin
docker compose ps               # all should be "running"/"healthy"
```

Services and ports:

| Service | Port | What |
|---|---|---|
| admin (web UI) | `5173` | operator console — open http://localhost:5173 |
| backend (API) | `8080` | REST API (`/api/*` admin, `/api/v1/*` third-party) |
| device push | `7660` / `7670` | OTAP dial-in (plain / TLS) |

After any code change: `docker compose up -d --build backend admin`.

---

## 3. Log in

Open **http://localhost:5173** and sign in.

- Seeded HQ admin: **`hq@faceauth.local`** / **`changeme`** (change it after first login).
- An HQ admin works across tenants; tenant admins are scoped to their tenant.

---

## 4. Choose how the platform reaches the device

This is the single most important decision. Set it per device via **Reach via** in the Add/Edit Device form.

| Reach | Use when | Needs |
|---|---|---|
| **Direct** | The backend can reach the device's IP directly (same LAN) | device IP + ISAPI credentials |
| **Agent** | The backend is elsewhere (cloud / different subnet) and can't reach the device | a LAN agent (§6) |
| **OTAP push** | You want zero LAN presence — the device dials out to the backend | device firmware that supports OTAP + PUSHCfg pointed at this server |

> WiFi vs cable doesn't matter — what matters is whether the backend can reach the device's IP. If it can't, use Agent or OTAP.

---

## 5. Add a device (Direct)

1. **Devices → Add device.**
2. Fill in **Device ID** (serial works), **Name**, **IP**, **Port** (80), **Username**, **Password**.
3. **Reach via → Direct ISAPI.**
4. **Save & probe** — you should see `reachable: true` with the device model/firmware.

If the probe fails, the backend can't reach the IP → switch to **Agent** (§6) or fix the network.

---

## 6. Add a device via a LAN agent

Use this when the backend can't reach the device directly.

1. **Agents → Add agent** → note the generated **Agent ID** and **Token** (token shown once).
2. In the **Agent binaries** card, download the binary for the agent host's OS (macOS Intel/ARM, Linux x64/ARM64/ARMv7, Windows). *(Downloads are public — no token needed.)*
3. On a machine on the **device's LAN**, run it with the env vars shown in the UI:
   ```bash
   chmod +x face_auth-agent-linux-amd64
   CLOUD_URL=wss://<your-server> AGENT_ID=<id> AGENT_TOKEN=<token> ./face_auth-agent-linux-amd64
   ```
4. The agent appears as **connected** in the Agents tab.
5. **Devices → Add device → Reach via → Via agent**, pick the agent. No device IP needs to be reachable from the backend.

---

## 7. Add a device via OTAP push (no agent)

1. On the **device's own web page**: `Configuration → Network → Advanced → Platform Access` (a.k.a. ISUP / Push). Point it at this server's push host on `:7660` (or `:7670` TLS).
2. Confirm dial-in:
   ```bash
   docker compose logs -f backend | grep -iE "PUSH|AuthInfo|Login|/iot/"
   ```
   You want `AuthInfo` → `Login` → periodic `CommandRequest`.
3. **Devices → Reach via → OTAP push.** Commands now ride the dial-in queue.

> Firmware caveat: if the device has no Platform Access / ISUP page or never dials in, OTAP isn't supported on that firmware — use Direct/Agent.

---

## 8. Enrol your first user

1. **Persons → New person** (name, employee number, role).
2. **Enrol** tab → pick the **device** and **person**, then either:
   - **Upload a photo** / **Use camera**, then **Enrol**, or
   - **Capture at the device (reader)** → *Capture face* (user looks at the terminal). You can also *Capture card* / *Capture fingerprint*, and *Bind card to person*.
3. The face now lives on the device; the person can authenticate.

---

## 9. Optional: access schedules & health

- **Devices → Schedule** — set per-weekday allow windows (e.g. 09:00–18:00). This writes a week plan + plan template; assign the template to a person to enforce *when* they may enter.
- **Devices → Health** — door / lock / tamper / battery / capacity at a glance.

---

## 10. Optional: live view & intercom

- **Devices → Live** — live MJPEG view, grab a snapshot, and **Intercom call** (opens the device's two-way audio channel).

---

## 11. Third-party / programmatic access

Everything above is also available under **`/api/v1/*`** with an API key (issue keys in Settings). See [API.md](API.md) — the headline is `POST /api/v1/auth/face/start` to arm a face-auth session, plus enrolment, schedules, health, QR and intercom endpoints (§4.15–4.19).

---

## Troubleshooting

| Symptom | Likely cause / fix |
|---|---|
| Probe fails / "agent not connected" | Backend can't reach the device, or the agent isn't running. Check the agent is **connected** in Agents; verify the device IP is reachable from the agent host. |
| Door command does nothing right after a redeploy | The agent's WebSocket drops on backend restart and reconnects in ~8s. Wait ~10s. |
| "permission expired" on face | The device is in QR-2-step mode; a QR scan must flip the user's validity window before face works. |
| Capture/schedule/QR returns a 4xx in `response` | That feature isn't supported on the device firmware, or a field name differs — inspect the raw `response`. |
| Download link does nothing | Agent downloads are public; if blocked, check the backend is up at `:8080`. |

> ⚠️ Destructive actions (deleting devices, faces, cards, fingerprints; clearing faces on a live door) are irreversible on shared hardware — confirm scope before running them.
