#include <string.h>
#include <stdio.h>
#include <lvgl.h>
#include "vars.h"

char app_version[100] = { 0 };
char system_version[100] = { 0 };
char lvgl_version[32] = { 0 };
char main_screen[32] = "home_screen";
char mac_address[18] = { 0 };
char ip_v4_address[22] = { 0 };
char ip_v6_address[46] = { 0 };
char hostname[262] = { 0 };

const char *get_var_ip_v4_address() {
    return ip_v4_address;
}

void set_var_ip_v4_address(const char *value) {
    strncpy(ip_v4_address, value, sizeof(ip_v4_address) / sizeof(char));
    ip_v4_address[sizeof(ip_v4_address) / sizeof(char) - 1] = 0;

    tick_screen_home_screen();
}

const char *get_var_ip_v6_address() {
    return ip_v6_address;
}

void set_var_ip_v6_address(const char *value) {
    strncpy(ip_v6_address, value, sizeof(ip_v6_address) / sizeof(char));
    ip_v6_address[sizeof(ip_v6_address) / sizeof(char) - 1] = 0;

    tick_screen_home_screen();
}

const char *get_var_mac_address() {
    return mac_address;
}

void set_var_mac_address(const char *value) {
    strncpy(mac_address, value, sizeof(mac_address) / sizeof(char));
    mac_address[sizeof(mac_address) / sizeof(char) - 1] = 0;

    tick_screen_home_screen();
    tick_screen_status_screen();
}

const char *get_var_hostname() {
    return hostname;
}

void set_var_hostname(const char *value) {
    strncpy(hostname, value, sizeof(hostname) / sizeof(char));
    hostname[sizeof(hostname) / sizeof(char) - 1] = 0;

    tick_screen_home_screen();
}

const char *get_var_app_version() {
    return app_version;
}

void set_var_app_version(const char *value) {
    strncpy(app_version, value, sizeof(app_version) / sizeof(char));
    app_version[sizeof(app_version) / sizeof(char) - 1] = 0;
    
    tick_screen_boot_screen();
    tick_screen_about_screen();
}

const char *get_var_system_version() {
    return system_version;
}

void set_var_system_version(const char *value) {
    strncpy(system_version, value, sizeof(system_version) / sizeof(char));
    system_version[sizeof(system_version) / sizeof(char) - 1] = 0;

    tick_screen_about_screen();
}

const char *get_var_lvgl_version() {
    if (lvgl_version[0] == '\0') {
        char buf[32];
        sprintf(buf, "%d.%d.%d", LVGL_VERSION_MAJOR, LVGL_VERSION_MINOR, LVGL_VERSION_PATCH);
        
        
        strncpy(lvgl_version, buf, sizeof(lvgl_version) / sizeof(char));
        app_version[sizeof(lvgl_version) / sizeof(char) - 1] = 0;
    }
    return lvgl_version;
}

void set_var_lvgl_version(const char *value) {
    // intentional NOP since this is actually generated
 
    tick_screen_about_screen();
}

const char *get_var_main_screen() {
    return main_screen;
}

void set_var_main_screen(const char *value) {
    strncpy(main_screen, value, sizeof(main_screen) / sizeof(char));
    main_screen[sizeof(main_screen) / sizeof(char) - 1] = 0;
}
