#include "styles.h"
#include "images.h"
#include "fonts.h"

#include "ui.h"
#include "screens.h"

//
// Style: FlexColumnSpaceBetween
//

void init_style_flex_column_space_between_MAIN_DEFAULT(lv_style_t *style) {
    lv_style_set_flex_flow(style, LV_FLEX_FLOW_COLUMN);
    lv_style_set_flex_main_place(style, LV_FLEX_ALIGN_SPACE_BETWEEN);
    lv_style_set_flex_cross_place(style, LV_FLEX_ALIGN_START);
    lv_style_set_flex_track_place(style, LV_FLEX_ALIGN_START);
    lv_style_set_text_align(style, LV_TEXT_ALIGN_CENTER);
};

lv_style_t *get_style_flex_column_space_between_MAIN_DEFAULT() {
    static lv_style_t *style;
    if (!style) {
        style = lv_malloc(sizeof(lv_style_t));
        lv_style_init(style);
        init_style_flex_column_space_between_MAIN_DEFAULT(style);
    }
    return style;
};

void add_style_flex_column_space_between(lv_obj_t *obj) {
    (void)obj;
    lv_obj_add_style(obj, get_style_flex_column_space_between_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

void remove_style_flex_column_space_between(lv_obj_t *obj) {
    (void)obj;
    lv_obj_remove_style(obj, get_style_flex_column_space_between_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

//
// Style: FlexCenter
//

void init_style_flex_center_MAIN_DEFAULT(lv_style_t *style) {
    lv_style_set_layout(style, LV_LAYOUT_FLEX);
    lv_style_set_flex_flow(style, LV_FLEX_FLOW_COLUMN);
    lv_style_set_flex_main_place(style, LV_FLEX_ALIGN_CENTER);
    lv_style_set_flex_cross_place(style, LV_FLEX_ALIGN_CENTER);
    lv_style_set_flex_track_place(style, LV_FLEX_ALIGN_CENTER);
};

lv_style_t *get_style_flex_center_MAIN_DEFAULT() {
    static lv_style_t *style;
    if (!style) {
        style = lv_malloc(sizeof(lv_style_t));
        lv_style_init(style);
        init_style_flex_center_MAIN_DEFAULT(style);
    }
    return style;
};

void add_style_flex_center(lv_obj_t *obj) {
    (void)obj;
    lv_obj_add_style(obj, get_style_flex_center_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

void remove_style_flex_center(lv_obj_t *obj) {
    (void)obj;
    lv_obj_remove_style(obj, get_style_flex_center_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

//
// Style: FlexStart
//

void init_style_flex_start_MAIN_DEFAULT(lv_style_t *style) {
    init_style_flex_center_MAIN_DEFAULT(style);
    
    lv_style_set_layout(style, LV_LAYOUT_FLEX);
    lv_style_set_flex_flow(style, LV_FLEX_FLOW_COLUMN);
    lv_style_set_flex_main_place(style, LV_FLEX_ALIGN_START);
    lv_style_set_flex_cross_place(style, LV_FLEX_ALIGN_START);
    lv_style_set_flex_track_place(style, LV_FLEX_ALIGN_START);
};

lv_style_t *get_style_flex_start_MAIN_DEFAULT() {
    static lv_style_t *style;
    if (!style) {
        style = lv_malloc(sizeof(lv_style_t));
        lv_style_init(style);
        init_style_flex_start_MAIN_DEFAULT(style);
    }
    return style;
};

void add_style_flex_start(lv_obj_t *obj) {
    (void)obj;
    lv_obj_add_style(obj, get_style_flex_start_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

void remove_style_flex_start(lv_obj_t *obj) {
    (void)obj;
    lv_obj_remove_style(obj, get_style_flex_start_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

//
// Style: FlowRowSpaceBetween
//

void init_style_flow_row_space_between_MAIN_DEFAULT(lv_style_t *style) {
    init_style_flex_center_MAIN_DEFAULT(style);
    
    lv_style_set_layout(style, LV_LAYOUT_FLEX);
    lv_style_set_flex_flow(style, LV_FLEX_FLOW_ROW);
    lv_style_set_flex_main_place(style, LV_FLEX_ALIGN_SPACE_BETWEEN);
    lv_style_set_flex_cross_place(style, LV_FLEX_ALIGN_CENTER);
    lv_style_set_flex_track_place(style, LV_FLEX_ALIGN_START);
};

lv_style_t *get_style_flow_row_space_between_MAIN_DEFAULT() {
    static lv_style_t *style;
    if (!style) {
        style = lv_malloc(sizeof(lv_style_t));
        lv_style_init(style);
        init_style_flow_row_space_between_MAIN_DEFAULT(style);
    }
    return style;
};

void add_style_flow_row_space_between(lv_obj_t *obj) {
    (void)obj;
    lv_obj_add_style(obj, get_style_flow_row_space_between_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

void remove_style_flow_row_space_between(lv_obj_t *obj) {
    (void)obj;
    lv_obj_remove_style(obj, get_style_flow_row_space_between_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

//
// Style: FlowRowStartCenter
//

void init_style_flow_row_start_center_MAIN_DEFAULT(lv_style_t *style) {
    init_style_flow_row_space_between_MAIN_DEFAULT(style);
    
    lv_style_set_layout(style, LV_LAYOUT_FLEX);
    lv_style_set_flex_flow(style, LV_FLEX_FLOW_ROW);
    lv_style_set_flex_main_place(style, LV_FLEX_ALIGN_START);
    lv_style_set_flex_cross_place(style, LV_FLEX_ALIGN_CENTER);
    lv_style_set_flex_track_place(style, LV_FLEX_ALIGN_START);
};

lv_style_t *get_style_flow_row_start_center_MAIN_DEFAULT() {
    static lv_style_t *style;
    if (!style) {
        style = lv_malloc(sizeof(lv_style_t));
        lv_style_init(style);
        init_style_flow_row_start_center_MAIN_DEFAULT(style);
    }
    return style;
};

void add_style_flow_row_start_center(lv_obj_t *obj) {
    (void)obj;
    lv_obj_add_style(obj, get_style_flow_row_start_center_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

void remove_style_flow_row_start_center(lv_obj_t *obj) {
    (void)obj;
    lv_obj_remove_style(obj, get_style_flow_row_start_center_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

//
// Style: FlexColumnStart
//

void init_style_flex_column_start_MAIN_DEFAULT(lv_style_t *style) {
    init_style_flow_row_space_between_MAIN_DEFAULT(style);
    
    lv_style_set_layout(style, LV_LAYOUT_FLEX);
    lv_style_set_flex_flow(style, LV_FLEX_FLOW_COLUMN);
    lv_style_set_flex_main_place(style, LV_FLEX_ALIGN_START);
    lv_style_set_flex_cross_place(style, LV_FLEX_ALIGN_START);
    lv_style_set_flex_track_place(style, LV_FLEX_ALIGN_START);
};

lv_style_t *get_style_flex_column_start_MAIN_DEFAULT() {
    static lv_style_t *style;
    if (!style) {
        style = lv_malloc(sizeof(lv_style_t));
        lv_style_init(style);
        init_style_flex_column_start_MAIN_DEFAULT(style);
    }
    return style;
};

void add_style_flex_column_start(lv_obj_t *obj) {
    (void)obj;
    lv_obj_add_style(obj, get_style_flex_column_start_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

void remove_style_flex_column_start(lv_obj_t *obj) {
    (void)obj;
    lv_obj_remove_style(obj, get_style_flex_column_start_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

//
// Style: FlexScreen
//

void init_style_flex_screen_MAIN_DEFAULT(lv_style_t *style) {
    lv_style_set_pad_top(style, 24);
    lv_style_set_pad_bottom(style, 24);
    lv_style_set_pad_left(style, 44);
    lv_style_set_pad_right(style, 24);
    lv_style_set_pad_row(style, 16);
    lv_style_set_layout(style, LV_LAYOUT_FLEX);
    lv_style_set_flex_flow(style, LV_FLEX_FLOW_COLUMN);
    lv_style_set_flex_main_place(style, LV_FLEX_ALIGN_START);
    lv_style_set_flex_cross_place(style, LV_FLEX_ALIGN_START);
    lv_style_set_flex_track_place(style, LV_FLEX_ALIGN_START);
    lv_style_set_bg_color(style, lv_color_hex(0xff000000));
};

lv_style_t *get_style_flex_screen_MAIN_DEFAULT() {
    static lv_style_t *style;
    if (!style) {
        style = lv_malloc(sizeof(lv_style_t));
        lv_style_init(style);
        init_style_flex_screen_MAIN_DEFAULT(style);
    }
    return style;
};

void add_style_flex_screen(lv_obj_t *obj) {
    (void)obj;
    lv_obj_add_style(obj, get_style_flex_screen_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

void remove_style_flex_screen(lv_obj_t *obj) {
    (void)obj;
    lv_obj_remove_style(obj, get_style_flex_screen_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

//
// Style: FlexScreenMenu
//

void init_style_flex_screen_menu_MAIN_DEFAULT(lv_style_t *style) {
    init_style_flex_screen_MAIN_DEFAULT(style);
    
};

lv_style_t *get_style_flex_screen_menu_MAIN_DEFAULT() {
    static lv_style_t *style;
    if (!style) {
        style = lv_malloc(sizeof(lv_style_t));
        lv_style_init(style);
        init_style_flex_screen_menu_MAIN_DEFAULT(style);
    }
    return style;
};

void add_style_flex_screen_menu(lv_obj_t *obj) {
    (void)obj;
    lv_obj_add_style(obj, get_style_flex_screen_menu_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

void remove_style_flex_screen_menu(lv_obj_t *obj) {
    (void)obj;
    lv_obj_remove_style(obj, get_style_flex_screen_menu_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

//
// Style: LabelFont16
//

void init_style_label_font16_MAIN_DEFAULT(lv_style_t *style) {
    lv_style_set_text_align(style, LV_TEXT_ALIGN_CENTER);
    lv_style_set_text_font(style, &ui_font_font_book16);
    lv_style_set_text_color(style, lv_color_hex(0xffffffff));
};

lv_style_t *get_style_label_font16_MAIN_DEFAULT() {
    static lv_style_t *style;
    if (!style) {
        style = lv_malloc(sizeof(lv_style_t));
        lv_style_init(style);
        init_style_label_font16_MAIN_DEFAULT(style);
    }
    return style;
};

void add_style_label_font16(lv_obj_t *obj) {
    (void)obj;
    lv_obj_add_style(obj, get_style_label_font16_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

void remove_style_label_font16(lv_obj_t *obj) {
    (void)obj;
    lv_obj_remove_style(obj, get_style_label_font16_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

//
// Style: LabelFontBold30
//

void init_style_label_font_bold30_MAIN_DEFAULT(lv_style_t *style) {
    init_style_label_font16_MAIN_DEFAULT(style);
    
    lv_style_set_text_font(style, &ui_font_font_bold30);
};

lv_style_t *get_style_label_font_bold30_MAIN_DEFAULT() {
    static lv_style_t *style;
    if (!style) {
        style = lv_malloc(sizeof(lv_style_t));
        lv_style_init(style);
        init_style_label_font_bold30_MAIN_DEFAULT(style);
    }
    return style;
};

void add_style_label_font_bold30(lv_obj_t *obj) {
    (void)obj;
    lv_obj_add_style(obj, get_style_label_font_bold30_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

void remove_style_label_font_bold30(lv_obj_t *obj) {
    (void)obj;
    lv_obj_remove_style(obj, get_style_label_font_bold30_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

//
// Style: HeaderLink
//

void init_style_header_link_MAIN_DEFAULT(lv_style_t *style) {
    init_style_label_font16_MAIN_DEFAULT(style);
    
    lv_style_set_text_color(style, lv_color_hex(0xff1d4ed8));
    lv_style_set_text_opa(style, 255);
    lv_style_set_text_font(style, &ui_font_font_book20);
    lv_style_set_text_align(style, LV_TEXT_ALIGN_CENTER);
};

lv_style_t *get_style_header_link_MAIN_DEFAULT() {
    static lv_style_t *style;
    if (!style) {
        style = lv_malloc(sizeof(lv_style_t));
        lv_style_init(style);
        init_style_header_link_MAIN_DEFAULT(style);
    }
    return style;
};

void add_style_header_link(lv_obj_t *obj) {
    (void)obj;
    lv_obj_add_style(obj, get_style_header_link_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

void remove_style_header_link(lv_obj_t *obj) {
    (void)obj;
    lv_obj_remove_style(obj, get_style_header_link_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

//
// Style: MenuButton
//

void init_style_menu_button_MAIN_DEFAULT(lv_style_t *style) {
    lv_style_set_radius(style, 8);
    lv_style_set_bg_color(style, lv_color_hex(0xff262626));
    lv_style_set_bg_opa(style, 255);
    lv_style_set_pad_top(style, 20);
    lv_style_set_pad_bottom(style, 20);
    lv_style_set_pad_left(style, 16);
    lv_style_set_pad_right(style, 0);
    lv_style_set_shadow_width(style, 0);
};

lv_style_t *get_style_menu_button_MAIN_DEFAULT() {
    static lv_style_t *style;
    if (!style) {
        style = lv_malloc(sizeof(lv_style_t));
        lv_style_init(style);
        init_style_menu_button_MAIN_DEFAULT(style);
    }
    return style;
};

void add_style_menu_button(lv_obj_t *obj) {
    (void)obj;
    lv_obj_add_style(obj, get_style_menu_button_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

void remove_style_menu_button(lv_obj_t *obj) {
    (void)obj;
    lv_obj_remove_style(obj, get_style_menu_button_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

//
// Style: MenuButtonLabel
//

void init_style_menu_button_label_MAIN_DEFAULT(lv_style_t *style) {
    lv_style_set_align(style, LV_ALIGN_LEFT_MID);
    lv_style_set_text_font(style, &ui_font_font_book20);
    lv_style_set_text_color(style, lv_color_hex(0xffffffff));
    lv_style_set_text_opa(style, 255);
};

lv_style_t *get_style_menu_button_label_MAIN_DEFAULT() {
    static lv_style_t *style;
    if (!style) {
        style = lv_malloc(sizeof(lv_style_t));
        lv_style_init(style);
        init_style_menu_button_label_MAIN_DEFAULT(style);
    }
    return style;
};

void add_style_menu_button_label(lv_obj_t *obj) {
    (void)obj;
    lv_obj_add_style(obj, get_style_menu_button_label_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

void remove_style_menu_button_label(lv_obj_t *obj) {
    (void)obj;
    lv_obj_remove_style(obj, get_style_menu_button_label_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

//
// Style: InfoHeadlineLabel
//

void init_style_info_headline_label_MAIN_DEFAULT(lv_style_t *style) {
    lv_style_set_text_color(style, lv_color_hex(0xff94a3b8));
    lv_style_set_text_font(style, &ui_font_font_book20);
};

lv_style_t *get_style_info_headline_label_MAIN_DEFAULT() {
    static lv_style_t *style;
    if (!style) {
        style = lv_malloc(sizeof(lv_style_t));
        lv_style_init(style);
        init_style_info_headline_label_MAIN_DEFAULT(style);
    }
    return style;
};

void add_style_info_headline_label(lv_obj_t *obj) {
    (void)obj;
    lv_obj_add_style(obj, get_style_info_headline_label_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

void remove_style_info_headline_label(lv_obj_t *obj) {
    (void)obj;
    lv_obj_remove_style(obj, get_style_info_headline_label_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

//
// Style: InfoContentLabel
//

void init_style_info_content_label_MAIN_DEFAULT(lv_style_t *style) {
    lv_style_set_text_font(style, &ui_font_font_book18);
    lv_style_set_text_color(style, lv_color_hex(0xffffffff));
};

lv_style_t *get_style_info_content_label_MAIN_DEFAULT() {
    static lv_style_t *style;
    if (!style) {
        style = lv_malloc(sizeof(lv_style_t));
        lv_style_init(style);
        init_style_info_content_label_MAIN_DEFAULT(style);
    }
    return style;
};

void add_style_info_content_label(lv_obj_t *obj) {
    (void)obj;
    lv_obj_add_style(obj, get_style_info_content_label_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

void remove_style_info_content_label(lv_obj_t *obj) {
    (void)obj;
    lv_obj_remove_style(obj, get_style_info_content_label_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

//
// Style: BackButton
//

void init_style_back_button_MAIN_DEFAULT(lv_style_t *style) {
    lv_style_set_bg_color(style, lv_color_hex(0xff262626));
    lv_style_set_bg_opa(style, 255);
    lv_style_set_radius(style, 10000);
    lv_style_set_shadow_width(style, 0);
};

lv_style_t *get_style_back_button_MAIN_DEFAULT() {
    static lv_style_t *style;
    if (!style) {
        style = lv_malloc(sizeof(lv_style_t));
        lv_style_init(style);
        init_style_back_button_MAIN_DEFAULT(style);
    }
    return style;
};

void add_style_back_button(lv_obj_t *obj) {
    (void)obj;
    lv_obj_add_style(obj, get_style_back_button_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

void remove_style_back_button(lv_obj_t *obj) {
    (void)obj;
    lv_obj_remove_style(obj, get_style_back_button_MAIN_DEFAULT(), LV_PART_MAIN | LV_STATE_DEFAULT);
};

//
//
//

void add_style(lv_obj_t *obj, int32_t styleIndex) {
    typedef void (*AddStyleFunc)(lv_obj_t *obj);
    static const AddStyleFunc add_style_funcs[] = {
        add_style_flex_column_space_between,
        add_style_flex_center,
        add_style_flex_start,
        add_style_flow_row_space_between,
        add_style_flow_row_start_center,
        add_style_flex_column_start,
        add_style_flex_screen,
        add_style_flex_screen_menu,
        add_style_label_font16,
        add_style_label_font_bold30,
        add_style_header_link,
        add_style_menu_button,
        add_style_menu_button_label,
        add_style_info_headline_label,
        add_style_info_content_label,
        add_style_back_button,
    };
    add_style_funcs[styleIndex](obj);
}

void remove_style(lv_obj_t *obj, int32_t styleIndex) {
    typedef void (*RemoveStyleFunc)(lv_obj_t *obj);
    static const RemoveStyleFunc remove_style_funcs[] = {
        remove_style_flex_column_space_between,
        remove_style_flex_center,
        remove_style_flex_start,
        remove_style_flow_row_space_between,
        remove_style_flow_row_start_center,
        remove_style_flex_column_start,
        remove_style_flex_screen,
        remove_style_flex_screen_menu,
        remove_style_label_font16,
        remove_style_label_font_bold30,
        remove_style_header_link,
        remove_style_menu_button,
        remove_style_menu_button_label,
        remove_style_info_headline_label,
        remove_style_info_content_label,
        remove_style_back_button,
    };
    remove_style_funcs[styleIndex](obj);
}

