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

typedef void (jetkvm_video_state_handler_t)(jetkvm_video_state_t *state);
typedef void (jetkvm_log_handler_t)(int level, const char *filename, const char *funcname, int line, const char *message);
typedef void (jetkvm_rpc_handler_t)(const char *method, const char *params);
typedef void (jetkvm_video_handler_t)(const uint8_t *frame, ssize_t len);
typedef void (jetkvm_mjpeg_handler_t)(const uint8_t *frame, ssize_t len);
typedef void (jetkvm_indev_handler_t)(int code);

void jetkvm_set_log_handler(jetkvm_log_handler_t *handler);
void jetkvm_set_video_handler(jetkvm_video_handler_t *handler);
void jetkvm_set_mjpeg_handler(jetkvm_mjpeg_handler_t *handler);
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
jetkvm_video_state_t *jetkvm_video_get_status();

void video_report_format(bool ready, const char *error, u_int16_t width, u_int16_t height, double frame_per_second);
void video_send_format_report();
int video_send_frame(const uint8_t *frame, ssize_t len);
int video_send_mjpeg_frame(const uint8_t *frame, ssize_t len);

// UVC/MJPEG streaming control
void jetkvm_mjpeg_set_enabled(bool enabled);
bool jetkvm_mjpeg_get_enabled();

// MJPEG frame rate and quality control
// frame_divisor: 1 = full fps, 2 = half fps, 3 = third fps, etc.
void jetkvm_mjpeg_set_frame_divisor(int divisor);
int jetkvm_mjpeg_get_frame_divisor();
// quality: 0.1 to 1.0 (1.0 = highest quality, larger files)
void jetkvm_mjpeg_set_quality(float quality);
float jetkvm_mjpeg_get_quality();

// H.264 to MJPEG transcoder for camera passthrough
// Decodes browser H.264 and encodes to MJPEG for UVC
int jetkvm_transcode_init(int width, int height);
int jetkvm_transcode_start();
void jetkvm_transcode_stop();
void jetkvm_transcode_shutdown();
int jetkvm_transcode_send_h264(const uint8_t *frame, ssize_t len);
bool jetkvm_transcode_is_running();

#endif //VIDEO_DAEMON_CTRL_H
