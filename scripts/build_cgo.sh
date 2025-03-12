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
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_INSTALL_PREFIX="${TMP_DIR}"

msg_info "▶ Copying built library and header files"
cmake --build "${BUILD_DIR}" --target install
cp -r "${TMP_DIR}/include" "${CGO_PATH}"
cp -r "${TMP_DIR}/lib" "${CGO_PATH}"
rm -rf "${TMP_DIR}"

popd > /dev/null
