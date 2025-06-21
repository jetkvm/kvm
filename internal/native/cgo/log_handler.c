#include <stddef.h>
#include "log_handler.h"

/* Log handler */
jetkvm_log_handler_t *log_handler = NULL;

void log_message(int level, const char *filename, const char *funcname, const int line, const char *message) {
    if (log_handler != NULL) {
        log_handler(level, filename, funcname, line, message);
    }
}

void log_set_handler(jetkvm_log_handler_t *handler) {
    log_handler = handler;
}