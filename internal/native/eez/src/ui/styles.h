#ifndef EEZ_LVGL_UI_STYLES_H
#define EEZ_LVGL_UI_STYLES_H

#include <lvgl.h>

#ifdef __cplusplus
extern "C" {
#endif

// Style: FlexColumnSpaceBetween
lv_style_t *get_style_flex_column_space_between_MAIN_DEFAULT();
void add_style_flex_column_space_between(lv_obj_t *obj);
void remove_style_flex_column_space_between(lv_obj_t *obj);

// Style: FlexCenter
lv_style_t *get_style_flex_center_MAIN_DEFAULT();
void add_style_flex_center(lv_obj_t *obj);
void remove_style_flex_center(lv_obj_t *obj);

// Style: FlexStart
lv_style_t *get_style_flex_start_MAIN_DEFAULT();
void add_style_flex_start(lv_obj_t *obj);
void remove_style_flex_start(lv_obj_t *obj);

// Style: FlowRowSpaceBetween
lv_style_t *get_style_flow_row_space_between_MAIN_DEFAULT();
void add_style_flow_row_space_between(lv_obj_t *obj);
void remove_style_flow_row_space_between(lv_obj_t *obj);

// Style: FlowRowStartCenter
lv_style_t *get_style_flow_row_start_center_MAIN_DEFAULT();
void add_style_flow_row_start_center(lv_obj_t *obj);
void remove_style_flow_row_start_center(lv_obj_t *obj);

// Style: FlexColumnStart
lv_style_t *get_style_flex_column_start_MAIN_DEFAULT();
void add_style_flex_column_start(lv_obj_t *obj);
void remove_style_flex_column_start(lv_obj_t *obj);

// Style: FlexScreen
lv_style_t *get_style_flex_screen_MAIN_DEFAULT();
void add_style_flex_screen(lv_obj_t *obj);
void remove_style_flex_screen(lv_obj_t *obj);

// Style: FlexScreenMenu
lv_style_t *get_style_flex_screen_menu_MAIN_DEFAULT();
void add_style_flex_screen_menu(lv_obj_t *obj);
void remove_style_flex_screen_menu(lv_obj_t *obj);

// Style: LabelFont16
lv_style_t *get_style_label_font16_MAIN_DEFAULT();
void add_style_label_font16(lv_obj_t *obj);
void remove_style_label_font16(lv_obj_t *obj);

// Style: LabelFontBold30
lv_style_t *get_style_label_font_bold30_MAIN_DEFAULT();
void add_style_label_font_bold30(lv_obj_t *obj);
void remove_style_label_font_bold30(lv_obj_t *obj);

// Style: HeaderLink
lv_style_t *get_style_header_link_MAIN_DEFAULT();
void add_style_header_link(lv_obj_t *obj);
void remove_style_header_link(lv_obj_t *obj);

// Style: MenuButton
lv_style_t *get_style_menu_button_MAIN_DEFAULT();
void add_style_menu_button(lv_obj_t *obj);
void remove_style_menu_button(lv_obj_t *obj);

// Style: MenuButtonLabel
lv_style_t *get_style_menu_button_label_MAIN_DEFAULT();
void add_style_menu_button_label(lv_obj_t *obj);
void remove_style_menu_button_label(lv_obj_t *obj);

// Style: InfoHeadlineLabel
lv_style_t *get_style_info_headline_label_MAIN_DEFAULT();
void add_style_info_headline_label(lv_obj_t *obj);
void remove_style_info_headline_label(lv_obj_t *obj);

// Style: InfoContentLabel
lv_style_t *get_style_info_content_label_MAIN_DEFAULT();
void add_style_info_content_label(lv_obj_t *obj);
void remove_style_info_content_label(lv_obj_t *obj);

// Style: BackButton
lv_style_t *get_style_back_button_MAIN_DEFAULT();
void add_style_back_button(lv_obj_t *obj);
void remove_style_back_button(lv_obj_t *obj);



#ifdef __cplusplus
}
#endif

#endif /*EEZ_LVGL_UI_STYLES_H*/