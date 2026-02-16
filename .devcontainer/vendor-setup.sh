#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${1:-$(cd "$(dirname "$0")/.." && pwd)}"
VENDOR_ROOT="${HAL_VENDOR_ROOT:-/workspaces/vendor-sources}"
SUBMODULE_JOBS="${HAL_VENDOR_SUBMODULE_JOBS:-8}"
FETCH_DEPTH="${HAL_VENDOR_FETCH_DEPTH:-1}"

# Platform source trees.
JETKVM_KERNEL_REPO="${JETKVM_KERNEL_REPO_URL:-https://github.com/jetkvm/rv1106-system.git}"
COMET_KERNEL_REPO="${COMET_KERNEL_REPO_URL:-https://github.com/gl-inet/kernel-6.1.git}"
NANOKVM_SDK_REPO="${NANOKVM_SDK_REPO_URL:-https://github.com/sipeed/maix_ax620e_sdk.git}"
JETKVM_APP_REPO="${JETKVM_APP_REPO_URL:-https://github.com/jetkvm/kvm.git}"
COMET_APP_REPO="${COMET_APP_REPO_URL:-https://github.com/gl-inet/glkvm.git}"
NANOKVM_APP_REPO="${NANOKVM_APP_REPO_URL:-https://github.com/sipeed/NanoKVM.git}"

# Rockchip vendor trees and mirrors.
# The upstream MPP repository is often unavailable publicly; the maintained mirror
# is used by default but can be overridden with ROCKCHIP_MPP_REPO_URL.
ROCKCHIP_MPP_REPO="${ROCKCHIP_MPP_REPO_URL:-https://github.com/tsukumijima/mpp-rockchip.git}"
ROCKCHIP_RGA_UPSTREAM_REPO="${ROCKCHIP_RGA_UPSTREAM_REPO_URL:-https://github.com/airockchip/librga.git}"
ROCKCHIP_RGA_MIRROR_REPO="${ROCKCHIP_RGA_MIRROR_REPO_URL:-https://github.com/tsukumijima/librga-rockchip.git}"
ROCKCHIP_RKNN_TOOLKIT2_REPO="${ROCKCHIP_RKNN_TOOLKIT2_REPO_URL:-https://github.com/airockchip/rknn-toolkit2.git}"
ROCKCHIP_RKNN_MODEL_ZOO_REPO="${ROCKCHIP_RKNN_MODEL_ZOO_REPO_URL:-https://github.com/airockchip/rknn_model_zoo.git}"
ROCKCHIP_RKNN_TOOLKIT_LEGACY_REPO="${ROCKCHIP_RKNN_TOOLKIT_LEGACY_REPO_URL:-https://github.com/rockchip-linux/rknn-toolkit.git}"
ROCKCHIP_RKNPU_CURRENT_REPO="${ROCKCHIP_RKNPU_CURRENT_REPO_URL:-https://github.com/airockchip/rknpu2.git}"
ROCKCHIP_RKNPU_LEGACY_REPO="${ROCKCHIP_RKNPU_LEGACY_REPO_URL:-https://github.com/rockchip-linux/rknpu.git}"
OPENH264_REPO="${OPENH264_REPO_URL:-https://github.com/cisco/openh264.git}"

JETKVM_KERNEL_SRC="${JETKVM_KERNEL_SRC:-${VENDOR_ROOT}/jetkvm-sdk}"
COMET_KERNEL_SRC="${COMET_KERNEL_SRC:-${VENDOR_ROOT}/comet-kernel}"
NANOKVM_SDK_SRC="${NANOKVM_SDK_SRC:-${VENDOR_ROOT}/nanokvm-sdk}"
JETKVM_APP_SRC="${JETKVM_APP_SRC:-${VENDOR_ROOT}/jetkvm-app}"
COMET_APP_SRC="${COMET_APP_SRC:-${VENDOR_ROOT}/comet-app}"
NANOKVM_APP_SRC="${NANOKVM_APP_SRC:-${VENDOR_ROOT}/nanokvm-app}"
ROCKCHIP_MPP_SRC="${ROCKCHIP_MPP_SRC:-${VENDOR_ROOT}/mpp-mirror}"
ROCKCHIP_RGA_UPSTREAM_SRC="${ROCKCHIP_RGA_UPSTREAM_SRC:-${VENDOR_ROOT}/librga-upstream}"
ROCKCHIP_RGA_MIRROR_SRC="${ROCKCHIP_RGA_MIRROR_SRC:-${VENDOR_ROOT}/librga-mirror}"
ROCKCHIP_RKNN_TOOLKIT2_SRC="${ROCKCHIP_RKNN_TOOLKIT2_SRC:-${VENDOR_ROOT}/rknn-toolkit2}"
ROCKCHIP_RKNN_MODEL_ZOO_SRC="${ROCKCHIP_RKNN_MODEL_ZOO_SRC:-${VENDOR_ROOT}/rknn-model-zoo}"
ROCKCHIP_RKNN_TOOLKIT_LEGACY_SRC="${ROCKCHIP_RKNN_TOOLKIT_LEGACY_SRC:-${VENDOR_ROOT}/rknn-toolkit-legacy}"
ROCKCHIP_RKNPU_CURRENT_SRC="${ROCKCHIP_RKNPU_CURRENT_SRC:-${VENDOR_ROOT}/rknpu-current}"
ROCKCHIP_RKNPU_LEGACY_SRC="${ROCKCHIP_RKNPU_LEGACY_SRC:-${VENDOR_ROOT}/rknpu-legacy}"
OPENH264_SRC="${OPENH264_SRC:-${VENDOR_ROOT}/openh264}"

log() {
  echo "[vendor-setup] $*"
}

ensure_dir() {
  local path="$1"
  if [ -z "${path}" ]; then
    return 0
  fi
  mkdir -p "${path}"
}

ensure_safe_directory() {
  local path="$1"
  if [ -z "${path}" ]; then
    return 0
  fi
  if ! git config --global --get-all safe.directory | grep -Fxq "${path}"; then
    git config --global --add safe.directory "${path}" || true
  fi
}

prepare_vendor_tree() {
  local path="$1"
  local repo_url="$2"

  if [ -z "${path}" ]; then
    return 0
  fi

  ensure_safe_directory "${path}"

  if [ -d "${path}/.git" ]; then
    log "updating ${path}"
    git -C "${path}" fetch --all --prune
    return 0
  fi

  if [ -d "${path}" ]; then
    log "using existing non-git vendor tree: ${path}"
    return 0
  fi

  if [ -n "${repo_url}" ]; then
    log "cloning ${repo_url} -> ${path}"
    ensure_dir "$(dirname "${path}")"
    if [ "${FETCH_DEPTH}" = "0" ]; then
      git clone "${repo_url}" "${path}"
    else
      git clone --depth "${FETCH_DEPTH}" "${repo_url}" "${path}"
    fi
    ensure_safe_directory "${path}"
    return 0
  fi

  log "vendor tree missing and no repo URL provided: ${path}"
}

sync_vendor_submodules() {
  local path="$1"

  if [ ! -d "${path}/.git" ] || [ ! -f "${path}/.gitmodules" ]; then
    return 0
  fi

  log "syncing submodules in ${path}"
  git -C "${path}" submodule sync --recursive
  git -C "${path}" submodule update --init --recursive --jobs "${SUBMODULE_JOBS}"
}

log "vendor root: ${VENDOR_ROOT}"
ensure_dir "${VENDOR_ROOT}"

ensure_safe_directory "${VENDOR_ROOT}"
ensure_safe_directory "${JETKVM_KERNEL_SRC}"
ensure_safe_directory "${COMET_KERNEL_SRC}"
ensure_safe_directory "${NANOKVM_SDK_SRC}"
ensure_safe_directory "${JETKVM_APP_SRC}"
ensure_safe_directory "${COMET_APP_SRC}"
ensure_safe_directory "${NANOKVM_APP_SRC}"
ensure_safe_directory "${ROCKCHIP_MPP_SRC}"
ensure_safe_directory "${ROCKCHIP_RGA_UPSTREAM_SRC}"
ensure_safe_directory "${ROCKCHIP_RGA_MIRROR_SRC}"
ensure_safe_directory "${ROCKCHIP_RKNN_TOOLKIT2_SRC}"
ensure_safe_directory "${ROCKCHIP_RKNN_MODEL_ZOO_SRC}"
ensure_safe_directory "${ROCKCHIP_RKNN_TOOLKIT_LEGACY_SRC}"
ensure_safe_directory "${ROCKCHIP_RKNPU_CURRENT_SRC}"
ensure_safe_directory "${ROCKCHIP_RKNPU_LEGACY_SRC}"
ensure_safe_directory "${OPENH264_SRC}"

# Platform trees
prepare_vendor_tree "${JETKVM_KERNEL_SRC}" "${JETKVM_KERNEL_REPO}"
prepare_vendor_tree "${COMET_KERNEL_SRC}" "${COMET_KERNEL_REPO}"
prepare_vendor_tree "${NANOKVM_SDK_SRC}" "${NANOKVM_SDK_REPO}"

# Application trees
prepare_vendor_tree "${JETKVM_APP_SRC}" "${JETKVM_APP_REPO}"
prepare_vendor_tree "${COMET_APP_SRC}" "${COMET_APP_REPO}"
prepare_vendor_tree "${NANOKVM_APP_SRC}" "${NANOKVM_APP_REPO}"

# Rockchip and media dependencies
prepare_vendor_tree "${ROCKCHIP_MPP_SRC}" "${ROCKCHIP_MPP_REPO}"
prepare_vendor_tree "${ROCKCHIP_RGA_UPSTREAM_SRC}" "${ROCKCHIP_RGA_UPSTREAM_REPO}"
prepare_vendor_tree "${ROCKCHIP_RGA_MIRROR_SRC}" "${ROCKCHIP_RGA_MIRROR_REPO}"
prepare_vendor_tree "${ROCKCHIP_RKNN_TOOLKIT2_SRC}" "${ROCKCHIP_RKNN_TOOLKIT2_REPO}"
prepare_vendor_tree "${ROCKCHIP_RKNN_MODEL_ZOO_SRC}" "${ROCKCHIP_RKNN_MODEL_ZOO_REPO}"
prepare_vendor_tree "${ROCKCHIP_RKNN_TOOLKIT_LEGACY_SRC}" "${ROCKCHIP_RKNN_TOOLKIT_LEGACY_REPO}"
prepare_vendor_tree "${ROCKCHIP_RKNPU_CURRENT_SRC}" "${ROCKCHIP_RKNPU_CURRENT_REPO}"
prepare_vendor_tree "${ROCKCHIP_RKNPU_LEGACY_SRC}" "${ROCKCHIP_RKNPU_LEGACY_REPO}"
prepare_vendor_tree "${OPENH264_SRC}" "${OPENH264_REPO}"

# Submodule sync (critical for NanoKVM SDK and some app trees)
sync_vendor_submodules "${NANOKVM_SDK_SRC}"
sync_vendor_submodules "${JETKVM_KERNEL_SRC}"
sync_vendor_submodules "${JETKVM_APP_SRC}"
sync_vendor_submodules "${COMET_APP_SRC}"
sync_vendor_submodules "${NANOKVM_APP_SRC}"

mkdir -p "${ROOT_DIR}/.cache/kmod"

if [ ! -d "/opt/jetkvm-native-buildkit" ]; then
  log "JetKVM buildkit is not present. Install it with ./.devcontainer/install-deps.sh before native builds."
else
  log "JetKVM buildkit found at /opt/jetkvm-native-buildkit"
fi

if [ ! -d "/opt/jetkvm-audio-libs" ]; then
  log "Audio deps not found; running ./devcontainer/install_audio_deps.sh"
  bash "${ROOT_DIR}/.devcontainer/install_audio_deps.sh"
fi

if [ ! -d "/opt/jetkvm-ssl-libs" ]; then
  log "SSL deps not found; running ./devcontainer/install_ssl_deps.sh"
  bash "${ROOT_DIR}/.devcontainer/install_ssl_deps.sh"
fi

