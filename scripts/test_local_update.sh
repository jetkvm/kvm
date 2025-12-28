#!/bin/bash

# test_local_update.sh - E2E testing with OTA update flow
#
# Sets up mock API server and runs ALL E2E tests including OTA update flow.
#
# Usage: ./test_local_update.sh <device_ip> <binary_path> <version>
# Example: ./test_local_update.sh 192.168.1.77 bin/jetkvm_app 0.5.2-test-1766500000

set -e

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Global variables for cleanup
TEMP_DIR=""
HTTP_SERVER_PID=""

# Cleanup function - always runs via trap
cleanup() {
    echo ""
    echo -e "${BLUE}=== Cleanup ===${NC}"

    # Kill HTTP server
    if [ -n "$HTTP_SERVER_PID" ]; then
        echo -e "${YELLOW}Stopping HTTP server (PID: $HTTP_SERVER_PID)...${NC}"
        kill "$HTTP_SERVER_PID" 2>/dev/null || true
        echo -e "${GREEN}✓ HTTP server stopped${NC}"
    fi

    # Remove temp directory
    if [ -n "$TEMP_DIR" ] && [ -d "$TEMP_DIR" ]; then
        echo -e "${YELLOW}Removing temp directory...${NC}"
        rm -rf "$TEMP_DIR"
        echo -e "${GREEN}✓ Temp directory removed${NC}"
    fi

    echo -e "${BLUE}=== Cleanup complete ===${NC}"
}

# Register cleanup to always run
trap cleanup EXIT

# Check parameters
if [ $# -ne 3 ]; then
    echo -e "${RED}Error: Wrong number of arguments${NC}"
    echo "Usage: $0 <device_ip> <binary_path> <version>"
    echo "Example: $0 192.168.1.77 bin/jetkvm_app 0.5.2-dev202512221200"
    exit 1
fi

DEVICE_IP="$1"
BINARY_PATH="$2"
VERSION="$3"

echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}  E2E Tests (including OTA Update Flow)${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo "  Device IP: $DEVICE_IP"
echo "  Binary: $BINARY_PATH"
echo "  Version: $VERSION"
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo ""

# Verify binary exists
if [ ! -f "$BINARY_PATH" ]; then
    echo -e "${RED}Error: Binary not found at $BINARY_PATH${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Binary exists${NC}"

# Get stable version from GitHub
echo -e "${YELLOW}Getting latest stable version from GitHub...${NC}"
if ! command -v gh >/dev/null 2>&1; then
    echo -e "${RED}Error: gh CLI not installed${NC}"
    exit 1
fi

STABLE_VERSION=$(gh release list --repo jetkvm/kvm --exclude-drafts --exclude-pre-releases --limit 1 --json tagName --jq '.[0].tagName' | sed 's/^release\///')
if [ -z "$STABLE_VERSION" ]; then
    echo -e "${RED}Error: Could not get stable version from GitHub${NC}"
    exit 1
fi
echo "Latest stable version: $STABLE_VERSION"
echo -e "${GREEN}✓ Got stable version${NC}"

# Detect developer machine IP
echo -e "${YELLOW}Detecting developer machine IP...${NC}"
if command -v ip >/dev/null 2>&1; then
    DEV_MACHINE_IP=$(ip route get 1 2>/dev/null | awk '{print $7; exit}')
elif command -v ifconfig >/dev/null 2>&1; then
    DEV_MACHINE_IP=$(ifconfig | grep "inet " | grep -v 127.0.0.1 | awk '{print $2}' | head -1)
fi

if [ -z "$DEV_MACHINE_IP" ]; then
    echo -e "${RED}Error: Could not detect developer machine IP${NC}"
    exit 1
fi
echo "Developer machine IP: $DEV_MACHINE_IP"
echo -e "${GREEN}✓ IP detected${NC}"

# Calculate binary SHA256
echo -e "${YELLOW}Calculating binary SHA256...${NC}"
BINARY_SHA256=$(shasum -a 256 "$BINARY_PATH" | awk '{print $1}')
echo "SHA256: $BINARY_SHA256"
echo -e "${GREEN}✓ SHA256 calculated${NC}"

# Create mock API server directory structure
echo -e "${YELLOW}Creating mock API server...${NC}"
TEMP_DIR=$(mktemp -d)
echo "Temp directory: $TEMP_DIR"

# Create app directory structure
mkdir -p "$TEMP_DIR/app/$VERSION"
cp "$BINARY_PATH" "$TEMP_DIR/app/$VERSION/jetkvm_app"
chmod +x "$TEMP_DIR/app/$VERSION/jetkvm_app"
echo -e "${GREEN}✓ Binary copied to mock server${NC}"

# Create smart proxy server script
# - If /releases has appVersion param → proxy to real API (for downgrade)
# - If /releases has no appVersion → return our mock (for OTA upgrade)
# - Serve /app/* locally
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

        # Handle /releases endpoint
        if parsed.path == "/releases":
            # If appVersion is specified, proxy to real API (for downgrade)
            if "appVersion" in query or "systemVersion" in query:
                self.proxy_to_real_api()
            else:
                # Normal update check - return our mock
                self.send_mock_response()
        else:
            # Serve local files (for /app/*/jetkvm_app)
            super().do_GET()

    def proxy_to_real_api(self):
        """Proxy request to real API for custom version requests"""
        try:
            url = REAL_API + "?" + urllib.parse.urlparse(self.path).query
            print(f"[PROXY] → {url}")
            req = urllib.request.Request(url)
            with urllib.request.urlopen(req, timeout=30) as response:
                data = response.read()
                self.send_response(response.status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", len(data))
                self.end_headers()
                self.wfile.write(data)
                print(f"[PROXY] ← {response.status}")
        except Exception as e:
            print(f"[PROXY] ERROR: {e}")
            self.send_error(502, f"Proxy error: {e}")

    def send_mock_response(self):
        """Return mock response with our local build"""
        print(f"[MOCK] Returning local version {LOCAL_VERSION}")
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
        print(f"[HTTP] {args[0]}")

if __name__ == "__main__":
    os.chdir("$TEMP_DIR")
    with socketserver.TCPServer(("", PORT), SmartHandler) as httpd:
        print(f"Smart proxy server on port {PORT}")
        print(f"  - Custom version requests → proxy to {REAL_API}")
        print(f"  - Normal requests → return {LOCAL_VERSION}")
        httpd.serve_forever()
PYEOF
chmod +x "$TEMP_DIR/server.py"
echo -e "${GREEN}✓ Smart proxy server created${NC}"

# Start smart proxy server
echo -e "${YELLOW}Starting smart proxy server on port 8443...${NC}"
python3 "$TEMP_DIR/server.py" &
HTTP_SERVER_PID=$!

# Wait for server to start
sleep 2

# Verify server is running
if ! kill -0 "$HTTP_SERVER_PID" 2>/dev/null; then
    echo -e "${RED}Error: HTTP server failed to start${NC}"
    echo "Port 8443 may already be in use. Check with: lsof -i :8443"
    exit 1
fi
echo -e "${GREEN}✓ HTTP server started (PID: $HTTP_SERVER_PID)${NC}"

# Test server accessibility
echo -e "${YELLOW}Testing server accessibility...${NC}"
if curl -s "http://$DEV_MACHINE_IP:8443/releases" | grep -q "$VERSION"; then
    echo -e "${GREEN}✓ Mock API server is accessible${NC}"
else
    echo -e "${RED}Error: Mock API server is not accessible${NC}"
    exit 1
fi

# Run E2E Test
echo ""
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}  Running Playwright Test${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo ""

# Set environment variables for the test
export JETKVM_URL="http://$DEVICE_IP"
export MOCK_SERVER_URL="http://$DEV_MACHINE_IP:8443"
export TEST_UPDATE_VERSION="$VERSION"
export TEST_STABLE_VERSION="$STABLE_VERSION"

echo "JETKVM_URL: $JETKVM_URL"
echo "MOCK_SERVER_URL: $MOCK_SERVER_URL"
echo "TEST_UPDATE_VERSION: $TEST_UPDATE_VERSION"
echo "TEST_STABLE_VERSION: $TEST_STABLE_VERSION"
echo ""

# Change to ui directory and run the test
cd ui

# Ensure dependencies are installed
if [ ! -d "node_modules" ]; then
    echo -e "${YELLOW}Installing npm dependencies...${NC}"
    npm ci
fi

# Run ALL E2E tests (including OTA update flow)
echo ""
if NODE_NO_WARNINGS=1 npx playwright test; then
    echo ""
    echo -e "${GREEN}═══════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}  ✓ ALL E2E TESTS PASSED${NC}"
    echo -e "${GREEN}═══════════════════════════════════════════════════════${NC}"
    TEST_RESULT=0
else
    echo ""
    echo -e "${RED}═══════════════════════════════════════════════════════${NC}"
    echo -e "${RED}  ✗ E2E TESTS FAILED${NC}"
    echo -e "${RED}═══════════════════════════════════════════════════════${NC}"
    TEST_RESULT=1
fi

cd - >/dev/null

# Cleanup happens automatically via trap
exit $TEST_RESULT
