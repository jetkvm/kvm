#ifndef JETKVM_NATIVE_MAIN_H
#define JETKVM_NATIVE_MAIN_H

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <errno.h>
#include "ctrl.h"

void jetkvm_c_log_handler(int level, const char *filename, const char *funcname, int line, const char *message);
void jetkvm_video_handler(const uint8_t *frame, ssize_t len);
void jetkvm_video_state_handler(jetkvm_video_state_t *state);
void jetkvm_indev_handler(int code);
void jetkvm_rpc_handler(const char *method, const char *params);


// typedef void (jetkvm_video_state_handler_t)(jetkvm_video_state_t *state);
// typedef void (jetkvm_log_handler_t)(int level, const char *filename, const char *funcname, int line, const char *message);
// typedef void (jetkvm_rpc_handler_t)(const char *method, const char *params);
// typedef void (jetkvm_video_handler_t)(const uint8_t *frame, ssize_t len);
// typedef void (jetkvm_indev_handler_t)(int code);

#endif