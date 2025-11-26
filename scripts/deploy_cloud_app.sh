#!/bin/bash
set -e

SCRIPT_PATH=$(realpath "$(dirname $(realpath "${BASH_SOURCE[0]}"))")
source ${SCRIPT_PATH}/build_utils.sh

function show_help() {
  echo "Usage: $0 [options]"
  echo "Options:"
  echo "  -b, --branch <branch>    Checkout branch"
  echo "      --set-as-default     Set as default"
  echo "      --skip-confirmation  Skip confirmation"
  echo "  --help                   Show help"
}

# Parse command line arguments
CHECKOUT_BRANCH=
SET_AS_DEFAULT=false
SKIP_CONFIRMATION=false
while [[ $# -gt 0 ]]; do
  case $1 in
    -b|--branch)
      CHECKOUT_BRANCH="$2"
      shift 2
    ;;
    --set-as-default)
      SET_AS_DEFAULT=true
      shift
    ;;
    --skip-confirmation)
      SKIP_CONFIRMATION=true
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


# Checkout current branch in a new temporary directory
# only popd when exiting the script
TMP_DIR=$(mktemp -d)
trap 'popd > /dev/null && rm -rf ${TMP_DIR}' EXIT
msg_info "Copying repository to a new temporary directory ${TMP_DIR} ..."
# git fetch origin ${CH}ECKOUT_BRANCH:${CHECKOUT_BRANCH}
git clone . ${TMP_DIR}
cp ${SCRIPT_PATH}/versioned.patch ${TMP_DIR}
msg_info "Checking out branch ${CHECKOUT_BRANCH} ..."
pushd ${TMP_DIR} > /dev/null
git checkout ${CHECKOUT_BRANCH}


# CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
# # Verify branch name matches release/x.x.x or release/x.x.x-dev...
# if [[ ! $CURRENT_BRANCH =~ ^(release|release-cloud-app)/[0-9]+\.[0-9]+\.[0-9]+(-dev[0-9]+)?$ ]]; then
#   msg_err "Current branch '$CURRENT_BRANCH' does not match required pattern"
#   msg_err "Expected: release/x.x.x OR release/x.x.x-dev20241104123632"
#   exit 1
# fi

CURRENT_BRANCH=release/0.5.0

GIT_COMMIT=$(git rev-parse HEAD)
BUILD_TIMESTAMP=$(date -u +%FT%T%z)
VERSION=${CURRENT_BRANCH#release/}
VERSION=${VERSION#release-cloud-app/}
if [[ ! $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+(-dev[0-9]+)?$ ]]; then
  msg_err "Version '$VERSION' does not match required pattern"
  msg_err "Expected: x.x.x OR x.x.x-dev20241104123632"
  exit 1
fi

# Change to ui directory
cd ui

if [ "$SET_AS_DEFAULT" = true ]; then
  # Build for root dist
  msg_info "Building for root dist..."
  npm ci
  npm run build:prod
fi

# Build for versioned dist/v/VERSION
msg_info "Building for dist/v/${VERSION}..."
npm ci
npm run build:prod -- --base=/v/${VERSION}/ --outDir dist/v/${VERSION}

# Ask for confirmation
if [ "$SKIP_CONFIRMATION" = false ]; then
read -p "Do you want to deploy the cloud app to production? (y/N): " -n 1 -r
echo ""
  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    msg_err "Deployment cancelled."
    exit 0
  fi
fi

# Deploy to production
msg_info "Deploying to r2://jetkvm-cloud-app..."
rclone copyto \
  --progress \
  --stats=1s \
  --header-upload="x-amz-meta-jetkvm-version: ${VERSION}" \
  --header-upload="x-amz-meta-jetkvm-build-ref: ${GIT_COMMIT}" \
  --header-upload="x-amz-meta-jetkvm-build-timestamp: ${BUILD_TIMESTAMP}" \
  dist \
r2://jetkvm-cloud-app

msg_ok "Successfully deployed v${VERSION} to production"
