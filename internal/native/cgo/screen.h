#ifndef SCREEN_H
#define SCREEN_H

#include <lvgl.h>

typedef void (indev_handler_t)(lv_event_code_t code);

void lvgl_set_indev_handler(indev_handler_t *handler);

void lvgl_init(u_int16_t rotation);
void lvgl_tick(void);

void lvgl_set_rotation(lv_display_t *disp, u_int16_t rotation);

void ui_set_text(const char *name, const char *text);

lv_obj_t *ui_get_obj(const char *name);
lv_style_t *ui_get_style(const char *name);
lv_img_dsc_t *ui_get_image(const char *name);

#endif // SCREEN_H
