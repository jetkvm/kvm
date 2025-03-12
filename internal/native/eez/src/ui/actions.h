#ifndef EEZ_LVGL_UI_EVENTS_H
#define EEZ_LVGL_UI_EVENTS_H

#include <lvgl.h>

#ifdef __cplusplus
extern "C" {
#endif

extern int handle_gesture_screen_switch(lv_event_t *e, lv_dir_t direction, int screenId);

extern void action_switch_to_menu(lv_event_t * e);
extern void action_switch_to_advanced_menu(lv_event_t * e);
extern void action_switch_to_reset_config(lv_event_t * e);
extern void action_switch_to_about(lv_event_t * e);
extern void action_menu_screen_gesture(lv_event_t * e);
extern void action_home_screen_gesture(lv_event_t * e);
extern void action_menu_advanced_screen_gesture(lv_event_t * e);
extern void action_reset_config_screen_gesture(lv_event_t * e);
extern void action_about_screen_gesture(lv_event_t * e);
extern void action_switch_to_status(lv_event_t * e);
extern void action_common_click_event(lv_event_t * e);
extern void action_handle_common_press_event(lv_event_t * e);
extern void action_reset_config(lv_event_t * e);
extern void action_reboot(lv_event_t * e);
extern void action_switch_to_reboot(lv_event_t * e);


#ifdef __cplusplus
}
#endif

#endif /*EEZ_LVGL_UI_EVENTS_H*/