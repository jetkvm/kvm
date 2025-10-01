#!/bin/bash
set -e

SCRIPT_PATH=$(realpath "$(dirname $(realpath "${BASH_SOURCE[0]}"))")
source ${SCRIPT_PATH}/build_utils.sh

CGO_PATH=$(realpath "${SCRIPT_PATH}/../internal/native/cgo")
BUILD_DIR=${CGO_PATH}/build

CMAKE_TOOLCHAIN_FILE=/opt/jetkvm-native-buildkit/rv1106-jetkvm-v2.cmake
CLEAN_ALL=${CLEAN_ALL:-0}

if [ "$CLEAN_ALL" -eq 1 ]; then
    rm -rf "${BUILD_DIR}"
fi

TMP_DIR=$(mktemp -d)
# Ensure temp directory persists and is cleaned up properly
# Also handle SIGINT (CTRL+C) and SIGTERM - kill all child processes
trap 'pkill -P $$; rm -rf "${TMP_DIR}"; exit 1' INT TERM
pushd "${CGO_PATH}" > /dev/null

msg_info "▶ Generating UI index"
./ui_index.gen.sh

msg_info "▶ Building native library"

# Fix clock skew issues by resetting file timestamps
find "${CGO_PATH}" -type f -exec touch {} +

# Only clean CMake cache if the build configuration files don't exist
# This prevents re-running expensive compiler detection on every build
if [ ! -f "${BUILD_DIR}/CMakeCache.txt" ]; then
    msg_info "First build - CMake will configure the project"
fi

VERBOSE=1 cmake -B "${BUILD_DIR}" \
    -DCMAKE_SYSTEM_PROCESSOR=armv7l \
    -DCMAKE_SYSTEM_NAME=Linux \
    -DCMAKE_CROSSCOMPILING=1 \
    -DCMAKE_TOOLCHAIN_FILE=$CMAKE_TOOLCHAIN_FILE \
    -DCMAKE_C_COMPILER_WORKS=1 \
    -DCMAKE_CXX_COMPILER_WORKS=1 \
    -DCMAKE_C_ABI_COMPILED=1 \
    -DCMAKE_CXX_ABI_COMPILED=1 \
    -DCMAKE_TRY_COMPILE_TARGET_TYPE=STATIC_LIBRARY \
    -DLV_BUILD_USE_KCONFIG=ON \
    -DLV_BUILD_DEFCONFIG_PATH=${CGO_PATH}/lvgl_defconfig \
    -DCONFIG_LV_BUILD_EXAMPLES=OFF \
    -DCONFIG_LV_BUILD_DEMOS=OFF \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_INSTALL_PREFIX="${TMP_DIR}"

msg_info "▶ Copying built library and header files"
# Clock skew can cause make to return 1 even when build succeeds
# We verify success by checking if the output file exists
cmake --build "${BUILD_DIR}" --target install || true

if [ ! -f "${TMP_DIR}/lib/libjknative.a" ]; then
    msg_err "Build failed - libjknative.a not found"
    exit 1
fi

cp -r "${TMP_DIR}/include" "${CGO_PATH}"
cp -r "${TMP_DIR}/lib" "${CGO_PATH}"
rm -rf "${TMP_DIR}"

popd > /dev/null
