#ifndef EEZ_LVGL_UI_SCREENS_H
#define EEZ_LVGL_UI_SCREENS_H

#include <lvgl.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct _objects_t {
    lv_obj_t *boot_screen;
    lv_obj_t *no_network_screen;
    lv_obj_t *home_screen;
    lv_obj_t *menu_screen;
    lv_obj_t *menu_advanced_screen;
    lv_obj_t *menu_network_screen;
    lv_obj_t *about_screen;
    lv_obj_t *status_screen;
    lv_obj_t *reset_config_screen;
    lv_obj_t *reboot_screen;
    lv_obj_t *rebooting_screen;
    lv_obj_t *boot_logo;
    lv_obj_t *boot_screen_version;
    lv_obj_t *no_network_header_container;
    lv_obj_t *no_network_header_logo;
    lv_obj_t *no_network_content_container;
    lv_obj_t *no_network_title;
    lv_obj_t *home_info_ipv6_addr_1;
    lv_obj_t *home_header_container;
    lv_obj_t *home_header_logo;
    lv_obj_t *cloud_status_icon;
    lv_obj_t *cloud_status_label;
    lv_obj_t *home_info_container;
    lv_obj_t *home_info_ipv4_addr;
    lv_obj_t *home_info_ipv6_addr;
    lv_obj_t *home_info_mac_addr;
    lv_obj_t *divider;
    lv_obj_t *home_status_container;
    lv_obj_t *usb_status;
    lv_obj_t *usb_indicator;
    lv_obj_t *usb_status_label;
    lv_obj_t *hdmi_status;
    lv_obj_t *hdmi_indicator;
    lv_obj_t *hdmi_status_label;
    lv_obj_t *menu_header_container;
    lv_obj_t *menu_items_container;
    lv_obj_t *menu_btn_status;
    lv_obj_t *menu_btn_network;
    lv_obj_t *menu_btn_access;
    lv_obj_t *menu_btn_advanced;
    lv_obj_t *menu_btn_about;
    lv_obj_t *menu_header_container_1;
    lv_obj_t *menu_items_container_1;
    lv_obj_t *menu_btn_advanced_developer_mode;
    lv_obj_t *menu_btn_advanced_usb_emulation;
    lv_obj_t *menu_btn_advanced_reboot;
    lv_obj_t *menu_btn_advanced_reset_config;
    lv_obj_t *menu_header_container_2;
    lv_obj_t *menu_items_container_2;
    lv_obj_t *menu_btn_network_ipv4;
    lv_obj_t *menu_btn_network_ipv6;
    lv_obj_t *menu_btn_network_lldp;
    lv_obj_t *about_header_container;
    lv_obj_t *about_items_container;
    lv_obj_t *system_version_container;
    lv_obj_t *system_version;
    lv_obj_t *app_version_container;
    lv_obj_t *app_version;
    lv_obj_t *build_branch_container;
    lv_obj_t *build_branch;
    lv_obj_t *build_date_container;
    lv_obj_t *build_date;
    lv_obj_t *golang_version_container;
    lv_obj_t *golang_version;
    lv_obj_t *lvgl_version_container;
    lv_obj_t *lvgl_version;
    lv_obj_t *kernel_version_container;
    lv_obj_t *kernel_version;
    lv_obj_t *cpu_serial_container;
    lv_obj_t *cpu_serial;
    lv_obj_t *status_header_container;
    lv_obj_t *status_items_container;
    lv_obj_t *device_id_container;
    lv_obj_t *device_id;
    lv_obj_t *cloud_account_id_container;
    lv_obj_t *app_version_1;
    lv_obj_t *cloud_domain_container;
    lv_obj_t *cloud_domain;
    lv_obj_t *reset_config_header;
    lv_obj_t *reset_config_container;
    lv_obj_t *reset_config_label_container;
    lv_obj_t *reset_config_label;
    lv_obj_t *reset_config_spinner;
    lv_obj_t *reset_config_button;
    lv_obj_t *obj0;
    lv_obj_t *reboot_header;
    lv_obj_t *reboot_container;
    lv_obj_t *reboot_label_container;
    lv_obj_t *reboot_label;
    lv_obj_t *reboot_config_button;
    lv_obj_t *obj1;
    lv_obj_t *reboot_in_progress_logo;
    lv_obj_t *reboot_in_progress_label;
} objects_t;

extern objects_t objects;

enum ScreensEnum {
    SCREEN_ID_BOOT_SCREEN = 1,
    SCREEN_ID_NO_NETWORK_SCREEN = 2,
    SCREEN_ID_HOME_SCREEN = 3,
    SCREEN_ID_MENU_SCREEN = 4,
    SCREEN_ID_MENU_ADVANCED_SCREEN = 5,
    SCREEN_ID_MENU_NETWORK_SCREEN = 6,
    SCREEN_ID_ABOUT_SCREEN = 7,
    SCREEN_ID_STATUS_SCREEN = 8,
    SCREEN_ID_RESET_CONFIG_SCREEN = 9,
    SCREEN_ID_REBOOT_SCREEN = 10,
    SCREEN_ID_REBOOTING_SCREEN = 11,
};

void create_screen_boot_screen();
void tick_screen_boot_screen();

void create_screen_no_network_screen();
void tick_screen_no_network_screen();

void create_screen_home_screen();
void tick_screen_home_screen();

void create_screen_menu_screen();
void tick_screen_menu_screen();

void create_screen_menu_advanced_screen();
void tick_screen_menu_advanced_screen();

void create_screen_menu_network_screen();
void tick_screen_menu_network_screen();

void create_screen_about_screen();
void tick_screen_about_screen();

void create_screen_status_screen();
void tick_screen_status_screen();

void create_screen_reset_config_screen();
void tick_screen_reset_config_screen();

void create_screen_reboot_screen();
void tick_screen_reboot_screen();

void create_screen_rebooting_screen();
void tick_screen_rebooting_screen();

void tick_screen_by_id(enum ScreensEnum screenId);
void tick_screen(int screen_index);

void create_screens();


#ifdef __cplusplus
}
#endif

#endif /*EEZ_LVGL_UI_SCREENS_H*/