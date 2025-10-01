#ifndef EDID_H
#define EDID_H

#include <stdint.h>


#include <stddef.h>

/**
 * @brief Read the EDID from the display
 *
 * @param edid Buffer to store the EDID data
 * @param max_size Maximum size of the buffer (should be 128 or 256)
 * @return int Number of bytes read on success, -1 on failure
 */
int get_edid(uint8_t *edid, size_t max_size);

/**
 * @brief Set the EDID of the display
 *
 * @param edid The EDID to set, it can be modified
 * @param size The size of the EDID (should be 128 or 256)
 * @return int 0 on success, -1 on failure
 */
int set_edid(uint8_t *edid, size_t size);

/**
 * @brief Get the status of the videocontroller, aka v4l2-ctl --log-status.
 * User should free the returned string
 *
 * @return const char* The status of the videocontroller
 */
const char* videoc_log_status();

#endif // EDID_H
