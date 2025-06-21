#include <time.h>
#include <sys/time.h>
#include <stdio.h>
#include <unistd.h>

#include "log.h"
#include "screen.h"
#include <lvgl.h>
#include <display/fbdev.h>
#include <indev/evdev.h>
#include "ui/ui.h"
#include "ui_index.h"

#define DISP_BUF_SIZE (300 * 240 * 2)
static lv_color_t buf[DISP_BUF_SIZE];
static lv_disp_draw_buf_t disp_buf;
static lv_disp_drv_t disp_drv;
static lv_indev_drv_t indev_drv;

void lvgl_init(void) {
    log_trace("initalizing lvgl");
    lv_init();

    log_trace("initalizing fbdev");
    fbdev_init();
    lv_disp_draw_buf_init(&disp_buf, buf, NULL, DISP_BUF_SIZE);
    lv_disp_drv_init(&disp_drv);
    disp_drv.draw_buf   = &disp_buf;
    disp_drv.flush_cb   = fbdev_flush;
    disp_drv.hor_res = 240;
    disp_drv.ver_res = 300;
    disp_drv.rotated = LV_DISP_ROT_270;
    disp_drv.sw_rotate = true;
    // disp_drv.full_refresh = true;

    lv_disp_drv_register(&disp_drv);

    log_trace("initalizing evdev");
    evdev_init();
    evdev_set_file("/dev/input/event1");

    lv_indev_drv_init(&indev_drv);
    indev_drv.type = LV_INDEV_TYPE_POINTER;
    indev_drv.read_cb = evdev_read;
    lv_indev_drv_register(&indev_drv);

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
