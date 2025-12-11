#!/bin/bash
set -e

# Get absolute path of this script for recursive calls
SCRIPT_PATH="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"

DEVICE_IP="$1"
BINARY_PATH="$2"
ACTION="$3"      # "deploy", "restore", or "test"
VERSION="$4"     # required for "test" action

REMOTE_USER="root"
REMOTE_BIN_PATH="/userdata/jetkvm/bin"
REMOTE_UPDATE_PATH="/userdata/jetkvm"
SSH_OPTS="-o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -o ConnectTimeout=10 -o LogLevel=ERROR"

ssh_cmd() { ssh $SSH_OPTS "${REMOTE_USER}@${DEVICE_IP}" "$@"; }

case "$ACTION" in
  deploy)
    echo "Backing up current binary..."
    ssh_cmd "cp ${REMOTE_BIN_PATH}/jetkvm_app ${REMOTE_BIN_PATH}/jetkvm_app.pre_release_backup 2>/dev/null || true"
    echo "Deploying new binary via OTA update mechanism..."
    ssh_cmd "cat > ${REMOTE_UPDATE_PATH}/jetkvm_app.update" < "$BINARY_PATH"
    echo "Rebooting device..."
    ssh_cmd "reboot" || true
    ;;
  restore)
    echo "Restoring backup..."
    ssh_cmd "cp ${REMOTE_BIN_PATH}/jetkvm_app.pre_release_backup ${REMOTE_BIN_PATH}/jetkvm_app"
    echo "Rebooting device..."
    ssh_cmd "reboot" || true
    ;;
  test)
    # Full automated test flow: version verification + E2E tests
    [ -z "$VERSION" ] && { echo "Error: VERSION required for test action"; exit 1; }

    # Get the repo root directory (parent of scripts/)
    SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
    REPO_ROOT="$(dirname "$SCRIPT_DIR")"

    echo ""
    echo "═══════════════════════════════════════════════════════"
    echo "  Testing $VERSION on $DEVICE_IP"
    echo "═══════════════════════════════════════════════════════"
    echo ""

    echo "Step 1: Deploying $VERSION to $DEVICE_IP..."
    "$SCRIPT_PATH" "$DEVICE_IP" "$BINARY_PATH" deploy

    echo ""
    echo "Step 2: Waiting for device to come back online..."
    sleep 10  # Initial wait for reboot
    for i in {1..30}; do
      if curl -sf "http://$DEVICE_IP/device/status" > /dev/null 2>&1; then
        echo "Device is online"
        break
      fi
      echo "Waiting... ($i/30)"
      sleep 2
    done

    # Extra wait for services to fully start
    sleep 5

    echo ""
    echo "Step 3: Verifying deployed version..."
    # Get version from Prometheus metrics endpoint
    DEPLOYED_VERSION=$(curl -sf "http://$DEVICE_IP/metrics" | grep 'jetkvm_build_info' | sed -n 's/.*version="\([^"]*\)".*/\1/p')
    echo "  Expected: $VERSION"
    echo "  Deployed: $DEPLOYED_VERSION"

    if [ "$DEPLOYED_VERSION" != "$VERSION" ]; then
      echo ""
      echo "❌ Version mismatch! Restoring previous binary..."
      "$SCRIPT_PATH" "$DEVICE_IP" "$BINARY_PATH" restore
      exit 1
    fi
    echo "  ✓ Version matches"

    echo ""
    echo "Step 4: Running E2E tests..."
    E2E_RESULT=0
    cd "$REPO_ROOT/ui" && NODE_NO_WARNINGS=1 JETKVM_URL="http://$DEVICE_IP" npm run test:e2e || E2E_RESULT=$?

    echo ""
    echo "Step 5: Restoring device to previous binary..."
    "$SCRIPT_PATH" "$DEVICE_IP" "$BINARY_PATH" restore

    if [ $E2E_RESULT -ne 0 ]; then
      echo ""
      echo "❌ E2E tests failed."
      exit 1
    fi

    echo ""
    echo "✅ All tests passed for $VERSION"
    ;;
  *)
    echo "Usage: $SCRIPT_PATH <device_ip> <binary_path> <deploy|restore|test> [version]"
    exit 1
    ;;
esac
