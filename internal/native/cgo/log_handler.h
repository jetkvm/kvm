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

/**
 * @brief Set the runtime log level
 *
 * @param level The log level: TRACE=-1, DEBUG=0, INFO=1, WARN=2, ERROR=3, FATAL=4, PANIC=5
 * @return void
 */
void jetkvm_set_log_level(int level);

/**
 * @brief Get the current runtime log level
 *
 * @return int The current log level
 */
int jetkvm_get_log_level(void);

/* Extern declaration for direct access in macros (volatile for single-core RV1106) */
extern volatile int jetkvm_runtime_log_level;

#endif