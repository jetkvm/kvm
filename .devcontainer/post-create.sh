#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

log() {
  echo "[post-create] $*"
}

ensure_user_writable_dir() {
  local dir_path="$1"
  local uid gid
  uid="$(id -u)"
  gid="$(id -g)"

  mkdir -p "${dir_path}" >/dev/null 2>&1 || true
  if [ -w "${dir_path}" ]; then
    return 0
  fi

  if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then
    sudo mkdir -p "${dir_path}" >/dev/null 2>&1 || true
    sudo chown -R "${uid}:${gid}" "${dir_path}" >/dev/null 2>&1 || true
  fi
}

safe_dir() {
  local path="$1"
  if [ -z "${path}" ]; then
    return 0
  fi
  if ! git config --global --get-all safe.directory | grep -Fxq "${path}"; then
    git config --global --add safe.directory "${path}" || true
  fi
}

mkdir_if_missing() {
  local path="$1"
  if [ ! -d "${path}" ]; then
    sudo mkdir -p "${path}" 2>/dev/null || mkdir -p "${path}"
  fi
}

link_if_available() {
  local source_path="$1"
  local target_path="$2"

  if [ ! -d "${source_path}" ]; then
    return 0
  fi

  if [ -L "${target_path}" ]; then
    return 0
  fi

  if [ -d "${target_path}" ] && [ -z "$(ls -A "${target_path}" 2>/dev/null)" ]; then
    sudo rm -rf "${target_path}" 2>/dev/null || rm -rf "${target_path}"
  fi

  if [ -e "${target_path}" ]; then
    log "keeping existing SDK path: ${target_path}"
    return 0
  fi

  sudo ln -s "${source_path}" "${target_path}" 2>/dev/null || ln -s "${source_path}" "${target_path}"
}

log "configuring git safe directories"
git config --global --add safe.directory "*" >/dev/null 2>&1 || true
safe_dir "${REPO_ROOT}"
safe_dir "${REPO_ROOT}/.git"

log "configuring git url rewrites (https -> ssh) for GitHub"
# Required for private submodules/modules when the checkout references https URLs.
git config --global url."git@github.com:".insteadOf "https://github.com/" || true
git config --global url."ssh://git@github.com/".insteadOf "https://github.com/" || true

# Named Docker volumes may appear root-owned on first attach (breaks go clean/npm).
ensure_user_writable_dir /home/vscode/go/pkg/mod
ensure_user_writable_dir /home/vscode/.cache/go-build
ensure_user_writable_dir /home/vscode/.npm
ensure_user_writable_dir /home/vscode/.local/share/pnpm/store

mkdir_if_missing /workspaces/vendor-sources

safe_dir "/workspaces/vendor-sources"
safe_dir "/workspaces/vendor-sources/jetkvm-sdk"
safe_dir "/workspaces/vendor-sources/comet-kernel"
safe_dir "/workspaces/vendor-sources/nanokvm-sdk"
safe_dir "/workspaces/vendor-sources/jetkvm-app"
safe_dir "/workspaces/vendor-sources/comet-app"
safe_dir "/workspaces/vendor-sources/nanokvm-app"
safe_dir "/workspaces/vendor-sources/mpp-mirror"
safe_dir "/workspaces/vendor-sources/librga-upstream"
safe_dir "/workspaces/vendor-sources/librga-mirror"
safe_dir "/workspaces/vendor-sources/rknn-toolkit2"
safe_dir "/workspaces/vendor-sources/rknn-model-zoo"
safe_dir "/workspaces/vendor-sources/rknn-toolkit-legacy"
safe_dir "/workspaces/vendor-sources/rknpu-current"
safe_dir "/workspaces/vendor-sources/rknpu-legacy"
safe_dir "/workspaces/vendor-sources/openh264"

if [ -f "${REPO_ROOT}/.devcontainer/vendor-setup.sh" ]; then
  bash "${REPO_ROOT}/.devcontainer/vendor-setup.sh"
fi

mkdir_if_missing /opt/kvm-sdks/rv1126b
mkdir_if_missing /opt/kvm-sdks/ax630c
link_if_available "/workspaces/vendor-sources/comet-kernel" "/opt/kvm-sdks/rv1126b"
link_if_available "/workspaces/vendor-sources/nanokvm-sdk" "/opt/kvm-sdks/ax630c"
if [ -d "/opt/kvm-sdks" ]; then
  sudo chown -R vscode:vscode /opt/kvm-sdks
fi

if [ -f "${REPO_ROOT}/.gitmodules" ]; then
  log "initializing git submodules"
  git -C "${REPO_ROOT}" submodule sync --recursive || true
  git -C "${REPO_ROOT}" submodule update --init --recursive --jobs 8 || true
fi
