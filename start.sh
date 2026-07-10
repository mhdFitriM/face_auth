#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AGENT_BIN="$SCRIPT_DIR/face_auth-agent"
AGENT_LOG="$SCRIPT_DIR/agent.log"
AGENT_PID_FILE="$SCRIPT_DIR/.agent.pid"

# ── helpers ──────────────────────────────────────────────────────────────────

log() { echo "[start.sh] $*"; }

stop_agent() {
  if [[ -f "$AGENT_PID_FILE" ]]; then
    local pid
    pid=$(cat "$AGENT_PID_FILE")
    if kill -0 "$pid" 2>/dev/null; then
      log "Stopping existing agent (pid $pid)…"
      kill "$pid" 2>/dev/null || true
      sleep 1
    fi
    rm -f "$AGENT_PID_FILE"
  fi
  # Also kill any stray agent processes by binary name.
  pkill -f "face_auth-agent" 2>/dev/null || true
}

# ── Docker stack ──────────────────────────────────────────────────────────────

log "Starting Docker stack…"
cd "$SCRIPT_DIR"
docker compose up -d --build

# Wait for backend to be ready.
log "Waiting for backend (http://localhost:8080/health)…"
for i in $(seq 1 30); do
  if curl -sf http://localhost:8080/health >/dev/null 2>&1; then
    log "Backend is up."
    break
  fi
  if [[ $i -eq 30 ]]; then
    log "WARNING: backend did not respond after 30s — starting agent anyway."
  fi
  sleep 2
done

# ── Build agent ───────────────────────────────────────────────────────────────

log "Building agent binary…"
cd "$SCRIPT_DIR/backend"
go build -o "$AGENT_BIN" ./cmd/agent
cd "$SCRIPT_DIR"

# ── Start agent ───────────────────────────────────────────────────────────────

stop_agent

log "Starting LAN agent (log → $AGENT_LOG)…"
"$AGENT_BIN" --service >> "$AGENT_LOG" 2>&1 &
echo $! > "$AGENT_PID_FILE"
log "Agent started (pid $(cat "$AGENT_PID_FILE"))."

log ""
log "  Stack:  http://localhost:5173  (admin UI)"
log "  API:    http://localhost:8080"
log "  Agent:  http://127.0.0.1:7780  (dashboard)"
log "  Log:    tail -f $AGENT_LOG"
log ""
log "To stop everything: ./stop.sh"
