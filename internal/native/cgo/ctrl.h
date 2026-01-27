#ifndef VIDEO_DAEMON_CTRL_H
#define VIDEO_DAEMON_CTRL_H

#include <stdbool.h>
#include <stdint.h>
#include <sys/types.h>

typedef struct
{
    bool ready;
    uint8_t streaming;
    const char *error;
    u_int16_t width;
    u_int16_t height;
    double frame_per_second;
} jetkvm_video_state_t;

typedef void (jetkvm_video_state_handler_t)(volatile jetkvm_video_state_t *state);
typedef void (jetkvm_log_handler_t)(int level, const char *filename, const char *funcname, int line, const char *message);
typedef void (jetkvm_rpc_handler_t)(const char *method, const char *params);
typedef void (jetkvm_video_handler_t)(const uint8_t *frame, ssize_t len);
typedef void (jetkvm_jpeg_handler_t)(const uint8_t *frame, ssize_t len);
typedef void (jetkvm_rgb_handler_t)(const uint8_t *frame, ssize_t len, uint32_t width, uint32_t height);
typedef void (jetkvm_indev_handler_t)(int code);

void jetkvm_set_log_handler(jetkvm_log_handler_t *handler);
void jetkvm_set_video_handler(jetkvm_video_handler_t *handler);
void jetkvm_set_jpeg_handler(jetkvm_jpeg_handler_t *handler);
void jetkvm_set_rgb_handler(jetkvm_rgb_handler_t *handler);
void jetkvm_set_indev_handler(jetkvm_indev_handler_t *handler);
void jetkvm_set_rpc_handler(jetkvm_rpc_handler_t *handler);
void jetkvm_call_rpc_handler(const char *method, const char *params);
void jetkvm_set_video_state_handler(jetkvm_video_state_handler_t *handler);
void jetkvm_crash();

void jetkvm_ui_set_var(const char *name, const char *value);
const char *jetkvm_ui_get_var(const char *name);

void jetkvm_ui_init(u_int16_t rotation);
void jetkvm_ui_tick();


void jetkvm_ui_set_rotation(u_int16_t rotation);
const char *jetkvm_ui_get_current_screen();
void jetkvm_ui_load_screen(const char *obj_name);
int jetkvm_ui_set_text(const char *obj_name, const char *text);
void jetkvm_ui_set_image(const char *obj_name, const char *image_name);
void jetkvm_ui_add_state(const char *obj_name, const char *state_name);
void jetkvm_ui_clear_state(const char *obj_name, const char *state_name);
void jetkvm_ui_fade_in(const char *obj_name, u_int32_t duration);
void jetkvm_ui_fade_out(const char *obj_name, u_int32_t duration);
void jetkvm_ui_set_opacity(const char *obj_name, u_int8_t opacity);
int jetkvm_ui_add_flag(const char *obj_name, const char *flag_name);
int jetkvm_ui_clear_flag(const char *obj_name, const char *flag_name);

const char *jetkvm_ui_get_lvgl_version();

const char *jetkvm_ui_event_code_to_name(int code);

int jetkvm_video_init(float quality_factor);
void jetkvm_video_shutdown();
void jetkvm_video_start();
void jetkvm_video_stop();
uint8_t jetkvm_video_get_streaming_status();
int jetkvm_video_set_quality_factor(float quality_factor);
float jetkvm_video_get_quality_factor();
int jetkvm_video_set_edid(const char *edid_hex);
char *jetkvm_video_get_edid_hex();
char *jetkvm_video_log_status();
volatile jetkvm_video_state_t *jetkvm_video_get_status();

void video_report_format(bool ready, const char *error, u_int16_t width, u_int16_t height, double frame_per_second);
void video_send_format_report();
int video_send_frame(const uint8_t *frame, ssize_t len);
int video_send_jpeg_frame(const uint8_t *frame, ssize_t len);
int video_send_rgb_frame(const uint8_t *frame, ssize_t len, uint32_t width, uint32_t height);

// JPEG encoder control
int jetkvm_jpeg_start(int quality);
void jetkvm_jpeg_stop();
int jetkvm_jpeg_set_quality(int quality);
int jetkvm_jpeg_get_quality();
bool jetkvm_jpeg_is_running();

// RGA RGB encoder control (hardware YUV to RGB conversion)
int jetkvm_rgb_start();
void jetkvm_rgb_stop();
bool jetkvm_rgb_is_running();

// H.264 encoder control
int jetkvm_video_request_keyframe();

// H.264/YUV to MJPEG transcoder control (BETA feature)
// Used for camera redirection to convert various formats to MJPEG for UVC gadget
// Supports RGA hardware-accelerated scaling from input to output resolution
typedef void (*jetkvm_transcode_output_cb)(const uint8_t *jpeg_data, size_t jpeg_len, void *user_data);
int jetkvm_transcode_init(uint32_t input_width, uint32_t input_height,
                          uint32_t output_width, uint32_t output_height,
                          uint32_t fps, uint32_t quality,
                          jetkvm_transcode_output_cb cb, void *user_data);
void jetkvm_transcode_shutdown(void);
bool jetkvm_transcode_is_running(void);
int jetkvm_transcode_feed_h264(const uint8_t *data, size_t len);
int jetkvm_transcode_feed_nv12(const uint8_t *data, size_t len); // Direct NV12 (fastest)
int jetkvm_transcode_feed_i420(const uint8_t *data, size_t len); // I420 (NEON convert)
int jetkvm_transcode_feed_yuy2(const uint8_t *data, size_t len); // YUY2 (NEON convert)

#endif //VIDEO_DAEMON_CTRL_H
