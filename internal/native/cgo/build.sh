#!/bin/bash
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

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
BUILD_DIR=${SCRIPT_DIR}/build

CMAKE_TOOLCHAIN_FILE=/opt/jetkvm-native-buildkit/rv1106-jetkvm-v2.cmake
CLEAN_ALL=${CLEAN_ALL:-0}

if [ "$CLEAN_ALL" -eq 1 ]; then
    rm -rf "${BUILD_DIR}"
fi

TMP_DIR=$(mktemp -d)
pushd "${SCRIPT_DIR}" > /dev/null

msg_info "▶ Generating UI index"
./ui_index.gen.sh

msg_info "▶ Building native library"
VERBOSE=1 cmake -B "${BUILD_DIR}" \
    -DCMAKE_SYSTEM_PROCESSOR=armv7l \
    -DCMAKE_SYSTEM_NAME=Linux \
    -DCMAKE_CROSSCOMPILING=1 \
    -DCMAKE_TOOLCHAIN_FILE=$CMAKE_TOOLCHAIN_FILE \
    -DLV_BUILD_USE_KCONFIG=ON \
    -DLV_BUILD_DEFCONFIG_PATH=${SCRIPT_DIR}/lvgl_defconfig \
    -DCONFIG_LV_BUILD_EXAMPLES=OFF \
    -DCONFIG_LV_BUILD_DEMOS=OFF \
    -DSKIP_GLIBC_NAMES=ON \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_INSTALL_PREFIX="${TMP_DIR}"

msg_info "▶ Copying built library and header files"
cmake --build "${BUILD_DIR}" --target install
cp -r "${TMP_DIR}/include" ../
cp -r "${TMP_DIR}/lib" ../
rm -rf "${TMP_DIR}"

popd > /dev/null
