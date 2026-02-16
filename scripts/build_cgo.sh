#!/bin/bash
set -e

SCRIPT_PATH=$(realpath "$(dirname $(realpath "${BASH_SOURCE[0]}"))")
source ${SCRIPT_PATH}/build_utils.sh

CMAKE_BUILD_TYPE=${CMAKE_BUILD_TYPE:-Release}

CGO_PATH=$(realpath "${SCRIPT_PATH}/../internal/hal/native/cgo")

# Devpod uses different container workspace roots (e.g. /workspaces/kvm vs /workspaces/kvm-local)
# for the same host-mounted repo. CMake caches absolute source/build paths, so a single shared build
# directory causes cache mismatches between workspaces. Use a per-workspace build directory.
REPO_ROOT=$(realpath "${SCRIPT_PATH}/..")
WORKSPACE_TAG=$(basename "${REPO_ROOT}")
BUILD_DIR_DEFAULT="${CGO_PATH}/build-${WORKSPACE_TAG}"
BUILD_DIR="${JK_CGO_BUILD_DIR:-${BUILD_DIR_DEFAULT}}"

CMAKE_TOOLCHAIN_FILE=/opt/jetkvm-native-buildkit/rv1106-jetkvm-v2.cmake
CLEAN_ALL=${CLEAN_ALL:-0}
BUILD_JKNATIVE_BIN=${BUILD_JKNATIVE_BIN:-OFF}

if [ "${CMAKE_BUILD_TYPE}" = "Debug" ]; then
    BUILD_JKNATIVE_BIN=ON
fi

if [ "$CLEAN_ALL" -eq 1 ]; then
	    rm -rf "${BUILD_DIR}"
fi

if [ -f "${BUILD_DIR}/CMakeCache.txt" ]; then
    # If the repo was moved/renamed, CMake will refuse to reuse the old cache.
    CACHED_SRC_DIR=$(grep -E '^CMAKE_HOME_DIRECTORY:INTERNAL=' "${BUILD_DIR}/CMakeCache.txt" | cut -d= -f2- || true)
    if [ -n "${CACHED_SRC_DIR}" ] && [ "${CACHED_SRC_DIR}" != "${CGO_PATH}" ]; then
        msg_warn "CMake cache source dir mismatch (${CACHED_SRC_DIR} != ${CGO_PATH}); cleaning ${BUILD_DIR}"
        rm -rf "${BUILD_DIR}"
    fi
fi

TMP_DIR=$(mktemp -d)
pushd "${CGO_PATH}" > /dev/null

msg_info "▶ Generating UI index"
./ui_index.gen.sh

msg_info "▶ Building native library"
VERBOSE=1 cmake -B "${BUILD_DIR}" \
    -DCMAKE_SYSTEM_PROCESSOR=armv7l \
    -DCMAKE_SYSTEM_NAME=Linux \
    -DCMAKE_CROSSCOMPILING=1 \
    -DCMAKE_TOOLCHAIN_FILE=$CMAKE_TOOLCHAIN_FILE \
    -DLV_BUILD_USE_KCONFIG=ON \
    -DLV_BUILD_DEFCONFIG_PATH=${CGO_PATH}/lvgl_defconfig \
    -DCONFIG_LV_BUILD_EXAMPLES=OFF \
    -DCONFIG_LV_BUILD_DEMOS=OFF \
	    -DSKIP_GLIBC_NAMES=ON \
	    -DCMAKE_BUILD_TYPE=${CMAKE_BUILD_TYPE} \
	    -DCMAKE_INSTALL_PREFIX="${TMP_DIR}" \
	    -DBUILD_JKNATIVE_BIN="${BUILD_JKNATIVE_BIN}"

msg_info "▶ Copying built library and header files"
cmake --build "${BUILD_DIR}" --target jknative
if [ "${BUILD_JKNATIVE_BIN}" = "ON" ]; then
    cmake --build "${BUILD_DIR}" --target jknative-bin
fi
cmake --install "${BUILD_DIR}"
cp -r "${TMP_DIR}/include" "${CGO_PATH}"
cp -r "${TMP_DIR}/lib" "${CGO_PATH}"
rm -rf "${TMP_DIR}"

popd > /dev/null
