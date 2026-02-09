#!/bin/bash

# test_signed_ota.sh - Run OTA signature verification E2E tests
#
# This is a thin wrapper that sets environment variables and invokes Playwright.
# All test setup (mock server, binary deployment, device config) is handled
# inside the Playwright test itself (z-ota-signature.spec.ts).
#
# Usage: ./test_signed_ota.sh <device_ip> <baseline_path> <target_path> <target_version> --signature <sig_path>

set -e

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

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

set -- "${POSITIONAL_ARGS[@]}"

if [ $# -ne 4 ]; then
    echo -e "${RED}Usage: $0 <device_ip> <baseline_path> <target_path> <target_version> --signature <sig_path>${NC}"
    echo ""
    echo "Arguments:"
    echo "  device_ip      - IP address of the JetKVM device"
    echo "  baseline_path  - Path to the baseline binary (deployed to device by Playwright)"
    echo "  target_path    - Path to the target binary (served by Playwright mock server)"
    echo "  target_version - Version string of the target binary"
    echo "  --signature    - Path to the GPG signature file for the target binary"
    exit 1
fi

DEVICE_IP="$1"
BASELINE_PATH="$2"
TARGET_PATH="$3"
TARGET_VERSION="$4"

# Verify files exist
for file_desc in "Baseline binary:$BASELINE_PATH" "Target binary:$TARGET_PATH"; do
    label="${file_desc%%:*}"
    path="${file_desc#*:}"
    if [ ! -f "$path" ]; then
        echo -e "${RED}Error: $label not found at $path${NC}"
        exit 1
    fi
done

if [ -z "$SIGNATURE_PATH" ]; then
    echo -e "${RED}Error: --signature is required${NC}"
    exit 1
fi
if [ ! -f "$SIGNATURE_PATH" ]; then
    echo -e "${RED}Error: Signature file not found at $SIGNATURE_PATH${NC}"
    exit 1
fi

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
printf "${CYAN}│${NC}  ${GREEN}%-$((BOX_WIDTH - 2))s${NC}${CYAN}│${NC}\n" "OTA Signature Verification Tests"
echo -e "${CYAN}├${HLINE}┤${NC}"
print_row "Device   " "http://$DEVICE_IP"
print_row "Baseline " "$BASELINE_PATH"
print_row "Target   " "$TARGET_VERSION"
print_row "Signature" "$SIGNATURE_PATH"
echo -e "${CYAN}╰${HLINE}╯${NC}"
echo ""

# Set environment variables -- Playwright tests handle all setup internally
export JETKVM_URL="http://$DEVICE_IP"
export BASELINE_BINARY_PATH="$(realpath "$BASELINE_PATH")"
export RELEASE_BINARY_PATH="$(realpath "$TARGET_PATH")"
export RELEASE_SIGNATURE_PATH="$(realpath "$SIGNATURE_PATH")"
export TEST_UPDATE_VERSION="$TARGET_VERSION"

# Change to ui directory and run the test
cd ui

# Ensure dependencies are installed
if [ ! -d "node_modules" ]; then
    echo -e "${YELLOW}Installing npm dependencies...${NC}"
    npm ci
fi

# Run the OTA signature verification tests
if NODE_NO_WARNINGS=1 npx playwright test z-ota-signature.spec.ts; then
    echo ""
    echo -e "${GREEN}✓ OTA signature verification tests passed${NC}"
    TEST_RESULT=0
else
    echo ""
    echo -e "${RED}✗ OTA signature verification tests failed${NC}"
    TEST_RESULT=1
fi

cd - >/dev/null

exit $TEST_RESULT
