#!/bin/bash

# test_signed_ota.sh - Test signed OTA upgrade flow
#
# This test verifies that GPG signature verification actually runs during upgrades.
# It works by deploying a baseline binary (with GPG verification code) and then
# upgrading to a signed target binary.
#
# Usage: ./test_signed_ota.sh <device_ip> <baseline_path> <target_path> <target_version> --signature <sig_path>
#
# Example:
#   ./test_signed_ota.sh 192.168.1.77 \
#       bin/jetkvm_app_baseline \
#       bin/jetkvm_app \
#       0.5.3-dev202501151200 \
#       --signature bin/jetkvm_app.sig

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
    # Restore device config to point back to real API
    if [ -n "$DEVICE_IP" ]; then
        echo -e "${CYAN}Restoring device config to production API...${NC}"
        sshdev "sed -i 's|\"update_api_url\": \"[^\"]*\"|\"update_api_url\": \"https://api.jetkvm.com\"|' /userdata/kvm_config.json" 2>/dev/null || true
    fi
    if [ -n "$TEMP_DIR" ] && [ -d "$TEMP_DIR" ]; then
        rm -rf "$TEMP_DIR"
    fi
}

# Register cleanup to always run
trap cleanup EXIT

# Parse arguments
SIGNATURE_PATH=""
POSITIONAL_ARGS=()

while [[ $# -gt 0 ]]; do
    case $1 in
        --signature)
            SIGNATURE_PATH="$2"
            shift 2
            ;;
        *)
            POSITIONAL_ARGS+=("$1")
            shift
            ;;
    esac
done

# Restore positional parameters
set -- "${POSITIONAL_ARGS[@]}"

# Check parameters
if [ $# -ne 4 ]; then
    echo -e "${RED}Usage: $0 <device_ip> <baseline_path> <target_path> <target_version> --signature <sig_path>${NC}"
    echo ""
    echo "Arguments:"
    echo "  device_ip      - IP address of the JetKVM device"
    echo "  baseline_path  - Path to the baseline binary (will be deployed first)"
    echo "  target_path    - Path to the target binary (upgrade destination)"
    echo "  target_version - Version string of the target binary"
    echo "  --signature    - Path to the GPG signature file for the target binary"
    exit 1
fi

DEVICE_IP="$1"
BASELINE_PATH="$2"
TARGET_PATH="$3"
TARGET_VERSION="$4"

# Verify files exist
if [ ! -f "$BASELINE_PATH" ]; then
    echo -e "${RED}Error: Baseline binary not found at $BASELINE_PATH${NC}"
    exit 1
fi

if [ ! -f "$TARGET_PATH" ]; then
    echo -e "${RED}Error: Target binary not found at $TARGET_PATH${NC}"
    exit 1
fi

# Signature is required for this test
if [ -z "$SIGNATURE_PATH" ]; then
    echo -e "${RED}Error: --signature is required for signed OTA test${NC}"
    exit 1
fi

if [ ! -f "$SIGNATURE_PATH" ]; then
    echo -e "${RED}Error: Signature file not found at $SIGNATURE_PATH${NC}"
    exit 1
fi

echo -e "${GREEN}Signature file: $SIGNATURE_PATH${NC}"

# Extract baseline version from binary using strings
# The version is embedded as builtAppVersion in the binary
BASELINE_VERSION=$(strings "$BASELINE_PATH" 2>/dev/null | grep -E '^[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "")
if [ -z "$BASELINE_VERSION" ]; then
    # Fallback: try to find version in a different format
    BASELINE_VERSION=$(strings "$BASELINE_PATH" 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+-test-baseline' | head -1 || echo "unknown")
fi
echo -e "${CYAN}Detected baseline version: $BASELINE_VERSION${NC}"

# Detect developer machine IP
if command -v ip >/dev/null 2>&1; then
    DEV_MACHINE_IP=$(ip route get 1 2>/dev/null | awk '{print $7; exit}')
elif command -v ifconfig >/dev/null 2>&1; then
    DEV_MACHINE_IP=$(ifconfig | grep "inet " | grep -v "127\." | awk '{print $2}' | head -1)
fi

if [ -z "$DEV_MACHINE_IP" ]; then
    echo -e "${RED}Error: Could not detect developer machine IP${NC}"
    exit 1
fi

# SSH helper function
sshdev() {
    ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no "root@$DEVICE_IP" "$@"
}

# Calculate target SHA256
TARGET_SHA256=$(shasum -a 256 "$TARGET_PATH" | awk '{print $1}')

# Helper to wait for device to come back online after reboot
wait_for_device() {
    local label="${1:-reboot}"
    echo -e "${YELLOW}Waiting for device to reboot ($label)...${NC}"
    sleep 30

    for i in {1..30}; do
        if ping -c 1 -W 2 "$DEVICE_IP" >/dev/null 2>&1; then
            break
        fi
        sleep 2
    done

    for i in {1..30}; do
        if curl -s --max-time 5 "http://$DEVICE_IP" >/dev/null 2>&1; then
            echo -e "${GREEN}Device is ready${NC}"
            return 0
        fi
        sleep 2
    done
    echo -e "${RED}Device did not come back online${NC}"
    return 1
}

# Create mock API server directory (must be ready before config change,
# since the device may check for updates immediately after reboot)
TEMP_DIR=$(mktemp -d)
mkdir -p "$TEMP_DIR/app/$TARGET_VERSION"
cp "$TARGET_PATH" "$TEMP_DIR/app/$TARGET_VERSION/jetkvm_app"
chmod +x "$TEMP_DIR/app/$TARGET_VERSION/jetkvm_app"
cp "$SIGNATURE_PATH" "$TEMP_DIR/app/$TARGET_VERSION/jetkvm_app.sig"

CURRENT_TIMESTAMP=$(($(date +%s) * 1000))

# Create mock server that always includes signature URL
cat > "$TEMP_DIR/server.py" <<PYEOF
#!/usr/bin/env python3
import http.server
import socketserver
import urllib.parse
import json
import os

PORT = 8443
TARGET_VERSION = "$TARGET_VERSION"
TARGET_HASH = "$TARGET_SHA256"
DEV_IP = "$DEV_MACHINE_IP"
TIMESTAMP = $CURRENT_TIMESTAMP

class SignedOTAHandler(http.server.SimpleHTTPRequestHandler):
    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)

        if parsed.path == "/releases":
            self.send_mock_response()
        else:
            super().do_GET()

    def send_mock_response(self):
        # Always include signature URL - this is the signed OTA test
        response = {
            "appVersion": TARGET_VERSION,
            "appUrl": f"http://{DEV_IP}:{PORT}/app/{TARGET_VERSION}/jetkvm_app",
            "appHash": TARGET_HASH,
            "appSigUrl": f"http://{DEV_IP}:{PORT}/app/{TARGET_VERSION}/jetkvm_app.sig",
            "appCachedAt": TIMESTAMP,
            "appMaxSatisfying": "*",
            # Keep system at a low version so no system update is triggered
            "systemVersion": "0.0.1",
            "systemUrl": "",
            "systemHash": "",
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
    with socketserver.TCPServer(("", PORT), SignedOTAHandler) as httpd:
        print(f"Serving signed OTA on port {PORT}")
        httpd.serve_forever()
PYEOF
chmod +x "$TEMP_DIR/server.py"

# Start mock server
python3 "$TEMP_DIR/server.py" >/dev/null 2>&1 &
HTTP_SERVER_PID=$!
sleep 2

if ! kill -0 "$HTTP_SERVER_PID" 2>/dev/null; then
    echo -e "${RED}Error: Mock server failed to start (port 8443 in use?)${NC}"
    exit 1
fi

# Verify server is accessible and includes signature URL
MOCK_RESPONSE=$(curl -s "http://$DEV_MACHINE_IP:8443/releases")
if ! echo "$MOCK_RESPONSE" | grep -q "$TARGET_VERSION"; then
    echo -e "${RED}Error: Mock server not returning expected version${NC}"
    exit 1
fi
if ! echo "$MOCK_RESPONSE" | grep -q "appSigUrl"; then
    echo -e "${RED}Error: Mock server not returning signature URL${NC}"
    exit 1
fi

# Deploy BASELINE binary to device (not target!)
echo -e "${CYAN}Deploying baseline binary to device...${NC}"
sshdev "cat > /userdata/jetkvm/jetkvm_app.update" < "$BASELINE_PATH"
sshdev "reboot"
wait_for_device "baseline deploy"

# Configure device to use mock API server via SSH (before Playwright,
# so the SSH log tail captures the boot where GPG verification happens)
echo -e "${CYAN}Configuring device to use mock API ($DEV_MACHINE_IP:8443)...${NC}"
sshdev "sed -i 's|\"update_api_url\": \"[^\"]*\"|\"update_api_url\": \"http://$DEV_MACHINE_IP:8443\"|' /userdata/kvm_config.json"
sshdev "reboot"
wait_for_device "config change"

# Print banner
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
printf "${CYAN}│${NC}  ${GREEN}%-$((BOX_WIDTH - 2))s${NC}${CYAN}│${NC}\n" "Signed OTA E2E Test"
echo -e "${CYAN}├${HLINE}┤${NC}"
print_row "Device   " "http://$DEVICE_IP"
print_row "Baseline " "$BASELINE_VERSION"
print_row "Target   " "$TARGET_VERSION"
print_row "Signature" "Required (GPG verified)"
echo -e "${CYAN}╰${HLINE}╯${NC}"
echo ""

# Set environment variables for the test
export JETKVM_URL="http://$DEVICE_IP"
export MOCK_SERVER_URL="http://$DEV_MACHINE_IP:8443"
export TEST_UPDATE_VERSION="$TARGET_VERSION"
export TEST_BASELINE_VERSION="$BASELINE_VERSION"
export SIGNED_OTA_TEST="1"

# Change to ui directory and run the test
cd ui

# Ensure dependencies are installed
if [ ! -d "node_modules" ]; then
    echo -e "${YELLOW}Installing npm dependencies...${NC}"
    npm ci
fi

# Run only the signed OTA upgrade test
if NODE_NO_WARNINGS=1 npx playwright test z-ota-signed-upgrade.spec.ts; then
    echo ""
    echo -e "${GREEN}✓ Signed OTA test passed - GPG signature verification works!${NC}"
    TEST_RESULT=0
else
    echo ""
    echo -e "${RED}✗ Signed OTA test failed${NC}"
    TEST_RESULT=1
fi

cd - >/dev/null

exit $TEST_RESULT
