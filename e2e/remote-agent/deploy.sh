#!/bin/bash
# Build, deploy, and start the remote agent on the target host.
# Usage: ./deploy.sh [user@host] [port]
#   e.g.: ./deploy.sh tony@192.168.1.180 9182

set -euo pipefail

TARGET="${1:-tony@192.168.1.180}"
PORT="${2:-9182}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY="$SCRIPT_DIR/remote-agent"

echo "Building remote-agent for linux/amd64..."
cd "$SCRIPT_DIR"
GOOS=linux GOARCH=amd64 go build -o "$BINARY" .

echo "Deploying to $TARGET..."
scp "$BINARY" "$TARGET:/tmp/remote-agent"

echo "Starting remote-agent on port $PORT..."
ssh "$TARGET" "pkill -f '/tmp/remote-agent' 2>/dev/null || true; sleep 0.3; PORT=$PORT nohup /tmp/remote-agent > /tmp/remote-agent.log 2>&1 & disown"

sleep 1

# Verify it's running
HOST=$(echo "$TARGET" | cut -d@ -f2)
if curl -sf "http://$HOST:$PORT/health" > /dev/null 2>&1; then
    echo "Remote agent is running at http://$HOST:$PORT"
else
    echo "ERROR: Remote agent failed to start. Logs:"
    ssh "$TARGET" "cat /tmp/remote-agent.log"
    exit 1
fi
