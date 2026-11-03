#include <assert.h>
#include <stdarg.h>

// Rename declarations too, keeping the real syscall signatures. This fixture
// compiles the production source rather than a copied get_edid implementation.
#define open test_open
#define close test_close
#define ioctl test_ioctl
#define klogctl test_klogctl
#include "edid.c"

static unsigned int requested_blocks, returned_blocks = 1;
static int open_calls, close_calls, open_error, ioctl_error;
int test_open(const char *name, int flags, ...) {
    assert(strcmp(name, V4L_SUBDEV) == 0);
    assert(flags == O_RDWR);
    open_calls++;
    if (open_error) { errno = EACCES; return -1; }
    return 100;
}
int test_close(int fd) { assert(fd == 100); close_calls++; return 0; }
int test_ioctl(int fd, unsigned long request, ...) {
    assert(fd == 100 && request == VIDIOC_G_EDID);
    va_list args; va_start(args, request);
    struct v4l2_edid *value = va_arg(args, struct v4l2_edid *);
    va_end(args);
    assert(value->pad == 0 && value->start_block == 0);
    requested_blocks = value->blocks;
    if (ioctl_error) { errno = EIO; return -1; }
    // Model a driver filling the number of blocks requested, without performing
    // out-of-allocation writes: tests reserve storage for the trailing guard.
    memset(value->edid, 0x42, value->blocks * 128);
    value->blocks = returned_blocks;
    return 0;
}
int test_klogctl(int type, char *buf, int len) {
    (void)type; (void)buf; (void)len; assert(0); return -1;
}
void log_message(int level, const char *file, const char *func, int line, const char *message) {
    (void)level; (void)file; (void)func; (void)line; (void)message;
}
static void guard_is_intact(const uint8_t *data, size_t begin, size_t end) {
    for (size_t i = begin; i < end; i++) { assert(data[i] == 0xa5); }
}
int main(int argc, char **argv) {
    assert(argc == 2);
    uint8_t storage[512]; memset(storage, 0xa5, sizeof(storage));
    uint8_t *data = storage + 128;
    if (strcmp(argv[1], "capacity128") == 0) {
        assert(get_edid(data, 128) == 128);
        guard_is_intact(storage, 256, sizeof(storage));
        assert(requested_blocks == 1);
    } else if (strcmp(argv[1], "capacity256") == 0) {
        returned_blocks = 2;
        assert(get_edid(data, 256) == 256 && requested_blocks == 2);
    } else if (strcmp(argv[1], "one_block_in256") == 0) {
        assert(get_edid(data, 256) == 128 && requested_blocks == 2);
    } else if (strcmp(argv[1], "empty_result") == 0) {
        returned_blocks = 0;
        assert(get_edid(data, 256) == -1 && errno == EIO);
    } else if (strcmp(argv[1], "oversize128") == 0) {
        returned_blocks = 2;
        assert(get_edid(data, 128) == -1 && errno == EIO);
    } else if (strcmp(argv[1], "oversize256") == 0) {
        returned_blocks = 3;
        assert(get_edid(data, 256) == -1 && errno == EIO);
    } else if (strcmp(argv[1], "invalid_capacity") == 0) {
        for (size_t size = 0; size <= 384; size++) {
            if (size == 128 || size == 256) { continue; }
            assert(get_edid(data, size) == -1 && errno == EINVAL);
        }
        assert(open_calls == 0 && close_calls == 0);
        return 0;
    } else if (strcmp(argv[1], "null_buffer") == 0) {
        assert(get_edid(NULL, 128) == -1 && errno == EINVAL);
        assert(open_calls == 0 && close_calls == 0);
        return 0;
    } else if (strcmp(argv[1], "open_error") == 0) {
        open_error = 1;
        assert(get_edid(data, 128) == -1 && errno == EACCES);
        assert(open_calls == 1 && close_calls == 0);
        return 0;
    } else if (strcmp(argv[1], "ioctl_error") == 0) {
        ioctl_error = 1;
        assert(get_edid(data, 128) == -1 && errno == EIO);
    } else { return 2; }
    guard_is_intact(storage, 0, 128);
    guard_is_intact(storage, 384, sizeof(storage));
    assert(open_calls == 1 && close_calls == 1);
    return 0;
}
