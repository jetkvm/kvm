#!/bin/bash
set -e

DEVICE_IP="$1"
BINARY_PATH="$2"
ACTION="$3"  # "deploy" or "restore"

REMOTE_USER="root"
REMOTE_BIN_PATH="/userdata/jetkvm/bin"
REMOTE_UPDATE_PATH="/userdata/jetkvm"
SSH_OPTS="-o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -o ConnectTimeout=10"

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
  *)
    echo "Usage: $0 <device_ip> <binary_path> <deploy|restore>"
    exit 1
    ;;
esac

