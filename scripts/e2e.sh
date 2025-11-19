#!/usr/bin/env bash
#
# Exit immediately if a command exits with a non-zero status
set -e

SCRIPT_PATH=$(realpath "$(dirname $(realpath "${BASH_SOURCE[0]}"))")
source ${SCRIPT_PATH}/build_utils.sh

# Function to display help message
show_help() {
    echo "Usage: $0 [options] -r <remote_ip>"
    echo
    echo "Required:"
    echo "  -r, --remote <remote_ip>   Remote host IP address"
    echo
    echo "Example:"
    echo "  $0 -r 192.168.0.17"
}

REMOTE_HOST=""
PAUSE_AFTER_PUSH=0

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -r|--remote)
            REMOTE_HOST="$2"
            shift 2
        ;;
        --help)
            show_help
            exit 0
        ;;
        --pause)
            PAUSE_AFTER_PUSH=1
            shift
        ;;
        *)
            echo "Unknown option: $1"
            show_help
            exit 1
        ;;
    esac
done

# Verify required parameters
if [ -z "$REMOTE_HOST" ]; then
    msg_err "Error: Remote IP is a required parameter"
    show_help
    exit 1
fi

function sshrun() {
    ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no "root@${REMOTE_HOST}" "$@"
} 

msg_info "▶ Pushing baseline version to remote host"
gzip -9c bin/jetkvm_app | pv | sshrun "gzip -d > /userdata/jetkvm/jetkvm_app.update"

msg_info "▶ Rebooting device"
sshrun ash << EOF
set -e
sync
echo 1 > /proc/sys/vm/drop_caches
reboot -d 5 -f &
exit 0
EOF

if [ $PAUSE_AFTER_PUSH -eq 1 ]; then
    read -p "Press Enter to continue"
fi

sleep 5

pushd ui > /dev/null
# npx playwright test --headed -g 'standard upgrade process'
npx playwright test --headed -g 'custom upgrade process: upgrade app only'
popd > /dev/null