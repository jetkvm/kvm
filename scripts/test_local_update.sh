#!/bin/bash

# test_local_update.sh - Run E2E tests with a mock API server
#
# Usage: ./test_local_update.sh <device_ip> <binary_path> <version>
# Example: ./test_local_update.sh 192.168.1.77 bin/jetkvm_app 0.5.2-dev202512221200

set -e

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
DIM='\033[2m'
NC='\033[0m' # No Color

# Global variables for cleanup
TEMP_DIR=""
HTTP_SERVER_PID=""

# Cleanup function - always runs via trap
cleanup() {
    if [ -n "$HTTP_SERVER_PID" ]; then
        kill "$HTTP_SERVER_PID" 2>/dev/null || true
    fi
    if [ -n "$TEMP_DIR" ] && [ -d "$TEMP_DIR" ]; then
        rm -rf "$TEMP_DIR"
    fi
}

# Register cleanup to always run
trap cleanup EXIT

# Check parameters
if [ $# -ne 3 ]; then
    echo -e "${RED}Usage: $0 <device_ip> <binary_path> <version>${NC}"
    exit 1
fi

DEVICE_IP="$1"
BINARY_PATH="$2"
VERSION="$3"

# Verify binary exists
if [ ! -f "$BINARY_PATH" ]; then
    echo -e "${RED}Error: Binary not found at $BINARY_PATH${NC}"
    exit 1
fi

# Get stable version from GitHub
if ! command -v gh >/dev/null 2>&1; then
    echo -e "${RED}Error: gh CLI not installed${NC}"
    exit 1
fi

STABLE_VERSION=$(gh release list --repo jetkvm/kvm --exclude-drafts --exclude-pre-releases --limit 1 --json tagName --jq '.[0].tagName' | sed 's/^release\///')
if [ -z "$STABLE_VERSION" ]; then
    echo -e "${RED}Error: Could not get stable version from GitHub${NC}"
    exit 1
fi

# Detect developer machine IP
if command -v ip >/dev/null 2>&1; then
    DEV_MACHINE_IP=$(ip route get 1 2>/dev/null | awk '{print $7; exit}')
elif command -v ifconfig >/dev/null 2>&1; then
    DEV_MACHINE_IP=$(ifconfig | grep "inet " | grep -v 127.0.0.1 | awk '{print $2}' | head -1)
fi

if [ -z "$DEV_MACHINE_IP" ]; then
    echo -e "${RED}Error: Could not detect developer machine IP${NC}"
    exit 1
fi

# Calculate binary SHA256
BINARY_SHA256=$(shasum -a 256 "$BINARY_PATH" | awk '{print $1}')

# Create mock API server
TEMP_DIR=$(mktemp -d)
mkdir -p "$TEMP_DIR/app/$VERSION"
cp "$BINARY_PATH" "$TEMP_DIR/app/$VERSION/jetkvm_app"
chmod +x "$TEMP_DIR/app/$VERSION/jetkvm_app"

CURRENT_TIMESTAMP=$(($(date +%s) * 1000))

cat > "$TEMP_DIR/server.py" <<PYEOF
#!/usr/bin/env python3
import http.server
import socketserver
import urllib.request
import urllib.parse
import json
import os

PORT = 8443
REAL_API = "https://api.jetkvm.com/releases"
LOCAL_VERSION = "$VERSION"
LOCAL_HASH = "$BINARY_SHA256"
DEV_IP = "$DEV_MACHINE_IP"
TIMESTAMP = $CURRENT_TIMESTAMP

class SmartHandler(http.server.SimpleHTTPRequestHandler):
    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        query = urllib.parse.parse_qs(parsed.query)

        if parsed.path == "/releases":
            if "appVersion" in query or "systemVersion" in query:
                self.proxy_to_real_api()
            else:
                self.send_mock_response()
        else:
            super().do_GET()

    def proxy_to_real_api(self):
        try:
            url = REAL_API + "?" + urllib.parse.urlparse(self.path).query
            req = urllib.request.Request(url)
            with urllib.request.urlopen(req, timeout=30) as response:
                data = response.read()
                self.send_response(response.status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", len(data))
                self.end_headers()
                self.wfile.write(data)
        except Exception as e:
            self.send_error(502, f"Proxy error: {e}")

    def send_mock_response(self):
        response = {
            "appVersion": LOCAL_VERSION,
            "appUrl": f"http://{DEV_IP}:{PORT}/app/{LOCAL_VERSION}/jetkvm_app",
            "appHash": LOCAL_HASH,
            "appCachedAt": TIMESTAMP,
            "appMaxSatisfying": "*",
            "systemVersion": "0.2.7",
            "systemUrl": "https://update.jetkvm.com/system/0.2.7/system.tar",
            "systemHash": "da62bc0246d84e575c719a076a8f403e16e492192e178ecd68bc04ada853f557",
            "systemCachedAt": TIMESTAMP,
            "systemMaxSatisfying": "*"
        }
        data = json.dumps(response).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", len(data))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, format, *args):
        pass  # Silence all HTTP logs

if __name__ == "__main__":
    os.chdir("$TEMP_DIR")
    with socketserver.TCPServer(("", PORT), SmartHandler) as httpd:
        httpd.serve_forever()
PYEOF
chmod +x "$TEMP_DIR/server.py"

# Start mock server (silently)
python3 "$TEMP_DIR/server.py" >/dev/null 2>&1 &
HTTP_SERVER_PID=$!
sleep 2

if ! kill -0 "$HTTP_SERVER_PID" 2>/dev/null; then
    echo -e "${RED}Error: Mock server failed to start (port 8443 in use?)${NC}"
    exit 1
fi

# Verify server is accessible
if ! curl -s "http://$DEV_MACHINE_IP:8443/releases" | grep -q "$VERSION"; then
    echo -e "${RED}Error: Mock server not accessible${NC}"
    exit 1
fi

# Print nice banner using printf for proper alignment
BOX_WIDTH=50
HLINE=$(printf '─%.0s' $(seq 1 $BOX_WIDTH))

print_row() {
    local label="$1"
    local value="$2"
    local content="  $label  $value"
    local pad=$((BOX_WIDTH - ${#content}))
    printf "${CYAN}│${NC}%s%${pad}s${CYAN}│${NC}\n" "$content" ""
}

echo ""
echo -e "${CYAN}╭${HLINE}╮${NC}"
printf "${CYAN}│${NC}  ${GREEN}%-$((BOX_WIDTH - 2))s${NC}${CYAN}│${NC}\n" "E2E Tests"
echo -e "${CYAN}├${HLINE}┤${NC}"
print_row "Device " "http://$DEVICE_IP"
print_row "Version" "$VERSION"
print_row "Stable " "$STABLE_VERSION"
echo -e "${CYAN}╰${HLINE}╯${NC}"
echo ""

# Set environment variables for the test
export JETKVM_URL="http://$DEVICE_IP"
export MOCK_SERVER_URL="http://$DEV_MACHINE_IP:8443"
export TEST_UPDATE_VERSION="$VERSION"
export TEST_STABLE_VERSION="$STABLE_VERSION"

# Change to ui directory and run the test
cd ui

# Ensure dependencies are installed
if [ ! -d "node_modules" ]; then
    echo -e "${YELLOW}Installing npm dependencies...${NC}"
    npm ci
fi

# Run E2E tests
if NODE_NO_WARNINGS=1 npx playwright test; then
    echo ""
    echo -e "${GREEN}✓ All tests passed${NC}"
    TEST_RESULT=0
else
    echo ""
    echo -e "${RED}✗ Tests failed${NC}"
    TEST_RESULT=1
fi

cd - >/dev/null
exit $TEST_RESULT
