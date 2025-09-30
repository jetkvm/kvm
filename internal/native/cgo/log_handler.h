#ifndef LOG_HANDLER_H
#define LOG_HANDLER_H

typedef void (jetkvm_log_handler_t)(int level, const char *filename, const char *funcname, const int line, const char *message);

/**
 * @brief Log a message
 *
 * @param level The level of the message
 * @param filename The filename of the message
 * @param funcname The function name of the message
 * @param line The line number of the message
 * @param message The message to log
 * @return void
 */
void log_message(int level, const char *filename, const char *funcname, const int line, const char *message);

/**
 * @brief Set the log handler
 *
 * @param handler The handler to set
 * @return void
 */
void log_set_handler(jetkvm_log_handler_t *handler);

#endif