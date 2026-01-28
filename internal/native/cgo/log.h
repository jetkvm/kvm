#ifndef VIDEO_DAEMON_LOG_H
#define VIDEO_DAEMON_LOG_H

#include <stdio.h>
#include <string.h>
#include <time.h>
#include "log_handler.h"

#define __FILENAME__ (strrchr(__FILE__, '/') ? strrchr(__FILE__, '/') + 1 : __FILE__)

void jetkvm_log(const char *message);

/* Log to screen */
#define emit_log(level, file, func, line, ...) do {                              \
    /* call the log handler */                                                   \
    char msg_buffer[1024];                                                       \
    sprintf(msg_buffer, __VA_ARGS__);                                            \
    log_message(level, file, func, line, msg_buffer);                            \
} while (0)

/* Level enum - matches zerolog levels for consistency */
#define LEVEL_PANIC   5
#define LEVEL_FATAL   4
#define LEVEL_ERROR   3
#define LEVEL_WARN    2
#define LEVEL_INFO    1
#define LEVEL_DEBUG   0
#define LEVEL_TRACE   -1

/*
 * Runtime log level macros - optimized for single-core RV1106 SoC
 * Uses __builtin_expect for branch prediction hints:
 * - TRACE/DEBUG: expected to be disabled (0 = unlikely)
 * - INFO: expected to be disabled at WARN level (0 = unlikely)
 * - WARN/ERROR/PANIC: expected to pass (1 = likely)
 *
 * Short-circuit evaluation: arguments are not evaluated if level check fails,
 * avoiding expensive string formatting when logging is disabled.
 */

/* TRACE LOG - rarely enabled */
#define log_trace(...) do {                                                        \
    if (__builtin_expect(LEVEL_TRACE >= jetkvm_runtime_log_level, 0)) {            \
        emit_log(                                                                  \
            LEVEL_TRACE, __FILENAME__, __func__, __LINE__, __VA_ARGS__             \
        );                                                                         \
    }                                                                              \
} while (0)

/* DEBUG LOG - rarely enabled in production */
#define log_debug(...) do {                                                        \
    if (__builtin_expect(LEVEL_DEBUG >= jetkvm_runtime_log_level, 0)) {            \
        emit_log(                                                                  \
            LEVEL_DEBUG, __FILENAME__, __func__, __LINE__, __VA_ARGS__             \
        );                                                                         \
    }                                                                              \
} while (0)

/* INFO LOG - often disabled (default is WARN) */
#define log_info(...) do {                                                         \
    if (__builtin_expect(LEVEL_INFO >= jetkvm_runtime_log_level, 0)) {             \
        emit_log(                                                                  \
            LEVEL_INFO, __FILENAME__, __func__, __LINE__, __VA_ARGS__              \
        );                                                                         \
    }                                                                              \
} while (0)

/* NOTICE LOG - same as INFO */
#define log_notice(...) do {                                                       \
    if (__builtin_expect(LEVEL_INFO >= jetkvm_runtime_log_level, 0)) {             \
        emit_log(                                                                  \
            LEVEL_INFO, __FILENAME__, __func__, __LINE__, __VA_ARGS__              \
        );                                                                         \
    }                                                                              \
} while (0)

/* WARN LOG - usually enabled */
#define log_warn(...) do {                                                         \
    if (__builtin_expect(LEVEL_WARN >= jetkvm_runtime_log_level, 1)) {             \
        emit_log(                                                                  \
            LEVEL_WARN, __FILENAME__, __func__, __LINE__, __VA_ARGS__              \
        );                                                                         \
    }                                                                              \
} while (0)

/* ERROR LOG - always enabled */
#define log_error(...) do {                                                        \
    if (__builtin_expect(LEVEL_ERROR >= jetkvm_runtime_log_level, 1)) {            \
        emit_log(                                                                  \
            LEVEL_ERROR, __FILENAME__, __func__, __LINE__, __VA_ARGS__             \
        );                                                                         \
    }                                                                              \
} while (0)

/* PANIC LOG - always enabled */
#define log_panic(...) do {                                                        \
    if (__builtin_expect(LEVEL_PANIC >= jetkvm_runtime_log_level, 1)) {            \
        emit_log(                                                                  \
            LEVEL_PANIC, __FILENAME__, __func__, __LINE__, __VA_ARGS__             \
        );                                                                         \
    }                                                                              \
} while (0)

#endif //VIDEO_DAEMON_LOG_H
