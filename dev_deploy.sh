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
    echo "      --skip-ui-build        Skip frontend/UI build"
    echo "      --help                 Display this help message"
    echo
    echo "Example:"
    echo "  $0 -r 192.168.0.17"
    echo "  $0 -r 192.168.0.17 -u admin"
}

# Default values
REMOTE_USER="root"
REMOTE_PATH="/userdata/jetkvm/bin"
SKIP_UI_BUILD=false

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
        --skip-ui-build)
            SKIP_UI_BUILD=true
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

# Verify required parameters
if [ -z "$REMOTE_HOST" ]; then
    echo "Error: Remote IP is a required parameter"
    show_help
fi

# Build the development version on the host
if [ "$SKIP_UI_BUILD" = false ]; then
    make frontend
fi
make build_dev

# Change directory to the binary output directory
cd bin

# Kill any existing instances of the application
ssh "${REMOTE_USER}@${REMOTE_HOST}" "killall jetkvm_app_debug || true"

# Copy the binary to the remote host
ssh "${REMOTE_USER}@${REMOTE_HOST}" "cat > ${REMOTE_PATH}/jetkvm_app_debug" < jetkvm_app

# Deploy and run the application on the remote host
ssh "${REMOTE_USER}@${REMOTE_HOST}" ash << EOF
set -e

# Set the library path to include the directory where librockit.so is located
export LD_LIBRARY_PATH=/oem/usr/lib:\$LD_LIBRARY_PATH

# Kill any existing instances of the application
killall jetkvm_app || true
killall jetkvm_app_debug || true

# Navigate to the directory where the binary will be stored
cd "${REMOTE_PATH}"

# Make the new binary executable
chmod +x jetkvm_app_debug

# Run the application in the background
PION_LOG_TRACE=jetkvm,cloud ./jetkvm_app_debug
EOF

echo "Deployment complete."
