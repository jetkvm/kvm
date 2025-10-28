#ifndef EEZ_LVGL_UI_VARS_H
#define EEZ_LVGL_UI_VARS_H

#include <stdint.h>
#include <stdbool.h>

void tick_screen_home_screen();
void tick_screen_status_screen();
void tick_screen_boot_screen();
void tick_screen_about_screen();

#ifdef __cplusplus
extern "C" {
#endif

// enum declarations



// Flow global variables

enum FlowGlobalVariables {
    FLOW_GLOBAL_VARIABLE_APP_VERSION = 0,
    FLOW_GLOBAL_VARIABLE_SYSTEM_VERSION = 1,
    FLOW_GLOBAL_VARIABLE_LVGL_VERSION = 2,
    FLOW_GLOBAL_VARIABLE_MAIN_SCREEN = 3,
    FLOW_GLOBAL_VARIABLE_MAC_ADDRESS = 4,
    FLOW_GLOBAL_VARIABLE_IP_V6_ADDRESS = 5,
    FLOW_GLOBAL_VARIABLE_IP_V4_ADDRESS = 6,
    FLOW_GLOBAL_VARIABLE_HOSTNAME = 7
};

// Native global variables

extern const char *get_var_app_version();
extern void set_var_app_version(const char *value);
extern const char *get_var_system_version();
extern void set_var_system_version(const char *value);
extern const char *get_var_lvgl_version();
extern void set_var_lvgl_version(const char *value);
extern const char *get_var_main_screen();
extern void set_var_main_screen(const char *value);
extern const char *get_var_mac_address();
extern void set_var_mac_address(const char *value);
extern const char *get_var_ip_v6_address();
extern void set_var_ip_v6_address(const char *value);
extern const char *get_var_ip_v4_address();
extern void set_var_ip_v4_address(const char *value);
extern const char *get_var_hostname();
extern void set_var_hostname(const char *value);


#ifdef __cplusplus
}
#endif

#endif /*EEZ_LVGL_UI_VARS_H*/