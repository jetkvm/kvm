#include <time.h>
#include <sys/time.h>
#include <stdio.h>
#include <unistd.h>

#include "log.h"
#include "screen.h"
#include <lvgl.h>
// #include "st7789/lcd.h"
#include "ui/ui.h"
#include "ui_index.h"

#define DISP_BUF_SIZE (300 * 240 * 2)
static lv_color_t buf[DISP_BUF_SIZE];

indev_handler_t *indev_handler = NULL;

void lvgl_set_indev_handler(indev_handler_t *handler) {
    indev_handler = handler;
}

void handle_indev_event(lv_event_t *e) {
    if (indev_handler == NULL) {
        return;
    }
    indev_handler(lv_event_get_code(e));
}

void lvgl_init(u_int16_t rotation) {
    log_trace("initalizing lvgl");

    /*LittlevGL init*/
    lv_init();

    /*Linux frame buffer device init*/

    /*Linux frame buffer device init*/
    lv_display_t *disp = lv_linux_fbdev_create();
    // lv_display_set_physical_resolution(disp, 240, 300);
    lv_display_set_resolution(disp, 240, 300);
    lv_linux_fbdev_set_file(disp, "/dev/fb0");

    lvgl_set_rotation(disp, rotation);

    // lv_display_t *disp = lv_st7789_create(LCD_H_RES, LCD_V_RES, LV_LCD_FLAG_NONE, lcd_send_cmd, lcd_send_color);
    // lv_display_set_resolution(disp, 240, 300);
    // lv_display_set_rotation(disp, LV_DISP_ROTATION_270);

    // lv_color_t * buf1 = NULL;
    // lv_color_t * buf2 = NULL;

    // uint32_t buf_size = LCD_H_RES * LCD_V_RES / 10 * lv_color_format_get_size(lv_display_get_color_format(disp));

    // buf1 = lv_malloc(buf_size);
    // if(buf1 == NULL) {
    //     log_error("display draw buffer malloc failed");
    //     return;
    // }

    // buf2 = lv_malloc(buf_size);
    // if(buf2 == NULL) {
    //     log_error("display buffer malloc failed");
    //     lv_free(buf1);
    //     return;
    // }
    // lv_display_set_buffers(disp, buf1, buf2, buf_size, LV_DISPLAY_RENDER_MODE_PARTIAL);

    /* Linux input device init */
    lv_indev_t *mouse = lv_evdev_create(LV_INDEV_TYPE_POINTER, "/dev/input/event1");
    lv_indev_set_group(mouse, lv_group_get_default());
    lv_indev_set_display(mouse, disp);

    lv_indev_add_event_cb(mouse, handle_indev_event, LV_EVENT_ALL, NULL);

    log_trace("initalizing ui");

    ui_init();
    
    log_info("ui initalized");
    // lv_label_set_text(ui_Boot_Screen_Version, "");
    // lv_label_set_text(ui_Home_Content_Ip, "...");
    // lv_label_set_text(ui_Home_Header_Cloud_Status_Label, "0 active");
}

void lvgl_tick(void) {
    lv_timer_handler();
    ui_tick();
}

void lvgl_set_rotation(lv_display_t *disp, u_int16_t rotation) {
    log_info("setting rotation to %d", rotation);
    if (rotation == 0) {
        lv_display_set_rotation(disp, LV_DISP_ROTATION_0);
    } else if (rotation == 90) {
        lv_display_set_rotation(disp, LV_DISP_ROTATION_90);
    } else if (rotation == 180) {
        lv_display_set_rotation(disp, LV_DISP_ROTATION_180);
    } else if (rotation == 270) {
        lv_display_set_rotation(disp, LV_DISP_ROTATION_270);
    } else {
        log_error("invalid rotation %d", rotation);
    }

    lv_style_t *flex_screen_style = ui_get_style("flex_screen");
    if (flex_screen_style == NULL) {
        log_error("flex_screen style not found");
        return;
    }

    lv_style_t *flex_screen_menu_style = ui_get_style("flex_screen_menu");
    if (flex_screen_menu_style == NULL) {
        log_error("flex_screen_menu style not found");
        return;
    }

    if (rotation == 90) {
        lv_style_set_pad_left(flex_screen_style, 24);
        lv_style_set_pad_right(flex_screen_style, 44);
    } else if (rotation == 270) {
        lv_style_set_pad_left(flex_screen_style, 44);
        lv_style_set_pad_right(flex_screen_style, 24);
    }

    log_info("refreshing objects");
    lv_obj_report_style_change(&flex_screen_style);
    lv_obj_report_style_change(&flex_screen_menu_style);
}

uint32_t custom_tick_get(void)
{
    static uint64_t start_ms = 0;
    if(start_ms == 0) {
        struct timeval tv_start;
        gettimeofday(&tv_start, NULL);
        start_ms = (tv_start.tv_sec * 1000000 + tv_start.tv_usec) / 1000;
    }

    struct timeval tv_now;
    gettimeofday(&tv_now, NULL);
    uint64_t now_ms;
    now_ms = (tv_now.tv_sec * 1000000 + tv_now.tv_usec) / 1000;

    uint32_t time_ms = now_ms - start_ms;
    return time_ms;
}

lv_obj_t *ui_get_obj(const char *name) {
    for (size_t i = 0; i < ui_objects_size; i++) {
        if (strcmp(ui_objects[i].name, name) == 0) {
            return *ui_objects[i].obj;
        }
    }
    return NULL;
}

lv_style_t *ui_get_style(const char *name) {
    for (size_t i = 0; i < ui_styles_size; i++) {
        if (strcmp(ui_styles[i].name, name) == 0) {
            return ui_styles[i].getter();
        }
    }
    return NULL;
}


const char *ui_get_current_screen() {
    lv_obj_t *scr = lv_scr_act();
    if (scr == NULL) {
        return NULL;
    }
    for (size_t i = 0; i < ui_objects_size; i++) {
        if (*(ui_objects[i].obj) == scr) {
            return ui_objects[i].name;
        }
    }
    return NULL;
}

lv_img_dsc_t *ui_get_image(const char *name) {
    for (size_t i = 0; i < ui_images_size; i++) {
        if (strcmp(ui_images[i].name, name) == 0) {
            return ui_images[i].img;
        }
    }
    return NULL;
}

void ui_set_text(const char *name, const char *text) {
    lv_obj_t *obj = ui_get_obj(name);
    if(obj == NULL) {
        log_error("ui_set_text %s %s, obj not found\n", name, text);
        return;
    }
    lv_label_set_text(obj, text);
}
