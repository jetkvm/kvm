#!/bin/bash

# test_local_update.sh - Local OTA update testing via /etc/hosts MITM
#
# Usage: ./test_local_update.sh <device_ip> <binary_path> <version>
# Example: ./test_local_update.sh 192.168.1.100 bin/jetkvm_app 0.5.1-test-1234567890

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
HOSTS_MODIFIED=false
CONFIG_MODIFIED=false
DEVICE_IP=""
DEV_MACHINE_IP=""

# Cleanup function - always runs via trap
cleanup() {
    echo ""
    echo -e "${BLUE}=== Phase 4: Cleanup ===${NC}"

    # Restore config on device
    if [ "$CONFIG_MODIFIED" = true ] && [ -n "$DEVICE_IP" ]; then
        echo -e "${YELLOW}Restoring config on device...${NC}"
        if ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no root@"$DEVICE_IP" \
            "mv /tmp/kvm_config.json.backup /userdata/kvm_config.json 2>/dev/null && systemctl restart jetkvm" 2>/dev/null; then
            echo -e "${GREEN}✓ Config restored and service restarted${NC}"
        else
            echo -e "${YELLOW}⚠ Failed to restore config (device may be rebooting)${NC}"
        fi
    fi

    # Kill HTTP server
    if [ -n "$HTTP_SERVER_PID" ]; then
        echo -e "${YELLOW}Stopping HTTP server (PID: $HTTP_SERVER_PID)...${NC}"
        if kill "$HTTP_SERVER_PID" 2>/dev/null; then
            echo -e "${GREEN}✓ HTTP server stopped${NC}"
        else
            echo -e "${YELLOW}⚠ HTTP server already stopped${NC}"
        fi
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

# Phase 1: Validation & Setup
echo -e "${BLUE}=== Phase 1: Validation & Setup ===${NC}"

# Check parameters
if [ $# -ne 3 ]; then
    echo -e "${RED}Error: Wrong number of arguments${NC}"
    echo "Usage: $0 <device_ip> <binary_path> <version>"
    echo "Example: $0 192.168.1.100 bin/jetkvm_app 0.5.1-test-1234567890"
    exit 1
fi

DEVICE_IP="$1"
BINARY_PATH="$2"
VERSION="$3"

echo "Device IP: $DEVICE_IP"
echo "Binary path: $BINARY_PATH"
echo "Version: $VERSION"

# Verify binary exists
if [ ! -f "$BINARY_PATH" ]; then
    echo -e "${RED}Error: Binary not found at $BINARY_PATH${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Binary exists${NC}"

# Detect developer machine IP
echo -e "${YELLOW}Detecting developer machine IP...${NC}"
if command -v ip >/dev/null 2>&1; then
    # Linux
    DEV_MACHINE_IP=$(ip route get 1 2>/dev/null | awk '{print $7; exit}')
elif command -v ifconfig >/dev/null 2>&1; then
    # macOS fallback
    DEV_MACHINE_IP=$(ifconfig | grep "inet " | grep -v 127.0.0.1 | awk '{print $2}' | head -1)
fi

if [ -z "$DEV_MACHINE_IP" ]; then
    echo -e "${RED}Error: Could not detect developer machine IP${NC}"
    exit 1
fi
echo "Developer machine IP: $DEV_MACHINE_IP"
echo -e "${GREEN}✓ IP detected${NC}"

# Test SSH connectivity
echo -e "${YELLOW}Testing SSH connectivity to device...${NC}"
if ! ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no root@"$DEVICE_IP" "echo 'SSH OK'" >/dev/null 2>&1; then
    echo -e "${RED}Error: Cannot connect to device via SSH${NC}"
    echo "Please check:"
    echo "  - Device is powered on"
    echo "  - Device IP is correct: $DEVICE_IP"
    echo "  - Network connectivity"
    echo "  - SSH is enabled on device"
    exit 1
fi
echo -e "${GREEN}✓ SSH connectivity confirmed${NC}"

# Calculate binary SHA256
echo -e "${YELLOW}Calculating binary SHA256...${NC}"
BINARY_SHA256=$(shasum -a 256 "$BINARY_PATH" | awk '{print $1}')
echo "SHA256: $BINARY_SHA256"
echo -e "${GREEN}✓ SHA256 calculated${NC}"

# Phase 2: Configuration Setup
echo ""
echo -e "${BLUE}=== Phase 2: Configuration Setup ===${NC}"

# Backup and modify config on device
echo -e "${YELLOW}Backing up config on device...${NC}"
ssh -o StrictHostKeyChecking=no root@"$DEVICE_IP" \
    "cp /userdata/kvm_config.json /tmp/kvm_config.json.backup"
echo -e "${GREEN}✓ Config backed up${NC}"

echo -e "${YELLOW}Downloading config to modify locally...${NC}"
scp -o StrictHostKeyChecking=no root@"$DEVICE_IP":/userdata/kvm_config.json /tmp/kvm_config_download.json >/dev/null 2>&1

echo -e "${YELLOW}Modifying config to use local update server...${NC}"
jq --arg url "http://$DEV_MACHINE_IP:8443" '.update_api_url = $url' /tmp/kvm_config_download.json > /tmp/kvm_config_modified.json

echo -e "${YELLOW}Uploading modified config...${NC}"
scp -o StrictHostKeyChecking=no /tmp/kvm_config_modified.json root@"$DEVICE_IP":/userdata/kvm_config.json >/dev/null 2>&1
CONFIG_MODIFIED=true
echo -e "${GREEN}✓ Config modified (update_api_url -> http://$DEV_MACHINE_IP:8443)${NC}"

# Clean up local temp files
rm -f /tmp/kvm_config_download.json /tmp/kvm_config_modified.json

# Restart jetkvm service to pick up new config
echo -e "${YELLOW}Restarting jetkvm service...${NC}"
ssh -o StrictHostKeyChecking=no root@"$DEVICE_IP" "systemctl restart jetkvm"
echo -e "${GREEN}✓ Service restarted${NC}"

# Wait for service to start
echo -e "${YELLOW}Waiting for service to start...${NC}"
sleep 5
echo -e "${GREEN}✓ Service should be ready${NC}"

# Create mock API server directory structure
echo -e "${YELLOW}Creating mock API server...${NC}"
TEMP_DIR=$(mktemp -d)
echo "Temp directory: $TEMP_DIR"

# Create app directory structure
mkdir -p "$TEMP_DIR/app/$VERSION"
cp "$BINARY_PATH" "$TEMP_DIR/app/$VERSION/jetkvm_app"
chmod +x "$TEMP_DIR/app/$VERSION/jetkvm_app"
echo -e "${GREEN}✓ Binary copied to mock server${NC}"

# Create releases API response
# Use current timestamp for cache fields
CURRENT_TIMESTAMP=$(($(date +%s) * 1000))

cat > "$TEMP_DIR/releases" <<EOF
{
  "appVersion": "$VERSION",
  "appUrl": "http://$DEV_MACHINE_IP:8443/app/$VERSION/jetkvm_app",
  "appHash": "$BINARY_SHA256",
  "appCachedAt": $CURRENT_TIMESTAMP,
  "appMaxSatisfying": "*",
  "systemVersion": "0.2.7",
  "systemUrl": "https://update.jetkvm.com/system/0.2.7/system.tar",
  "systemHash": "da62bc0246d84e575c719a076a8f403e16e492192e178ecd68bc04ada853f557",
  "systemCachedAt": $CURRENT_TIMESTAMP,
  "systemMaxSatisfying": "*"
}
EOF
echo -e "${GREEN}✓ Mock API response created${NC}"

# Start HTTP server
echo -e "${YELLOW}Starting HTTP server on port 8443...${NC}"
cd "$TEMP_DIR"
python3 -m http.server 8443 >/dev/null 2>&1 &
HTTP_SERVER_PID=$!
cd - >/dev/null

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

# Phase 3: Execute E2E Test
echo ""
echo -e "${BLUE}=== Phase 3: Execute E2E Test ===${NC}"

# Set environment variables for the test
export JETKVM_URL="http://$DEVICE_IP"
export TEST_UPDATE_VERSION="$VERSION"

echo "JETKVM_URL: $JETKVM_URL"
echo "TEST_UPDATE_VERSION: $TEST_UPDATE_VERSION"

# Change to ui directory and run the test
echo -e "${YELLOW}Running Playwright test...${NC}"
cd ui

# Ensure dependencies are installed
if [ ! -d "node_modules" ]; then
    echo -e "${YELLOW}Installing npm dependencies...${NC}"
    npm install
fi

# Run the specific local update test
echo ""
echo -e "${BLUE}=== Starting E2E Test ===${NC}"
echo ""

if NODE_NO_WARNINGS=1 npm run test:e2e -- local-update-flow.spec.ts; then
    echo ""
    echo -e "${GREEN}=== ✓ E2E Test PASSED ===${NC}"
    TEST_RESULT=0
else
    echo ""
    echo -e "${RED}=== ✗ E2E Test FAILED ===${NC}"
    TEST_RESULT=1
fi

cd - >/dev/null

# Cleanup happens automatically via trap
exit $TEST_RESULT

