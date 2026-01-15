#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <sys/un.h>
#include <sys/socket.h>
#include <errno.h>
#include <unistd.h>
#include <pthread.h>
#include <stdint.h>
#include <fcntl.h>
#include "video.h"
#include "screen.h"
#include "edid.h"
#include "ctrl.h"
#include <lvgl.h>
#include "ui_index.h"
#include "log.h"
#include "log_handler.h"

// Making state extern to ensure single definition across compilation units
volatile jetkvm_video_state_t state = {0};
jetkvm_video_state_handler_t *video_state_handler = NULL;
// Non-static for debugging - ensures single definition
volatile uint32_t video_report_call_count = 0;
volatile uint32_t video_get_status_call_count = 0;
// Unique identifier to verify same compilation unit
const char *ctrl_build_id = __FILE__ " " __DATE__ " " __TIME__;

// Setter function to allow video.c to modify the counter (avoids extern issues)
void ctrl_set_report_count(uint32_t value) {
    video_report_call_count = value;
    fprintf(stderr, "INFO: ctrl_set_report_count: Set counter=%u at addr=%p\n",
            video_report_call_count, (void*)&video_report_call_count);
    fflush(stderr);
}

jetkvm_rpc_handler_t *rpc_handler = NULL;
jetkvm_video_handler_t *video_handler = NULL;
jetkvm_jpeg_handler_t *jpeg_handler = NULL;


void jetkvm_set_log_handler(jetkvm_log_handler_t *handler) {
    log_set_handler(handler);
}

void jetkvm_set_video_handler(jetkvm_video_handler_t *handler) {
    video_handler = handler;
}

void jetkvm_set_jpeg_handler(jetkvm_jpeg_handler_t *handler) {
    jpeg_handler = handler;
}

static jetkvm_indev_handler_t *jetkvm_indev_handler = NULL;

static void jetkvm_indev_wrapper(lv_event_code_t code) {
    if (jetkvm_indev_handler != NULL) {
        (*jetkvm_indev_handler)((int)code);
    }
}

void jetkvm_set_indev_handler(jetkvm_indev_handler_t *handler) {
    jetkvm_indev_handler = handler;
    lvgl_set_indev_handler(jetkvm_indev_wrapper);
}

void jetkvm_set_rpc_handler(jetkvm_rpc_handler_t *handler) {
    rpc_handler = handler;
}

void jetkvm_call_rpc_handler(const char *method, const char *params) {
    if (rpc_handler != NULL) {
        (*rpc_handler)(method, params);
    }
}

const char *jetkvm_ui_event_code_to_name(int code) {
    return lv_event_code_get_name((lv_event_code_t)code);
}

void video_report_format(bool ready, const char *error, u_int16_t width, u_int16_t height, double frame_per_second)
{
    video_report_call_count++;
    if (video_report_call_count == 1) {
        fprintf(stderr, "INFO: video_report_format: BUILD_ID=%s, state=%p, counter=%p\n",
                ctrl_build_id, (void*)&state, (void*)&video_report_call_count);
    }
    fprintf(stderr, "INFO: video_report_format[%u]: state address=%p, setting ready=%d, width=%d, height=%d, error=%s\n",
            video_report_call_count, (void*)&state, ready, width, height, error ? error : "none");
    fflush(stderr);
    state.streaming = video_get_streaming_status();
    state.ready = ready;
    state.error = error;
    state.width = width;
    state.height = height;
    state.frame_per_second = frame_per_second;
    // Verify the values were written correctly
    fprintf(stderr, "INFO: video_report_format[%u]: AFTER SET: state.ready=%d, state.width=%d, state.height=%d\n",
            video_report_call_count, state.ready, state.width, state.height);
    fflush(stderr);
    if (video_state_handler != NULL) {
        (*video_state_handler)(&state);
    }
}

void video_send_format_report() {
    state.streaming = video_get_streaming_status();
    if (video_state_handler != NULL) {
        (*video_state_handler)(&state);
    }
}

int video_send_frame(const uint8_t *frame, ssize_t len)
{
    if (video_handler != NULL) {
        (*video_handler)(frame, len);
    } else {
        log_error("video handler is not set");
    }
    return 0;
}

int video_send_jpeg_frame(const uint8_t *frame, ssize_t len)
{
    if (jpeg_handler != NULL) {
        (*jpeg_handler)(frame, len);
    }
    return 0;
}

/**
 * @brief Convert a hexadecimal string to an array of uint8_t bytes
 *
 * @param hex_str The input hexadecimal string
 * @param bytes The output byte array (must be pre-allocated)
 * @param max_len The maximum number of bytes that can be stored in the output array
 * @return int The number of bytes converted, or -1 on error
 */
int hex_to_bytes(const char *hex_str, uint8_t *bytes, size_t max_len)
{
    size_t hex_len = strnlen(hex_str, 4096);
    if (hex_len % 2 != 0 || hex_len / 2 > max_len)
    {
        return -1; // Invalid input length or insufficient output buffer
    }

    for (size_t i = 0; i < hex_len; i += 2)
    {
        char byte_str[3] = {hex_str[i], hex_str[i + 1], '\0'};
        char *end_ptr;
        long value = strtol(byte_str, &end_ptr, 16);

        if (*end_ptr != '\0' || value < 0 || value > 255)
        {
            return -1; // Invalid hexadecimal value
        }

        bytes[i / 2] = (uint8_t)value;
    }

    return hex_len / 2;
}

/**
 * @brief Convert an array of uint8_t bytes to a hexadecimal string, user must free the returned string
 *
 * @param bytes The input byte array
 * @param len The number of bytes in the input array
 * @return char* The output hexadecimal string (dynamically allocated, must be freed by the caller), or NULL on error
 */
const char *bytes_to_hex(const uint8_t *bytes, size_t len)
{
    if (bytes == NULL || len == 0)
    {
        return NULL;
    }

    char *hex_str = malloc(2 * len + 1); // Each byte becomes 2 hex chars, plus null terminator
    if (hex_str == NULL)
    {
        return NULL; // Memory allocation failed
    }

    for (size_t i = 0; i < len; i++)
    {
        snprintf(hex_str + (2 * i), 3, "%02x", bytes[i]);
    }

    hex_str[2 * len] = '\0'; // Ensure null termination
    return hex_str;
}

lv_obj_flag_t str_to_lv_obj_flag(const char *flag)
{
    if (strcmp(flag, "LV_OBJ_FLAG_HIDDEN") == 0)
    {
        return LV_OBJ_FLAG_HIDDEN;
    }
    else if (strcmp(flag, "LV_OBJ_FLAG_CLICKABLE") == 0)
    {
        return LV_OBJ_FLAG_CLICKABLE;
    }
    else if (strcmp(flag, "LV_OBJ_FLAG_SCROLLABLE") == 0)
    {
        return LV_OBJ_FLAG_SCROLLABLE;
    }
    else if (strcmp(flag, "LV_OBJ_FLAG_CLICK_FOCUSABLE") == 0)
    {
        return LV_OBJ_FLAG_CLICK_FOCUSABLE;
    }
    else if (strcmp(flag, "LV_OBJ_FLAG_SCROLL_ON_FOCUS") == 0)
    {
        return LV_OBJ_FLAG_SCROLL_ON_FOCUS;
    }
    else if (strcmp(flag, "LV_OBJ_FLAG_SCROLL_CHAIN") == 0)
    {
        return LV_OBJ_FLAG_SCROLL_CHAIN;
    }
    else if (strcmp(flag, "LV_OBJ_FLAG_PRESS_LOCK") == 0)
    {
        return LV_OBJ_FLAG_PRESS_LOCK;
    }
    else if (strcmp(flag, "LV_OBJ_FLAG_OVERFLOW_VISIBLE") == 0)
    {
        return LV_OBJ_FLAG_OVERFLOW_VISIBLE;
    }
    else
    {
        return 0; // Unknown flag
    }
}

void jetkvm_ui_set_var(const char *name, const char *value) {
    for (int i = 0; i < ui_vars_size; i++) {
        if (strcmp(ui_vars[i].name, name) == 0) {
            ui_vars[i].setter(value);
            return;
        }
    }
    log_error("variable %s not found", name);
}

const char *jetkvm_ui_get_var(const char *name) {
    for (int i = 0; i < ui_vars_size; i++) {
        if (strcmp(ui_vars[i].name, name) == 0) {
            return ui_vars[i].getter();
        }
    }
    log_error("variable %s not found", name);
    return NULL;
}

void jetkvm_ui_init(u_int16_t rotation) {
    lvgl_init(rotation);
}

void jetkvm_ui_tick() {
    lvgl_tick();
}

void jetkvm_set_video_state_handler(jetkvm_video_state_handler_t *handler) {
    video_state_handler = handler;
}

void jetkvm_ui_set_rotation(u_int16_t rotation)
{
    lvgl_set_rotation(NULL, rotation);
}

const char *jetkvm_ui_get_current_screen() {
    return ui_get_current_screen();
}

void jetkvm_ui_load_screen(const char *obj_name) {
    lv_obj_t *obj = ui_get_obj(obj_name);
    if (obj == NULL) {
        return;
    }

    if (lv_scr_act() != obj) {
        lv_scr_load(obj);
    }
}

int jetkvm_ui_set_text(const char *obj_name, const char *text) {
    lv_obj_t *obj = ui_get_obj(obj_name);
    if (obj == NULL) {
        return -1;
    }

    if (strcmp(lv_label_get_text(obj), text) == 0) {
        return 1;
    }

    lv_label_set_text(obj, text);
    return 0;
}

void jetkvm_ui_set_image(const char *obj_name, const char *image_name) {
    lv_obj_t *obj = ui_get_obj(obj_name);
    if (obj == NULL) {
        return;
    }
    lv_img_set_src(obj, image_name);
}

lv_state_t str_to_lv_state(const char *state_name) {
    if (strcmp(state_name, "LV_STATE_USER_1") == 0) {
        return LV_STATE_USER_1;
    }
    else if (strcmp(state_name, "LV_STATE_USER_2") == 0) {
        return LV_STATE_USER_2;
    }
    else if (strcmp(state_name, "LV_STATE_USER_3") == 0) {
        return LV_STATE_USER_3;
    }
    else if (strcmp(state_name, "LV_STATE_USER_4") == 0) {
        return LV_STATE_USER_4;
    }
    else if (strcmp(state_name, "LV_STATE_DISABLED") == 0) {
        return LV_STATE_DISABLED;
    }
    else if (strcmp(state_name, "LV_STATE_DEFAULT") == 0) {
        return LV_STATE_DEFAULT;
    }
    else if (strcmp(state_name, "LV_STATE_CHECKED") == 0) {
        return LV_STATE_CHECKED;
    }
    else if (strcmp(state_name, "LV_STATE_FOCUSED") == 0) {
        return LV_STATE_FOCUSED;
    }
    return LV_STATE_DEFAULT;
}

void jetkvm_ui_add_state(const char *obj_name, const char *state_name) {
    lv_obj_t *obj = ui_get_obj(obj_name);
    if (obj == NULL) {
        return;
    }
    lv_state_t state_val = str_to_lv_state(state_name);
    lv_obj_add_state(obj, state_val);
}

void jetkvm_ui_clear_state(const char *obj_name, const char *state_name) {
    lv_obj_t *obj = ui_get_obj(obj_name);
    if (obj == NULL) {
        return;
    }
    lv_state_t state_val = str_to_lv_state(state_name);
    lv_obj_clear_state(obj, state_val);
}

int jetkvm_ui_add_flag(const char *obj_name, const char *flag_name) {
    lv_obj_t *obj = ui_get_obj(obj_name);
    if (obj == NULL) {
        return -1;
    }

    lv_obj_flag_t flag_val = str_to_lv_obj_flag(flag_name);
    if (flag_val == 0)
    {
        return -2;
    }
    lv_obj_add_flag(obj, flag_val);
    return 0;
}

int jetkvm_ui_clear_flag(const char *obj_name, const char *flag_name) {
    lv_obj_t *obj = ui_get_obj(obj_name);
    if (obj == NULL) {
        return -1;
    }

    lv_obj_flag_t flag_val = str_to_lv_obj_flag(flag_name);
    if (flag_val == 0)
    {
        return -2;
    }
    lv_obj_clear_flag(obj, flag_val);
    return 0;
}

void jetkvm_ui_fade_in(const char *obj_name, u_int32_t duration) {
    lv_obj_t *obj = ui_get_obj(obj_name);
    if (obj == NULL) {
        return;
    }
    lv_obj_fade_in(obj, duration, 0);
}

void jetkvm_ui_fade_out(const char *obj_name, u_int32_t duration) {
    lv_obj_t *obj = ui_get_obj(obj_name);
    if (obj == NULL) {
        return;
    }
    lv_obj_fade_out(obj, duration, 0);
}

void jetkvm_ui_set_opacity(const char *obj_name, u_int8_t opacity) {
    lv_obj_t *obj = ui_get_obj(obj_name);
    if (obj == NULL) {
        return;
    }
    lv_obj_set_style_opa(obj, opacity, LV_PART_MAIN);
}

const char *jetkvm_ui_get_lvgl_version() {
    return lv_version_info();
}

void jetkvm_video_start() {
    video_start_streaming();
}

void jetkvm_video_stop() {
    video_stop_streaming();
}

uint8_t jetkvm_video_get_streaming_status() {
    return video_get_streaming_status();
}

int jetkvm_video_set_quality_factor(float quality_factor) {
    if (quality_factor <= 0 || quality_factor > 1) {
        return -1;
    }
    video_set_quality_factor(quality_factor);
    return 0;
}

float jetkvm_video_get_quality_factor() {
    return video_get_quality_factor();
}

int jetkvm_video_set_edid(const char *edid_hex) {
    uint8_t edid[256];
    int edid_len = hex_to_bytes(edid_hex, edid, 256);
    if (edid_len < 0) {
        return -1;
    }
    return set_edid(edid, edid_len);
}

char *jetkvm_video_get_edid_hex() {
    uint8_t edid[256];
    int edid_len = get_edid(edid, 256);
    if (edid_len < 0) {
        return NULL;
    }
    return (char *)bytes_to_hex(edid, edid_len);
}

jetkvm_video_state_t *jetkvm_video_get_status() {
    video_get_status_call_count++;
    if (video_get_status_call_count == 1) {
        fprintf(stderr, "INFO: jetkvm_video_get_status: BUILD_ID=%s, state=%p, counter=%p\n",
                ctrl_build_id, (void*)&state, (void*)&video_report_call_count);
    }
    // Read values into local variables first to ensure atomic read
    bool local_ready = state.ready;
    uint16_t local_width = state.width;
    uint16_t local_height = state.height;
    fprintf(stderr, "INFO: jetkvm_video_get_status[%u]: state addr=%p, report_calls=%u, ready=%d, width=%d, height=%d\n",
            video_get_status_call_count, (void*)&state, video_report_call_count, local_ready, local_width, local_height);
    // Print raw bytes of state struct for debugging (only first few calls to avoid spam)
    if (video_get_status_call_count <= 5) {
        unsigned char *bytes = (unsigned char *)&state;
        fprintf(stderr, "INFO: jetkvm_video_get_status: raw bytes (first 20): ");
        for (int i = 0; i < 20 && i < sizeof(state); i++) {
            fprintf(stderr, "%02x ", bytes[i]);
        }
        fprintf(stderr, "\n");
    }
    fflush(stderr);
    return &state;
}

char *jetkvm_video_log_status() {
    return (char *)videoc_log_status();
}

int jetkvm_video_init(float factor) {
    return video_init(factor);
}

void jetkvm_video_shutdown() {
    video_shutdown();
}

void jetkvm_crash() {
    // let's call a function that will crash the program
    int* p = 0;
    *p = 0;
}

// JPEG encoder wrapper functions
int jetkvm_jpeg_start(int quality) {
    return jpeg_encoder_start(quality);
}

void jetkvm_jpeg_stop() {
    jpeg_encoder_stop();
}

int jetkvm_jpeg_set_quality(int quality) {
    return jpeg_encoder_set_quality(quality);
}

int jetkvm_jpeg_get_quality() {
    return jpeg_encoder_get_quality();
}

bool jetkvm_jpeg_is_running() {
    return jpeg_encoder_is_running();
}
