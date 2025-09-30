cmake_minimum_required(VERSION 3.14)
include(FetchContent)
include(ExternalProject)

project(jknative LANGUAGES C CXX)

set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

# Rockchip SDK paths
set(RK_SDK_BASE "/opt/jetkvm-native-buildkit")
set(RK_MEDIA_OUTPUT "${RK_SDK_BASE}/media/out")
set(RK_MEDIA_INCLUDE_PATH "${RK_MEDIA_OUTPUT}/include")
set(RK_APP_MEDIA_LIBS_PATH "${RK_MEDIA_OUTPUT}/lib")

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

add_library(jknative STATIC ${sources} ${CMAKE_CURRENT_SOURCE_DIR}/ctrl.h)

# Include directories
target_include_directories(jknative PRIVATE
    ${RK_MEDIA_INCLUDE_PATH}
    ${RK_MEDIA_INCLUDE_PATH}/libdrm
    ${CMAKE_CURRENT_SOURCE_DIR}
    ${CMAKE_CURRENT_SOURCE_DIR}/ui
    ${CMAKE_CURRENT_BINARY_DIR}/deps/libgpiod/include
)

# Set library search path
target_link_directories(jknative PRIVATE ${RK_APP_MEDIA_LIBS_PATH})
# target_link_directories(jknative PRIVATE ${CMAKE_CURRENT_BINARY_DIR}/deps/libgpiod/lib)

target_link_libraries(jknative PRIVATE
    lvgl::lvgl 
    pthread
    rockit
    rockchip_mpp
    rga
    m
    # libgpiod
)

install(TARGETS jknative DESTINATION lib)