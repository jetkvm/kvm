#!/bin/bash

# test_prerelease_unsigned_ota.sh - Run unsigned prerelease OTA E2E test
#
# This wrapper configures environment variables and runs
# z-ota-prerelease-unsigned.spec.ts, which deploys a baseline binary,
# starts a mock update server with prerelease metadata, and validates that
# prerelease OTA bypasses signature requirements.
#
# Usage: ./test_prerelease_unsigned_ota.sh <device_ip> <baseline_path> <target_path> <target_version>

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

if [ $# -ne 4 ]; then
    echo -e "${RED}Usage: $0 <device_ip> <baseline_path> <target_path> <target_version>${NC}"
    echo ""
    echo "Arguments:"
    echo "  device_ip      - IP address of the JetKVM device"
    echo "  baseline_path  - Path to the baseline binary (deployed by Playwright)"
    echo "  target_path    - Path to the target binary (served by Playwright mock server)"
    echo "  target_version - Version string of the target binary"
    exit 1
fi

DEVICE_IP="$1"
BASELINE_PATH="$2"
TARGET_PATH="$3"
TARGET_VERSION="$4"

for file_desc in "Baseline binary:$BASELINE_PATH" "Target binary:$TARGET_PATH"; do
    label="${file_desc%%:*}"
    path="${file_desc#*:}"
    if [ ! -f "$path" ]; then
        echo -e "${RED}Error: $label not found at $path${NC}"
        exit 1
    fi
done

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
printf "${CYAN}│${NC}  ${GREEN}%-$((BOX_WIDTH - 2))s${NC}${CYAN}│${NC}\n" "OTA Prerelease Unsigned Test"
echo -e "${CYAN}├${HLINE}┤${NC}"
print_row "Device   " "http://$DEVICE_IP"
print_row "Baseline " "$BASELINE_PATH"
print_row "Target   " "$TARGET_VERSION"
echo -e "${CYAN}╰${HLINE}╯${NC}"
echo ""

export JETKVM_URL="http://$DEVICE_IP"
export BASELINE_BINARY_PATH="$(realpath "$BASELINE_PATH")"
export RELEASE_BINARY_PATH="$(realpath "$TARGET_PATH")"
export TEST_UPDATE_VERSION="$TARGET_VERSION"

cd ui

if [ ! -d "node_modules" ]; then
    echo -e "${YELLOW}Installing npm dependencies...${NC}"
    npm ci
fi

if NODE_NO_WARNINGS=1 npx playwright test z-ota-prerelease-unsigned.spec.ts; then
    echo ""
    echo -e "${GREEN}✓ OTA prerelease unsigned test passed${NC}"
    TEST_RESULT=0
else
    echo ""
    echo -e "${RED}✗ OTA prerelease unsigned test failed${NC}"
    TEST_RESULT=1
fi

cd - >/dev/null
exit $TEST_RESULT
