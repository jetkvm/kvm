#!/bin/bash
set -e

SCRIPT_PATH=$(realpath "$(dirname $(realpath "${BASH_SOURCE[0]}"))")
source ${SCRIPT_PATH}/build_utils.sh

VERSION=
SET_AS_DEFAULT=false
SKIP_CONFIRMATION=false

while [[ $# -gt 0 ]]; do
  case $1 in
    -v|--version) VERSION="$2"; shift 2 ;;
    --set-as-default) SET_AS_DEFAULT=true; shift ;;
    --skip-confirmation) SKIP_CONFIRMATION=true; shift ;;
    --help)
      echo "Usage: $0 -v VERSION [--set-as-default] [--skip-confirmation]"
      echo "  -v VERSION         Version to deploy (required)"
      echo "  --set-as-default   Also deploy to root (production only)"
      echo "  --skip-confirmation Skip confirmation prompt"
      exit 0 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

[ -z "$VERSION" ] && { msg_err "Version required. Use -v VERSION"; exit 1; }

GIT_COMMIT=$(git rev-parse HEAD)
BUILD_TIMESTAMP=$(date -u +%FT%T%z)

cd ui
npm ci

# Build versioned app
msg_info "Building cloud app /v/${VERSION}/..."
VITE_CLOUD_ENABLE_VERSIONED_UI=true npm run build:prod -- --base=/v/${VERSION}/ --outDir dist/v/${VERSION}

# Build root app if --set-as-default
if [ "$SET_AS_DEFAULT" = true ]; then
  msg_info "Building root cloud app..."
  VITE_CLOUD_ENABLE_VERSIONED_UI=true npm run build:prod -- --outDir dist/root
fi

# Confirmation
if [ "$SKIP_CONFIRMATION" = false ]; then
  read -p "Deploy cloud app v${VERSION}? [y/N] " -n 1 -r
  echo ""
  [[ $REPLY =~ ^[Yy]$ ]] || { msg_err "Cancelled."; exit 0; }
fi

# Deploy versioned
msg_info "Deploying /v/${VERSION}/ to r2://jetkvm-cloud-app/v/${VERSION}..."
rclone copyto --progress \
  --header-upload="x-amz-meta-jetkvm-version: ${VERSION}" \
  --header-upload="x-amz-meta-jetkvm-build-ref: ${GIT_COMMIT}" \
  --header-upload="x-amz-meta-jetkvm-build-timestamp: ${BUILD_TIMESTAMP}" \
  dist/v/${VERSION} r2://jetkvm-cloud-app/v/${VERSION}

# Deploy root if --set-as-default
if [ "$SET_AS_DEFAULT" = true ]; then
  msg_info "Deploying root to r2://jetkvm-cloud-app..."
  rclone copyto --progress \
    --header-upload="x-amz-meta-jetkvm-version: ${VERSION}" \
    --header-upload="x-amz-meta-jetkvm-build-ref: ${GIT_COMMIT}" \
    --header-upload="x-amz-meta-jetkvm-build-timestamp: ${BUILD_TIMESTAMP}" \
    dist/root r2://jetkvm-cloud-app
fi

msg_ok "Deployed cloud app v${VERSION}"
