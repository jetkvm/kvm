#include "actions.h"
#include "screens.h"
#include <stdio.h>
#include <string.h>
#include "ui.h"
#include "vars.h"

int handle_gesture_screen_switch(lv_event_t *e, lv_dir_t direction, int screenId) {
    lv_event_code_t event_code = lv_event_get_code(e);
    if (event_code != LV_EVENT_GESTURE) {
        return 0;
    }

    if (lv_indev_get_gesture_dir(lv_indev_get_act()) != direction) {
        return 0;
    }
    lv_indev_wait_release(lv_indev_get_act());
    loadScreen(screenId);
    return 1;
}

void handle_gesture_main_screen_switch(lv_event_t *e, lv_dir_t direction) {
    const char *main_screen = get_var_main_screen();
    if (strcmp(main_screen, "home_screen") == 0) {   
        loadScreen(SCREEN_ID_HOME_SCREEN);
    } else if (strcmp(main_screen, "no_network_screen") == 0) {
        loadScreen(SCREEN_ID_NO_NETWORK_SCREEN);
    }
}

void action_switch_to_menu(lv_event_t *e) {
    loadScreen(SCREEN_ID_MENU_SCREEN);
}

void action_switch_to_advanced_menu(lv_event_t *e) {
    loadScreen(SCREEN_ID_MENU_ADVANCED_SCREEN);
}

void action_switch_to_status(lv_event_t *e) {
    loadScreen(SCREEN_ID_STATUS_SCREEN);
}

void action_switch_to_about(lv_event_t *e) {
    loadScreen(SCREEN_ID_ABOUT_SCREEN);
}

void action_switch_to_reset_config(lv_event_t *e) {
    loadScreen(SCREEN_ID_RESET_CONFIG_SCREEN);
}

void action_switch_to_reboot(lv_event_t *e) {
    loadScreen(SCREEN_ID_REBOOT_SCREEN);
}

void action_menu_screen_gesture(lv_event_t * e) {
    handle_gesture_main_screen_switch(e, LV_DIR_RIGHT);
}

void action_menu_advanced_screen_gesture(lv_event_t * e) {
    handle_gesture_screen_switch(e, LV_DIR_RIGHT, SCREEN_ID_MENU_SCREEN);
}

void action_reset_config_screen_gesture(lv_event_t * e) {
    handle_gesture_screen_switch(e, LV_DIR_RIGHT, SCREEN_ID_MENU_SCREEN);
}

void action_home_screen_gesture(lv_event_t * e) {
    handle_gesture_screen_switch(e, LV_DIR_LEFT, SCREEN_ID_MENU_SCREEN);
}

void action_about_screen_gesture(lv_event_t * e) {
    handle_gesture_screen_switch(e, LV_DIR_RIGHT, SCREEN_ID_MENU_SCREEN);
}

// user_data doesn't seem to be working, so we use a global variable here
static uint32_t t_reset_config;
static uint32_t t_reboot;

static bool b_reboot = false;
static bool b_reset_config = false;

static bool b_reboot_lock = false;
static bool b_reset_config_lock = false;

const int RESET_CONFIG_HOLD_TIME = 10;
const int REBOOT_HOLD_TIME = 5;

typedef struct {
    uint32_t *start_time;
    bool *completed;
    bool *lock;
    int hold_time_seconds;
    const char *rpc_method;
    lv_obj_t *button_obj;
    lv_obj_t *spinner_obj;
    lv_obj_t *label_obj;
    const char *default_text;
} hold_action_config_t;

static void handle_hold_action(lv_event_t *e, hold_action_config_t *config) {
    lv_event_code_t event_code = lv_event_get_code(e);
    
    if (event_code == LV_EVENT_PRESSED) {
        *(config->start_time) = lv_tick_get();
    }
    else if (event_code == LV_EVENT_PRESSING) {
        int remaining_time = config->hold_time_seconds * 1000 - lv_tick_elaps(*(config->start_time));
        if (remaining_time <= 0) {
            if (*(config->lock)) {
                return;
            }
            if (config->button_obj && config->spinner_obj) {
                lv_obj_add_flag(config->button_obj, LV_OBJ_FLAG_HIDDEN);
                lv_obj_clear_flag(config->spinner_obj, LV_OBJ_FLAG_HIDDEN);
            }
            ui_call_rpc_handler(config->rpc_method, NULL);
            *(config->lock) = true;
            *(config->completed) = true;
        } else {
            *(config->completed) = false;
            char buf[100];
            int remaining_time_seconds = remaining_time / 1000;
            if (remaining_time_seconds <= 1) {
                remaining_time_seconds = 1;
            }
            sprintf(buf, "Press and hold for\n%d seconds", remaining_time_seconds);
            lv_label_set_text(config->label_obj, buf);
        }
    } else if (event_code == LV_EVENT_RELEASED) {
        if (*(config->lock)) {
            *(config->lock) = false;
        }

        if (!*(config->completed)) {
            lv_label_set_text(config->label_obj, config->default_text);
        }
    }
}

void action_reset_config(lv_event_t * e) {
    hold_action_config_t config = {
        .start_time = &t_reset_config,
        .completed = &b_reset_config,
        .lock = &b_reset_config_lock,
        .hold_time_seconds = RESET_CONFIG_HOLD_TIME,
        .rpc_method = "resetConfig",
        .button_obj = objects.reset_config_button,
        .spinner_obj = objects.reset_config_spinner,
        .label_obj = objects.reset_config_label,
        .default_text = "Press and hold for\n10 seconds"
    };
    
    handle_hold_action(e, &config);
}

void action_reboot(lv_event_t * e) {
    hold_action_config_t config = {
        .start_time = &t_reboot,
        .completed = &b_reboot,
        .lock = &b_reboot_lock,
        .hold_time_seconds = REBOOT_HOLD_TIME,
        .rpc_method = "reboot",
        .button_obj = NULL,  // No button/spinner for reboot
        .spinner_obj = NULL,
        .label_obj = objects.reboot_label,
        .default_text = "Press and hold for\n5 seconds"
    };
    
    handle_hold_action(e, &config);
}