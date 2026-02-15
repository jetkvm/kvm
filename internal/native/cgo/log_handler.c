#include <stddef.h>
#include "log_handler.h"

/* Log handler */
jetkvm_log_handler_t *log_handler = NULL;

/* Runtime log level: 1=INFO (default), 2=WARN, 3=ERROR
 * Using volatile for single-core RV1106 (no cache coherency issues)
 * Levels map to: TRACE=-1, DEBUG=0, INFO=1, WARN=2, ERROR=3, FATAL=4, PANIC=5
 */
volatile int jetkvm_runtime_log_level = 2;  /* Default to WARN */

void log_message(int level, const char *filename, const char *funcname, const int line, const char *message) {
    /* Check runtime log level - only emit if message level >= configured level */
    if (level < jetkvm_runtime_log_level) {
        return;
    }
    if (log_handler != NULL) {
        log_handler(level, filename, funcname, line, message);
    }
}

void log_set_handler(jetkvm_log_handler_t *handler) {
    log_handler = handler;
}

void jetkvm_set_log_level(int level) {
    /* Clamp to valid range: TRACE=-1 to PANIC=5 */
    if (level < -1) level = -1;
    if (level > 5) level = 5;
    jetkvm_runtime_log_level = level;
}

int jetkvm_get_log_level(void) {
    return jetkvm_runtime_log_level;
}