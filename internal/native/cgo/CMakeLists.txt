cmake_minimum_required(VERSION 3.14)
include(FetchContent)

project(jknative LANGUAGES C CXX)

set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

# Specify path to own LVGL config header
set(
    LV_CONF_PATH
    ${CMAKE_CURRENT_SOURCE_DIR}/lv_conf.h
    CACHE STRING "" FORCE
)

set(
    LV_DRIVERS_PUBLIC_HEADERS
    ${CMAKE_CURRENT_SOURCE_DIR}/lv_drv_conf.h
    CACHE STRING "" FORCE
)

# Rockchip SDK paths
set(RK_SDK_BASE "/opt/jetkvm-native-buildkit")
set(RK_MEDIA_OUTPUT "${RK_SDK_BASE}/media/out")
set(RK_MEDIA_INCLUDE_PATH "${RK_MEDIA_OUTPUT}/include")
set(RK_APP_MEDIA_LIBS_PATH "${RK_MEDIA_OUTPUT}/lib")

# Fetch LVGL from GitHub
FetchContent_Declare(
    lvgl
    GIT_REPOSITORY https://github.com/lvgl/lvgl.git
    GIT_TAG v8.3.11
)
FetchContent_MakeAvailable(lvgl)

# Fetch LVGL drivers from GitHub
FetchContent_Declare(
    lv_drivers
    GIT_REPOSITORY https://github.com/lvgl/lv_drivers.git
    GIT_TAG v8.3.0
)
FetchContent_MakeAvailable(lv_drivers)

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
)

# Set library search path
target_link_directories(jknative PRIVATE ${RK_APP_MEDIA_LIBS_PATH})


target_link_libraries(jknative PRIVATE
    lvgl::lvgl 
    lvgl::drivers
    pthread
    rockit
    rockchip_mpp
    rga
    m
)