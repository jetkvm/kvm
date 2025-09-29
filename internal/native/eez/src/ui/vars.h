#ifndef EEZ_LVGL_UI_VARS_H
#define EEZ_LVGL_UI_VARS_H

#include <stdint.h>
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

// enum declarations



// Flow global variables

enum FlowGlobalVariables {
    FLOW_GLOBAL_VARIABLE_APP_VERSION = 0,
    FLOW_GLOBAL_VARIABLE_SYSTEM_VERSION = 1,
    FLOW_GLOBAL_VARIABLE_LVGL_VERSION = 2,
    FLOW_GLOBAL_VARIABLE_MAIN_SCREEN = 3
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


#ifdef __cplusplus
}
#endif

#endif /*EEZ_LVGL_UI_VARS_H*/