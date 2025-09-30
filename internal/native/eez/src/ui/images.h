#ifndef EEZ_LVGL_UI_IMAGES_H
#define EEZ_LVGL_UI_IMAGES_H

#include <lvgl.h>

#ifdef __cplusplus
extern "C" {
#endif

extern const lv_img_dsc_t img_logo;
extern const lv_img_dsc_t img_boot_logo_2;
extern const lv_img_dsc_t img_arrow_icon;
extern const lv_img_dsc_t img_back_caret;
extern const lv_img_dsc_t img_back_icon;
extern const lv_img_dsc_t img_check_icon;
extern const lv_img_dsc_t img_cloud_disconnected;
extern const lv_img_dsc_t img_cloud;
extern const lv_img_dsc_t img_d2;
extern const lv_img_dsc_t img_ethernet;
extern const lv_img_dsc_t img_hdmi;
extern const lv_img_dsc_t img_jetkvm;
extern const lv_img_dsc_t img_usb;
extern const lv_img_dsc_t img_x_icon;

#ifndef EXT_IMG_DESC_T
#define EXT_IMG_DESC_T
typedef struct _ext_img_desc_t {
    const char *name;
    const lv_img_dsc_t *img_dsc;
} ext_img_desc_t;
#endif

extern const ext_img_desc_t images[14];


#ifdef __cplusplus
}
#endif

#endif /*EEZ_LVGL_UI_IMAGES_H*/