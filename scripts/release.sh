#!/bin/bash
set -eE
set -o pipefail

SCRIPT_PATH=$(realpath "$(dirname $(realpath "${BASH_SOURCE[0]}"))")
source ${SCRIPT_PATH}/build_utils.sh

# Function to display help message
show_help() {
    echo "Usage: $0 [options] -v <version>"
    echo
    echo "Required:"
    echo "  --app-version <version>      App version to release"
    echo "  --system-version <version>   System version to release"
    echo
    echo "Optional:"
    echo "  -u, --user <remote_user>   Remote username (default: root)"
    echo "      --run-go-tests         Run go tests"
    echo "      --run-go-tests-only    Run go tests and exit"
    echo "      --skip-ui-build        Skip frontend/UI build"
    echo "      --skip-native-build    Skip native build"
    echo "      --disable-docker       Disable docker build"
    echo "  -i, --install              Build for release and install the app"
    echo "      --help                 Display this help message"
    echo
    echo "Example:"
    echo "  $0 --system-version 0.2.6"
}


BUILD_VERSION=$1
R2_PATH="r2://jetkvm-update/system"
PACK_BIN_PATH="./tools/linux/Linux_Pack_Firmware"
UNPACK_BIN="${PACK_BIN_PATH}/mk-update_unpack.sh"

# Create temporary directory for downloads
TEMP_DIR=$(mktemp -d)
msg_ok "Created temporary directory: $TEMP_DIR"

# Cleanup function
cleanup() {
    if [ -d "$TEMP_DIR" ]; then
        msg_info "Cleaning up temporary directory: $TEMP_DIR"
        rm -rf "$TEMP_DIR"
    fi
}

# Set trap to cleanup on exit
# trap cleanup EXIT

mkdir -p ${TEMP_DIR}/extracted-update
${UNPACK_BIN} -i update.img -o ${TEMP_DIR}/extracted-update

exit 0
# Check if the version already exists
if rclone lsf $R2_PATH/$BUILD_VERSION/ | grep -q .; then
    msg_err "Error: Version $BUILD_VERSION already exists in the remote storage."
    exit 1
fi

# Check if the version exists in the github
RELEASE_URL="https://api.github.com/repos/jetkvm/rv1106-system/releases/tags/v$BUILD_VERSION"

# Download the release JSON
RELEASE_JSON=$(curl -s $RELEASE_URL)

# Check if the release has assets we need
if echo $RELEASE_JSON | jq -e '.assets | length == 0' > /dev/null; then
    msg_err "Error: Version $BUILD_VERSION does not have assets we need."
    exit 1
fi

function get_file_by_name() {
    local file_name=$1
    local file_url=$(echo $RELEASE_JSON | jq -r ".assets[] | select(.name == \"$file_name\") | .browser_download_url")
    if [ -z "$file_url" ]; then
        msg_err "Error: File $file_name not found in the release."
        exit 1
    fi
    local digest=$(echo $RELEASE_JSON | jq -r ".assets[] | select(.name == \"$file_name\") | .digest")
    local temp_file_path="$TEMP_DIR/$file_name"

    msg_info "Downloading $file_name: $file_url"

    # Download the file to temporary directory
    curl -L -o "$temp_file_path" "$file_url"
    
    # Verify digest if available
    if [ "$digest" != "null" ] && [ -n "$digest" ]; then
        msg_info "Verifying digest for $file_name ..."
        local calculated_digest=$(sha256sum "$temp_file_path" | cut -d' ' -f1)
        # Strip "sha256:" prefix if present
        local expected_digest=$(echo "$digest" | sed 's/^sha256://')
        if [ "$calculated_digest" != "$expected_digest" ]; then
            msg_err "🙅 Digest verification failed for $file_name"
            msg_info "Expected: $expected_digest"
            msg_info "Calculated: $calculated_digest"
            exit 1
        fi
    else
        msg_warn "Warning: No digest available for $file_name, skipping verification"
    fi
    
    msg_ok "✅ $file_name downloaded and verified."
}

get_file_by_name "update_ota.tar"
get_file_by_name "update.img"

strings -d bin/jetkvm_app | grep -x '0.4.8'

# Ask for confirmation
msg_info "Do you want to continue with the release? (y/n)"
read -n 1 -s -r -p "Press y to continue, any other key to exit"
echo -ne "\n"
if [ "$REPLY" != "y" ]; then
    msg_err "🙅 Release cancelled."
    exit 1
fi

msg_info "Releasing $BUILD_VERSION..."

sha256sum $TEMP_DIR/update_ota.tar | awk '{print $1}' > $TEMP_DIR/update_ota.tar.sha256
sha256sum $TEMP_DIR/update.img | awk '{print $1}' > $TEMP_DIR/update.img.sha256

# Check if the version already exists
msg_info "Copying to $R2_PATH/$BUILD_VERSION/"

rclone copyto --progress $TEMP_DIR/update_ota.tar $R2_PATH/$BUILD_VERSION/system.tar
rclone copyto --progress $TEMP_DIR/update_ota.tar.sha256 $R2_PATH/$BUILD_VERSION/system.tar.sha256
rclone copyto --progress $TEMP_DIR/update.img $R2_PATH/$BUILD_VERSION/update.img
rclone copyto --progress $TEMP_DIR/update.img.sha256 $R2_PATH/$BUILD_VERSION/update.img.sha256

msg_ok "✅ $BUILD_VERSION released."