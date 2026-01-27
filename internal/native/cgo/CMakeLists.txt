cmake_minimum_required(VERSION 3.14)
include(FetchContent)
include(ExternalProject)

project(jknative LANGUAGES C CXX)

set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

# Rockchip SDK paths
# The buildkit puts files in the sysroot for cross-compilation
set(RK_SDK_BASE "/opt/jetkvm-native-buildkit")
set(RK_ARCH "arm-rockchip830-linux-uclibcgnueabihf")
set(RK_SYSROOT "${RK_SDK_BASE}/${RK_ARCH}/sysroot")
set(RK_MEDIA_INCLUDE_PATH "${RK_SYSROOT}/usr/include")
set(RK_APP_MEDIA_LIBS_PATH "${RK_SYSROOT}/usr/lib")

# RGA (2D Graphics Acceleration) - Fetch and build from source
# This provides hardware-accelerated YUV to RGB conversion
option(ENABLE_RGA "Enable RGA hardware acceleration" ON)

if(ENABLE_RGA)
    message(STATUS "Fetching librga from GitHub...")
    FetchContent_Declare(
        librga
        GIT_REPOSITORY https://github.com/tsukumijima/librga-rockchip.git
        GIT_TAG master
        GIT_SHALLOW 1
        UPDATE_DISCONNECTED 1
    )
    FetchContent_GetProperties(librga)
    if(NOT librga_POPULATED)
        FetchContent_Populate(librga)
    endif()

    # Check if librga was fetched successfully
    if(EXISTS "${librga_SOURCE_DIR}/im2d_api/im2d.h")
        set(RGA_FOUND TRUE)
        set(LIBRGA_SOURCE_DIR "${librga_SOURCE_DIR}")
        message(STATUS "RGA: librga fetched to ${LIBRGA_SOURCE_DIR}")

        # Collect librga source files for static linking
        # Core sources (excluding Android-specific files)
        file(GLOB LIBRGA_CORE_SOURCES
            "${LIBRGA_SOURCE_DIR}/core/*.cpp"
            "${LIBRGA_SOURCE_DIR}/core/utils/*.cpp"
            "${LIBRGA_SOURCE_DIR}/core/utils/drm_utils/src/*.cpp"
        )
        # Exclude Android-specific files
        list(FILTER LIBRGA_CORE_SOURCES EXCLUDE REGEX "platform_gralloc.*\\.cpp$")
        list(FILTER LIBRGA_CORE_SOURCES EXCLUDE REGEX "rga_gralloc\\.cpp$")

        # IM2D API sources
        file(GLOB LIBRGA_IM2D_SOURCES
            "${LIBRGA_SOURCE_DIR}/im2d_api/src/*.cpp"
        )

        # Create librga static library
        add_library(rga_static STATIC
            ${LIBRGA_CORE_SOURCES}
            ${LIBRGA_IM2D_SOURCES}
        )

        target_include_directories(rga_static PUBLIC
            "${LIBRGA_SOURCE_DIR}"
            "${LIBRGA_SOURCE_DIR}/include"
            "${LIBRGA_SOURCE_DIR}/im2d_api"
            "${LIBRGA_SOURCE_DIR}/im2d_api/src"
            "${LIBRGA_SOURCE_DIR}/core"
            "${LIBRGA_SOURCE_DIR}/core/hardware"
            "${LIBRGA_SOURCE_DIR}/core/utils"
            "${LIBRGA_SOURCE_DIR}/core/utils/drm_utils"
            "${LIBRGA_SOURCE_DIR}/core/3rdparty/libdrm/include"
            "${LIBRGA_SOURCE_DIR}/core/3rdparty/libdrm/include/drm"
            "${RK_MEDIA_INCLUDE_PATH}"
            "${RK_MEDIA_INCLUDE_PATH}/libdrm"
        )

        target_compile_definitions(rga_static PRIVATE
            LINUX=1
            __gnu_linux__=1
        )

        target_compile_options(rga_static PRIVATE
            -fPIC
            -fpermissive
            -Wno-unused-parameter
            -Wno-unused-function
            -Wno-pointer-arith
            -Wno-int-to-pointer-cast
            -Wno-pointer-to-int-cast
        )

        set(RK_RGA_INCLUDE_PATHS
            "${LIBRGA_SOURCE_DIR}/im2d_api"
            "${LIBRGA_SOURCE_DIR}/include"
        )

        message(STATUS "RGA hardware acceleration enabled (building from source)")
    else()
        set(RGA_FOUND FALSE)
        message(WARNING "RGA: Failed to fetch librga - hardware YUV conversion disabled")
    endif()
else()
    set(RGA_FOUND FALSE)
    message(STATUS "RGA: Disabled by ENABLE_RGA=OFF")
endif()

set(LV_USE_KCONFIG ON CACHE BOOL "" FORCE)
set(LV_BUILD_DEFCONFIG_PATH ${CMAKE_CURRENT_SOURCE_DIR}/lvgl_defconfig CACHE PATH "" FORCE)

# OpenH264 - H.264 software decoder for camera transcoding (BETA feature)
# Required for H.264→MJPEG transcoding when RDP client only sends H.264
# Uses Cisco's BSD-licensed OpenH264 with ARM NEON optimizations
option(ENABLE_OPENH264 "Enable OpenH264 software decoder for camera transcoding" ON)

if(ENABLE_OPENH264)
    message(STATUS "Fetching Cisco OpenH264 from GitHub...")
    FetchContent_Declare(
        openh264
        GIT_REPOSITORY https://github.com/cisco/openh264.git
        GIT_TAG v2.5.0
        GIT_SHALLOW 1
        UPDATE_DISCONNECTED 1
    )
    FetchContent_GetProperties(openh264)
    if(NOT openh264_POPULATED)
        FetchContent_Populate(openh264)
    endif()

    # Check if OpenH264 was fetched successfully
    if(EXISTS "${openh264_SOURCE_DIR}/codec/api/wels/codec_api.h")
        set(OPENH264_FOUND TRUE)
        set(OPENH264_SOURCE_DIR "${openh264_SOURCE_DIR}")
        message(STATUS "OpenH264: fetched to ${OPENH264_SOURCE_DIR}")

        # Collect OpenH264 decoder source files (decoder only, no encoder needed)
        file(GLOB_RECURSE OPENH264_DECODER_SOURCES
            "${OPENH264_SOURCE_DIR}/codec/decoder/core/src/*.cpp"
            "${OPENH264_SOURCE_DIR}/codec/decoder/plus/src/*.cpp"
        )

        # Common sources needed by decoder (C++ only, exclude arch-specific)
        file(GLOB OPENH264_COMMON_SOURCES
            "${OPENH264_SOURCE_DIR}/codec/common/src/*.cpp"
        )
        # Exclude arch-specific .c files (loongarch, x86, etc)
        # Only include ARM-specific assembly for our target

        # ARM assembly sources for NEON optimizations (32-bit ARM)
        # Both common and decoder-specific NEON functions
        file(GLOB OPENH264_ARM_ASM_SOURCES
            "${OPENH264_SOURCE_DIR}/codec/common/arm/*.S"
            "${OPENH264_SOURCE_DIR}/codec/decoder/core/arm/*.S"
        )

        # Create OpenH264 decoder static library
        add_library(openh264_static STATIC
            ${OPENH264_DECODER_SOURCES}
            ${OPENH264_COMMON_SOURCES}
            ${OPENH264_ARM_ASM_SOURCES}
        )

        target_include_directories(openh264_static PUBLIC
            "${OPENH264_SOURCE_DIR}/codec/api/wels"
            "${OPENH264_SOURCE_DIR}/codec/decoder/core/inc"
            "${OPENH264_SOURCE_DIR}/codec/decoder/plus/inc"
            "${OPENH264_SOURCE_DIR}/codec/common/inc"
            "${OPENH264_SOURCE_DIR}/codec/common/arm"
            "${OPENH264_SOURCE_DIR}/codec/decoder/core/arm"
        )

        # ARM32 NEON optimizations for Cortex-A7
        target_compile_options(openh264_static PRIVATE
            -fPIC
            -O3
            -mfpu=neon-vfpv4
            -mfloat-abi=hard
            -Wno-unused-parameter
            -Wno-unused-function
            -Wno-sign-compare
            -Wno-narrowing
        )

        target_compile_definitions(openh264_static PRIVATE
            HAVE_NEON=1
            __ARM_NEON__=1
        )

        # Enable assembly for ARM
        enable_language(ASM)
        set_source_files_properties(${OPENH264_ARM_ASM_SOURCES} PROPERTIES
            COMPILE_FLAGS "-mfpu=neon-vfpv4 -mfloat-abi=hard"
        )

        set(OPENH264_INCLUDE_PATH "${OPENH264_SOURCE_DIR}/codec/api/wels")
        message(STATUS "OpenH264: Cisco decoder enabled with ARM NEON (building from source)")
    else()
        set(OPENH264_FOUND FALSE)
        message(WARNING "OpenH264: Failed to fetch - camera transcoding will be disabled")
    endif()
else()
    set(OPENH264_FOUND FALSE)
    message(STATUS "OpenH264: Disabled by ENABLE_OPENH264=OFF")
endif()

# # libgpiod

# ExternalProject_Add(libgpiod-project
#     URL https://mirrors.edge.kernel.org/pub/software/libs/libgpiod/libgpiod-2.2.tar.gz
#     URL_HASH SHA256=f89c2176250f1a9563265479eb8ad5f22a63f42db6a1f438effc570f0254d2f5
#     SOURCE_DIR ${CMAKE_CURRENT_SOURCE_DIR}/deps/libgpiod
#     BINARY_DIR ${CMAKE_CURRENT_BINARY_DIR}/deps/libgpiod
#     CONFIGURE_COMMAND ${CMAKE_COMMAND} -E env CPPFLAGS=-fPIC ${CMAKE_CURRENT_SOURCE_DIR}/deps/libgpiod/configure --enable-tools=no CC=${CMAKE_C_COMPILER} --host=${CMAKE_HOST_SYSTEM_PROCESSOR}
#     BUILD_COMMAND make && make install
#     BUILD_BYPRODUCTS ${CMAKE_CURRENT_BINARY_DIR}/deps/libgpiod/lib/libgpiod.a
# )


# Fetch LVGL from GitHub
FetchContent_Declare(
    lvgl
    GIT_REPOSITORY https://github.com/lvgl/lvgl.git
    GIT_TAG v9.3.0
    GIT_SHALLOW 1
    UPDATE_DISCONNECTED 1
    PATCH_COMMAND patch -p1 -f < ${CMAKE_CURRENT_SOURCE_DIR}/lvgl-minify.patch && cat ${CMAKE_CURRENT_SOURCE_DIR}/lvgl-minify.del | xargs rm -v
    )
FetchContent_MakeAvailable(lvgl)

# Get source files, excluding CMake generated files
file(GLOB_RECURSE sources CONFIGURE_DEPENDS "*.c" "ui/*.c")
list(FILTER sources EXCLUDE REGEX "CMakeFiles.*CompilerId.*\\.c$")
# Exclude main.c from library sources (it's used for the binary target)
list(FILTER sources EXCLUDE REGEX "main\\.c$")
# Exclude FetchContent downloaded sources (OpenH264, librga, lvgl)
# These have their own static library targets with explicit source lists
list(FILTER sources EXCLUDE REGEX "_deps/")

add_library(jknative STATIC ${sources} ${CMAKE_CURRENT_SOURCE_DIR}/ctrl.h)

# Include directories
target_include_directories(jknative PRIVATE
    ${RK_MEDIA_INCLUDE_PATH}
    ${RK_MEDIA_INCLUDE_PATH}/libdrm
    ${CMAKE_CURRENT_SOURCE_DIR}
    ${CMAKE_CURRENT_SOURCE_DIR}/ui
    ${CMAKE_CURRENT_BINARY_DIR}/deps/libgpiod/include
)

# Add RGA include path and define if available
if(RGA_FOUND)
    target_include_directories(jknative PRIVATE ${RK_RGA_INCLUDE_PATHS})
    target_compile_definitions(jknative PRIVATE HAS_RGA=1)
endif()

# Add OpenH264 include path and define if available
if(OPENH264_FOUND)
    target_include_directories(jknative PRIVATE ${OPENH264_INCLUDE_PATH})
    target_compile_definitions(jknative PRIVATE HAS_OPENH264=1)
endif()

# Set library search path
target_link_directories(jknative PRIVATE ${RK_APP_MEDIA_LIBS_PATH})
# target_link_directories(jknative PRIVATE ${CMAKE_CURRENT_BINARY_DIR}/deps/libgpiod/lib)

target_link_libraries(jknative PRIVATE
    lvgl::lvgl
    pthread
    rockit
    rockchip_mpp
    m
    # libgpiod
)

# Link RGA library if available
if(RGA_FOUND)
    target_link_libraries(jknative PRIVATE rga_static)
endif()

# Link OpenH264 library if available
if(OPENH264_FOUND)
    target_link_libraries(jknative PRIVATE openh264_static)
endif()

# Binary target using main.c as entry point (optional - for testing only)
# Note: When RGA is enabled, jknative-bin may fail to link due to Android HAL
# references in librga that are not available. This is fine since Go uses
# the static library directly, not jknative-bin.
option(BUILD_JKNATIVE_BIN "Build standalone jknative-bin executable" OFF)
if(BUILD_JKNATIVE_BIN)
    add_executable(jknative-bin ${CMAKE_CURRENT_SOURCE_DIR}/main.c)
    target_link_libraries(jknative-bin PRIVATE
        jknative
        pthread
    )
    install(TARGETS jknative-bin DESTINATION bin)
endif()

# Install all static libraries to lib/ for CGO linking
install(TARGETS jknative DESTINATION lib)

if(RGA_FOUND)
    install(TARGETS rga_static DESTINATION lib)
endif()

if(OPENH264_FOUND)
    install(TARGETS openh264_static DESTINATION lib)
endif()