#ifndef LOG_HANDLER_H
#define LOG_HANDLER_H

typedef void (jetkvm_log_handler_t)(int level, const char *filename, const char *funcname, const int line, const char *message);
void log_message(int level, const char *filename, const char *funcname, const int line, const char *message);

void log_set_handler(jetkvm_log_handler_t *handler);

#endif