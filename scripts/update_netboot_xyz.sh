#!/usr/bin/env bash
#
# Exit immediately if a command exits with a non-zero status
set -e

C_RST="$(tput sgr0)"
C_ERR="$(tput setaf 1)"
C_OK="$(tput setaf 2)"
C_WARN="$(tput setaf 3)"
C_INFO="$(tput setaf 5)"

msg() { printf '%s%s%s\n' $2 "$1" $C_RST; }

msg_info() { msg "$1" $C_INFO; }
msg_ok() { msg "$1" $C_OK; }
msg_err() { msg "$1" $C_ERR; }
msg_warn() { msg "$1" $C_WARN; }

# Get the latest release information
msg_info "Getting latest release information ..."
LATEST_RELEASE=$(curl -s \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  https://api.github.com/repos/netbootxyz/netboot.xyz/releases | jq '
  [.[] | select(.prerelease == false and .draft == false and .assets != null and (.assets | length > 0))] |
  sort_by(.created_at) | 
  .[-1]')

# Extract version, download URL, and digest
VERSION=$(echo "$LATEST_RELEASE" | jq -r '.tag_name')
ISO_URL=$(echo "$LATEST_RELEASE" | jq -r '.assets[] | select(.name == "netboot.xyz-multiarch.iso") | .browser_download_url')
EXPECTED_CHECKSUM=$(echo "$LATEST_RELEASE" | jq -r '.assets[] | select(.name == "netboot.xyz-multiarch.iso") | .digest' | sed 's/sha256://')

msg_ok "Latest version: $VERSION"
msg_ok "ISO URL: $ISO_URL"
msg_ok "Expected SHA256: $EXPECTED_CHECKSUM"


# Check if we already have the same version
if [ -f "resource/netboot.xyz-multiarch.iso" ]; then
    msg_info "Checking current resource file ..."
    
    # First check by checksum (fastest)
    CURRENT_CHECKSUM=$(shasum -a 256 resource/netboot.xyz-multiarch.iso | awk '{print $1}')
    
    if [ "$CURRENT_CHECKSUM" = "$EXPECTED_CHECKSUM" ]; then
        msg_ok "Resource file is already up to date (version $VERSION). No update needed."
        exit 0
    else
        msg_info "Checksums differ, proceeding with download ..."
    fi
fi

# Download ISO file
TMP_ISO=$(mktemp -t netbootxyziso)
msg_info "Downloading ISO file ..."
curl -L -o "$TMP_ISO" "$ISO_URL"

# Verify SHA256 checksum
msg_info "Verifying SHA256 checksum ..."
ACTUAL_CHECKSUM=$(shasum -a 256 "$TMP_ISO" | awk '{print $1}')

if [ "$EXPECTED_CHECKSUM" = "$ACTUAL_CHECKSUM" ]; then
    msg_ok "Verified SHA256 checksum."
    mv -f "$TMP_ISO" "resource/netboot.xyz-multiarch.iso"
    msg_ok "Updated ISO file."
    git add "resource/netboot.xyz-multiarch.iso"
    git commit -m "chore: update netboot.xyz-multiarch.iso to $VERSION"
    msg_ok "Committed changes."
    msg_ok "You can now push the changes to the remote repository."
    exit 0
else
    msg_err "Inconsistent SHA256 checksum."
    msg_err "Expected: $EXPECTED_CHECKSUM"
    msg_err "Actual:   $ACTUAL_CHECKSUM"
    exit 1
fi