#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AGENT_PID_FILE="$SCRIPT_DIR/.agent.pid"

# Stop agent
if [[ -f "$AGENT_PID_FILE" ]]; then
  pid=$(cat "$AGENT_PID_FILE")
  if kill -0 "$pid" 2>/dev/null; then
    echo "[stop.sh] Stopping agent (pid $pid)…"
    kill "$pid"
  fi
  rm -f "$AGENT_PID_FILE"
fi
pkill -f "face_auth-agent" 2>/dev/null || true

# Stop Docker stack
echo "[stop.sh] Stopping Docker stack…"
cd "$SCRIPT_DIR"
docker compose down

echo "[stop.sh] Done."
