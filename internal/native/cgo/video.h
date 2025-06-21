#ifndef VIDEO_DAEMON_VIDEO_H
#define VIDEO_DAEMON_VIDEO_H

int video_init();
void video_shutdown();
void *run_detect_format(void *arg);
void video_start_streaming();
void video_stop_streaming();

void video_set_quality_factor(float factor);
float video_get_quality_factor();

#endif //VIDEO_DAEMON_VIDEO_H
