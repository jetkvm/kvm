#ifndef SCREEN_H
#define SCREEN_H

#include <lvgl.h>

void lvgl_init(void);
void lvgl_tick(void);

void ui_set_text(const char *name, const char *text);
lv_obj_t *ui_get_obj(const char *name);
lv_img_dsc_t *ui_get_image(const char *name);

#endif // SCREEN_H
