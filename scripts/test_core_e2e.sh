#!/bin/bash

# test_core_e2e.sh - Run core (non-OTA) Playwright E2E tests
#
# Usage: ./test_core_e2e.sh <device_ip> <binary_path>

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

if [ $# -ne 2 ]; then
    echo -e "${RED}Usage: $0 <device_ip> <binary_path>${NC}"
    exit 1
fi

DEVICE_IP="$1"
BINARY_PATH="$2"

if [ ! -f "$BINARY_PATH" ]; then
    echo -e "${RED}Error: Binary not found at $BINARY_PATH${NC}"
    exit 1
fi

sshdev() {
    ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no "root@$DEVICE_IP" "$@"
}

echo -e "${CYAN}Deploying binary to device...${NC}"
sshdev "cat > /userdata/jetkvm/jetkvm_app.update" < "$BINARY_PATH"
sshdev "reboot"

echo -e "${YELLOW}Waiting for device to reboot...${NC}"
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
        break
    fi
    sleep 2
done

export JETKVM_URL="http://$DEVICE_IP"

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
LOG_DIR="$SCRIPT_DIR/ui/test-results/device-logs"
PRE_LOG_DIR=$(mktemp -d)

# Capture device state before tests (to temp dir since Playwright wipes test-results/)
echo -e "${CYAN}Capturing pre-test device logs...${NC}"
sshdev 'cat /userdata/jetkvm/last.log' > "$PRE_LOG_DIR/pre-test-last.log" 2>/dev/null || true
sshdev 'cat /userdata/kvm_config.json' > "$PRE_LOG_DIR/pre-test-config.json" 2>/dev/null || true
sshdev 'ls -la /userdata/ /userdata/jetkvm/' > "$PRE_LOG_DIR/pre-test-fs.txt" 2>/dev/null || true

cd ui

if [ ! -d "node_modules" ]; then
    echo -e "${YELLOW}Installing npm dependencies...${NC}"
    npm ci
fi

CORE_SPECS=$(find e2e -maxdepth 1 -name "*.spec.ts" | sort | grep -v "z-ota")
if [ -z "$CORE_SPECS" ]; then
    echo -e "${RED}Error: No core E2E specs found${NC}"
    exit 1
fi

if NODE_NO_WARNINGS=1 npx playwright test $CORE_SPECS; then
    echo ""
    echo -e "${GREEN}✓ Core E2E tests passed${NC}"
    TEST_RESULT=0
else
    echo ""
    echo -e "${RED}✗ Core E2E tests failed${NC}"
    TEST_RESULT=1
fi

# Capture device state after tests (especially useful on failure)
mkdir -p "$LOG_DIR"
cp "$PRE_LOG_DIR"/* "$LOG_DIR/" 2>/dev/null || true
rm -rf "$PRE_LOG_DIR"
echo -e "${CYAN}Capturing post-test device logs...${NC}"
sshdev 'cat /userdata/jetkvm/last.log' > "$LOG_DIR/post-test-last.log" 2>/dev/null || true
sshdev 'cat /userdata/kvm_config.json' > "$LOG_DIR/post-test-config.json" 2>/dev/null || true
sshdev 'ls -la /userdata/ /userdata/jetkvm/' > "$LOG_DIR/post-test-fs.txt" 2>/dev/null || true
sshdev 'dmesg | tail -100' > "$LOG_DIR/post-test-dmesg.txt" 2>/dev/null || true

if [ "$TEST_RESULT" -ne 0 ]; then
    echo -e "${YELLOW}Device logs saved to $LOG_DIR/${NC}"
fi

cd - >/dev/null
exit $TEST_RESULT
