#!/bin/bash

CMAKE_TOOLCHAIN_FILE=/opt/jetkvm-native-buildkit/rv1106-jetkvm-v2.cmake
CLEAN_ALL=${CLEAN_ALL:-0}

if [ "$CLEAN_ALL" -eq 1 ]; then
    find . -name build -exec rm -r {} +
fi

set -x
VERBOSE=1 cmake -B build \
    -DCMAKE_SYSTEM_PROCESSOR=armv7l \
    -DCMAKE_SYSTEM_NAME=Linux \
    -DCMAKE_CROSSCOMPILING=1 \
    -DCMAKE_TOOLCHAIN_FILE=$CMAKE_TOOLCHAIN_FILE \
    -DSKIP_GLIBC_NAMES=ON \
    -DCMAKE_BUILD_TYPE=Release

cmake --build build