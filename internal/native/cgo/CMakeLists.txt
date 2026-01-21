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
            -Wno-unused-parameter
            -Wno-unused-function
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

install(TARGETS jknative DESTINATION lib)