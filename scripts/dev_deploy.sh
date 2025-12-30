#!/usr/bin/env bash
#
# Exit immediately if a command exits with a non-zero status
set -e

# Function to display help message
show_help() {
    echo "Usage: $0 [options] -r <remote_ip>"
    echo
    echo "Required:"
    echo "  -r, --remote <remote_ip>   Remote host IP address"
    echo
    echo "Optional:"
    echo "  -u, --user <remote_user>   Remote username (default: root)"
    echo "      --gdb-port <port>      GDB debug port (default: 2345)"
    echo "      --run-go-tests         Run go tests"
    echo "      --run-go-tests-only    Run go tests and exit"
    echo "      --skip-ui-build        Skip frontend/UI build"
    echo "      --skip-native-build    Skip native build"
    echo "      --log-trace <scopes>   Comma-separated scopes to trace"
    echo "                             (e.g., usb, usb,hidrpc, all)"
    echo "      --disable-docker       Disable docker build"
    echo "      --enable-sync-trace    Enable sync trace (do not use in release builds)"
    echo "      --native-binary        Build and deploy the native binary (FOR DEBUGGING ONLY)"
    echo "  -i, --install              Build for release and install the app"
    echo "      --help                 Display this help message"
    echo
    echo "Example:"
    echo "  $0 -r 192.168.0.17"
    echo "  $0 -r 192.168.0.17 -u admin"
}

# Function to check if device is pingable
check_ping() {
    local host=$1
    msg_info "▶ Checking if device is reachable at ${host}..."
    if ! ping -c 3 -W 5 "${host}" > /dev/null 2>&1; then
        msg_err "Error: Cannot reach device at ${host}"
        msg_err "Please verify the IP address and network connectivity"
        exit 1
    fi
    msg_info "✓ Device is reachable"
}

# Function to check if SSH is accessible
check_ssh() {
    local user=$1
    local host=$2
    msg_info "▶ Checking SSH connectivity to ${user}@${host}..."
    if ! ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -o ConnectTimeout=10 "${user}@${host}" "echo 'SSH connection successful'" > /dev/null 2>&1; then
        msg_err "Error: Cannot establish SSH connection to ${user}@${host}"
        msg_err "Please verify SSH access and credentials"
        exit 1
    fi
    msg_info "✓ SSH connection successful"
}

# Default values
SCRIPT_PATH=$(realpath "$(dirname $(realpath "${BASH_SOURCE[0]}"))")
REMOTE_USER="root"
REMOTE_PATH="/userdata/jetkvm/bin"
SKIP_UI_BUILD=false
SKIP_UI_BUILD_RELEASE=0
SKIP_NATIVE_BUILD=0
GDB_DEBUG_PORT=2345
BUILD_NATIVE_BINARY=false
ENABLE_SYNC_TRACE=0
RESET_USB_HID_DEVICE=false
LOG_TRACE_SCOPES="${LOG_TRACE_SCOPES:-jetkvm,cloud,websocket,native,jsonrpc}"  # Scopes to enable TRACE logging for
RUN_GO_TESTS=false
RUN_GO_TESTS_ONLY=false
INSTALL_APP=false
BUILD_IN_DOCKER=true
DOCKER_BUILD_DEBUG=false
DOCKER_BUILD_TAG=ghcr.io/jetkvm/buildkit:latest

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -r|--remote)
            REMOTE_HOST="$2"
            shift 2
            ;;
        -u|--user)
            REMOTE_USER="$2"
            shift 2
            ;;
        --gdb-port)
            GDB_DEBUG_PORT="$2"
            shift 2
            ;;
        --skip-ui-build)
            SKIP_UI_BUILD=true
            shift
            ;;
        --skip-native-build)
            SKIP_NATIVE_BUILD=1
            shift
            ;;
        --log-trace)
            LOG_TRACE_SCOPES="$2"
            shift 2
            ;;
        --reset-usb-hid)
            RESET_USB_HID_DEVICE=true
            shift
            ;;
        --enable-sync-trace)
            ENABLE_SYNC_TRACE=1
            LOG_TRACE_SCOPES="${LOG_TRACE_SCOPES},synctrace"
            shift
            ;;
        --disable-docker)
            BUILD_IN_DOCKER=false
            shift
            ;;
        --docker-build-debug)
            DOCKER_BUILD_DEBUG=true
            shift
            ;;
        --run-go-tests)
            RUN_GO_TESTS=true
            shift
            ;;
        --run-go-tests-only)
            RUN_GO_TESTS_ONLY=true
            RUN_GO_TESTS=true
            shift
            ;;
        --native-binary)
            BUILD_NATIVE_BINARY=true
            shift
            ;;
        -i|--install)
            INSTALL_APP=true
            shift
            ;;
        --help)
            show_help
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

if [ "$ENABLE_SYNC_TRACE" = 1 ]; then
    if [[ ! "$LOG_TRACE_SCOPES" =~ synctrace ]]; then
        LOG_TRACE_SCOPES="${LOG_TRACE_SCOPES},synctrace"
    fi
fi

source ${SCRIPT_PATH}/build_utils.sh

# Verify required parameters
if [ -z "$REMOTE_HOST" ]; then
    msg_err "Error: Remote IP is a required parameter"
    show_help
    exit 1
fi

# Check device connectivity before proceeding
check_ping "${REMOTE_HOST}"
check_ssh "${REMOTE_USER}" "${REMOTE_HOST}"
function sshdev() {
    ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no "${REMOTE_USER}@${REMOTE_HOST}" "$@"
    return $?
}

# check if the current CPU architecture is x86_64
if [ "$(uname -m)" != "x86_64" ]; then
    msg_warn "Warning: This script is only supported on x86_64 architecture"
    BUILD_IN_DOCKER=true
fi

if [ "$BUILD_IN_DOCKER" = true ]; then
    build_docker_image
fi

if [ "$BUILD_NATIVE_BINARY" = true ]; then
    msg_info "▶ Building native binary"
    CMAKE_BUILD_TYPE=Debug make build_native
    msg_info "▶ Checking if GDB is available on remote host"
    if ! sshdev "command -v gdbserver > /dev/null 2>&1"; then
        msg_warn "Error: gdbserver is not installed on the remote host"
        tar -czf - -C /opt/jetkvm-native-buildkit/gdb/ . | sshdev "tar -xzf - -C /usr/bin"
        msg_info "✓ gdbserver installed on remote host"
    fi
    msg_info "▶ Stopping any existing instances of jetkvm_native_debug on remote host"
    sshdev "killall -9 jetkvm_app jetkvm_app_debug jetkvm_native_debug gdbserver || true >> /dev/null 2>&1"
    sshdev "cat > ${REMOTE_PATH}/jetkvm_native_debug" < internal/native/cgo/build/jknative-bin
    sshdev -t ash << EOF
set -e

# Set the library path to include the directory where librockit.so is located
export LD_LIBRARY_PATH=/oem/usr/lib:\$LD_LIBRARY_PATH

cd ${REMOTE_PATH}
killall -9 jetkvm_app jetkvm_app_debug jetkvm_native_debug || true
sleep 5
echo 'V' > /dev/watchdog
chmod +x jetkvm_native_debug
gdbserver localhost:${GDB_DEBUG_PORT} ./jetkvm_native_debug
EOF
    exit 0
fi

# Build the development version on the host
# When using `make build_release`, the frontend will be built regardless of the `SKIP_UI_BUILD` flag
# check if static/index.html exists
if [[ "$SKIP_UI_BUILD" = true && ! -f "static/index.html" ]]; then
    msg_warn "static/index.html does not exist, forcing UI build"
    SKIP_UI_BUILD=false
fi

if [[ "$SKIP_UI_BUILD" = false && "$JETKVM_INSIDE_DOCKER" != 1 ]]; then
    msg_info "▶ Building frontend"
    make frontend SKIP_UI_BUILD=0
    SKIP_UI_BUILD_RELEASE=1
fi

if [[ "$SKIP_UI_BUILD_RELEASE" = 0 && "$BUILD_IN_DOCKER" = true ]]; then
    msg_info "UI build is skipped when building in Docker"
    SKIP_UI_BUILD_RELEASE=1
fi

if [ "$RUN_GO_TESTS" = true ]; then
    msg_info "▶ Building go tests"
    make build_dev_test

    msg_info "▶ Copying device-tests.tar.gz to remote host"
    sshdev "cat > /tmp/device-tests.tar.gz" < device-tests.tar.gz

    msg_info "▶ Running go tests"
    sshdev ash << 'EOF'
set -e
TMP_DIR=$(mktemp -d)
cd ${TMP_DIR}
tar zxf /tmp/device-tests.tar.gz
./gotestsum --format=testdox \
    --jsonfile=/tmp/device-tests.json \
    --post-run-command 'sh -c "echo $TESTS_FAILED > /tmp/device-tests.failed"' \
    --raw-command -- ./run_all_tests -json

GOTESTSUM_EXIT_CODE=$?
if [ $GOTESTSUM_EXIT_CODE -ne 0 ]; then
    echo "❌ Tests failed (exit code: $GOTESTSUM_EXIT_CODE)"
    rm -rf ${TMP_DIR} /tmp/device-tests.tar.gz
    exit 1
fi

TESTS_FAILED=$(cat /tmp/device-tests.failed)
if [ "$TESTS_FAILED" -ne 0 ]; then
    echo "❌ Tests failed $TESTS_FAILED tests failed"
    rm -rf ${TMP_DIR} /tmp/device-tests.tar.gz
    exit 1
fi

echo "✅ Tests passed"
rm -rf ${TMP_DIR} /tmp/device-tests.tar.gz
EOF

    if [ "$RUN_GO_TESTS_ONLY" = true ]; then
        msg_info "▶ Go tests completed"
        exit 0
    fi
fi

if [ "$INSTALL_APP" = true ]
then
	msg_info "▶ Building release binary"
	do_make build_release \
    SKIP_NATIVE_IF_EXISTS=${SKIP_NATIVE_BUILD} \
    SKIP_UI_BUILD=${SKIP_UI_BUILD_RELEASE} \
    ENABLE_SYNC_TRACE=${ENABLE_SYNC_TRACE}

	# Copy the binary to the remote host as if we were the OTA updater.
	sshdev "cat > /userdata/jetkvm/jetkvm_app.update" < bin/jetkvm_app

	# Reboot the device, the new app will be deployed by the startup process.
	sshdev "reboot"
else
	msg_info "▶ Building development binary"
	do_make build_dev \
    SKIP_NATIVE_IF_EXISTS=${SKIP_NATIVE_BUILD} \
    SKIP_UI_BUILD=${SKIP_UI_BUILD_RELEASE} \
    ENABLE_SYNC_TRACE=${ENABLE_SYNC_TRACE}

	# Kill any existing instances of the application
	sshdev "killall jetkvm_app_debug || true"

	# Copy the binary to the remote host
	sshdev "cat > ${REMOTE_PATH}/jetkvm_app_debug" < bin/jetkvm_app

	if [ "$RESET_USB_HID_DEVICE" = true ]; then
	msg_info "▶ Resetting USB HID device"
	msg_warn "The option has been deprecated and will be removed in a future version, as JetKVM will now reset USB gadget configuration when needed"
	# Remove the old USB gadget configuration
	sshdev "rm -rf /sys/kernel/config/usb_gadget/jetkvm/configs/c.1/hid.usb*"
	sshdev "ls /sys/class/udc > /sys/kernel/config/usb_gadget/jetkvm/UDC"
	fi

	# Deploy and run the application on the remote host
	sshdev ash << EOF
set -e

# Set the library path to include the directory where librockit.so is located
export LD_LIBRARY_PATH=/oem/usr/lib:\$LD_LIBRARY_PATH

# Kill any existing instances of the application
killall jetkvm_app || true
killall jetkvm_app_debug || true

# Wait until both binaries are killed, max 10 seconds
i=1
while [ \$i -le 10 ]; do
    echo "Waiting for jetkvm_app and jetkvm_app_debug to be killed, \$i/10 ..."
    if ! pgrep -f "jetkvm_app" > /dev/null && ! pgrep -f "jetkvm_app_debug" > /dev/null; then
        break
    fi
    sleep 1
    i=\$((i + 1))
done

# Navigate to the directory where the binary will be stored
cd "${REMOTE_PATH}"

# Make the new binary executable
chmod +x jetkvm_app_debug

# Run the application with logging configuration
if [ -n "${LOG_TRACE_SCOPES}" ]; then
    export JETKVM_LOG_ERROR=all
    export JETKVM_LOG_TRACE="${LOG_TRACE_SCOPES}"
fi
./jetkvm_app_debug | tee -a /tmp/jetkvm_app_debug.log
EOF
fi

echo "Deployment complete."
