#include "rt.h"

#include <stdlib.h>
#include <unistd.h>

uint64_t rt_write_stdout(const uint8_t* ptr, uint64_t length) {
    if (ptr == NULL || length == 0) {
        return 0;
    }
    uint64_t written = 0;
    while (written < length) {
        ssize_t chunk = write(STDOUT_FILENO, ptr + written, (size_t)(length - written));
        if (chunk <= 0) {
            break;
        }
        written += (uint64_t)chunk;
    }
    return written;
}

void rt_exit(int64_t code) {
    exit((int)code);
}
