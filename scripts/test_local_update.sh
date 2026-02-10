#!/bin/bash

# test_local_update.sh - Deploy a binary and run core E2E tests
#
# Deploys the given binary to the device, then runs the Playwright E2E suite.
# OTA-specific tests that need a mock server create their own internally.
#
# Usage: ./test_local_update.sh <device_ip> <binary_path> <version>
# Example: ./test_local_update.sh 192.168.1.77 bin/jetkvm_app 0.5.4

set -e

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

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

# SSH helper function
sshdev() {
    ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no "root@$DEVICE_IP" "$@"
}

# Deploy binary to device
echo -e "${CYAN}Deploying binary to device...${NC}"
sshdev "cat > /userdata/jetkvm/jetkvm_app.update" < "$BINARY_PATH"
sshdev "reboot"

echo -e "${YELLOW}Waiting for device to reboot...${NC}"
sleep 30

# Wait for device to come back online
for i in {1..30}; do
    if ping -c 1 -W 2 "$DEVICE_IP" >/dev/null 2>&1; then
        break
    fi
    sleep 2
done

# Wait for web interface to be ready
for i in {1..30}; do
    if curl -s --max-time 5 "http://$DEVICE_IP" >/dev/null 2>&1; then
        echo -e "${GREEN}Device is ready${NC}"
        break
    fi
    sleep 2
done

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
printf "${CYAN}│${NC}  ${GREEN}%-$((BOX_WIDTH - 2))s${NC}${CYAN}│${NC}\n" "E2E Tests"
echo -e "${CYAN}├${HLINE}┤${NC}"
print_row "Device  " "http://$DEVICE_IP"
print_row "Version " "$VERSION"
print_row "Deployed" "Yes"
echo -e "${CYAN}╰${HLINE}╯${NC}"
echo ""

# Set environment variables for compatibility with OTA wrapper scripts.
export JETKVM_URL="http://$DEVICE_IP"
export RELEASE_BINARY_PATH="$(realpath "$BINARY_PATH")"
export TEST_UPDATE_VERSION="$VERSION"

# Change to ui directory and run the tests
cd ui

# Ensure dependencies are installed
if [ ! -d "node_modules" ]; then
    echo -e "${YELLOW}Installing npm dependencies...${NC}"
    npm ci
fi

# Run E2E tests, excluding OTA suites that require dedicated baseline+target lanes.
PLAYWRIGHT_ARGS=()
PLAYWRIGHT_ARGS+=(--grep-invert "OTA Signature Verification|OTA Specific Version Unsigned|OTA Prerelease Unsigned")

if NODE_NO_WARNINGS=1 npx playwright test "${PLAYWRIGHT_ARGS[@]}"; then
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
