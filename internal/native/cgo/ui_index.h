#ifndef UI_INDEX_H
#define UI_INDEX_H

#include "ui/ui.h"
#include "ui/screens.h"
#include "ui/styles.h"
#include "ui/images.h"
#include "ui/vars.h"

typedef struct {
    const char *name;
    lv_obj_t **obj; // Pointer to the object pointer, as the object pointer is only populated after the ui is initialized
} ui_obj_map;

extern ui_obj_map ui_objects[];
extern const int ui_objects_size;

typedef struct {
    const char *name;
    lv_style_t *(*getter)();
} ui_style_map;

extern ui_style_map ui_styles[];
extern const int ui_styles_size;

typedef struct {
    const char *name;
    const lv_img_dsc_t *img; // Pointer to the image descriptor const
} ui_img_map;

extern ui_img_map ui_images[];
extern const int ui_images_size;

typedef struct {
    const char *name;
    const char *(*getter)();
    void (*setter)(const char *value);
} ui_var_map;

extern ui_var_map ui_vars[];
extern const int ui_vars_size;

#endif // UI_INDEX_H
