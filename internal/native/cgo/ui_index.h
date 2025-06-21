#ifndef UI_INDEX_H
#define UI_INDEX_H

#include "ui/ui.h"
#include "ui/screens.h"
#include "ui/images.h"

typedef struct {
    const char *name;
    lv_obj_t **obj; // Pointer to the object pointer, as the object pointer is only populated after the ui is initialized
} ui_obj_map;

extern ui_obj_map ui_objects[];
extern const int ui_objects_size;

typedef struct {
    const char *name;
    const lv_img_dsc_t *img; // Pointer to the image descriptor const
} ui_img_map;

extern ui_img_map ui_images[];
extern const int ui_images_size;


#endif // UI_INDEX_H
