#ifndef SCREEN_H
#define SCREEN_H

#include <lvgl.h>

typedef void (indev_handler_t)(lv_event_code_t code);

void lvgl_set_indev_handler(indev_handler_t *handler);

void lvgl_init(u_int16_t rotation);
void lvgl_tick(void);

void lvgl_set_rotation(lv_display_t *disp, u_int16_t rotation);

/**
 * @brief Set the text of an object
 *
 * @param name The name of the object
 * @param text The text to set
 * @return void
 */
void ui_set_text(const char *name, const char *text);

/**
 * @brief Get the object with the given name
 *
 * @param name The name of the object
 * @return lv_obj_t* The object with the given name
 */
lv_obj_t *ui_get_obj(const char *name);

/**
 * @brief Get the style with the given name
 *
 * @param name The name of the style
 * @return lv_style_t* The style with the given name
 */
lv_style_t *ui_get_style(const char *name);

/**
 * @brief Get the image with the given name
 *
 * @param name The name of the image
 * @return const lv_img_dsc_t* The image with the given name
 */
const lv_img_dsc_t *ui_get_image(const char *name);

/**
 * @brief Get the current screen name
 *
 * @return const char* The name of the current screen
 */
const char *ui_get_current_screen();

#endif // SCREEN_H
