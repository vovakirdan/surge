#include "rt.h"

#include <errno.h>
#include <unistd.h>

static void fatal_write_all(const uint8_t* bytes, uint64_t length) {
    while (bytes != NULL && length > 0) {
        // A small fixed chunk stays below every write(2) implementation's
        // signed count limit, including 32-bit targets.
        size_t chunk = length > 4096 ? 4096 : (size_t)length;
        ssize_t written = write(STDERR_FILENO, bytes, chunk);
        if (written > 0) {
            bytes += (size_t)written;
            length -= (uint64_t)written;
            continue;
        }
        if (written < 0 && errno == EINTR) {
            continue;
        }
        break;
    }
}

_Noreturn void
rt_fatal_static(rt_fatal_code code, const uint8_t* message, uint64_t message_length) {
    static const uint8_t prefix[] = "surge: fatal [";
    static const uint8_t separator[] = "]: ";
    static const uint8_t newline[] = "\n";
    static const uint8_t panic_code[] = "PANIC";
    static const uint8_t oom_code[] = "RT_OOM";
    static const uint8_t trap_code[] = "RT_TRAP";

    // An invalid selector is an internal reporter protocol violation, so it is
    // classified as the trap family rather than silently called a panic.
    const uint8_t* code_bytes = trap_code;
    uint64_t code_length = sizeof(trap_code) - 1;
    if (code == RT_FATAL_PANIC) {
        code_bytes = panic_code;
        code_length = sizeof(panic_code) - 1;
    } else if (code == RT_OOM) {
        code_bytes = oom_code;
        code_length = sizeof(oom_code) - 1;
    }

    fatal_write_all(prefix, sizeof(prefix) - 1);
    fatal_write_all(code_bytes, code_length);
    fatal_write_all(separator, sizeof(separator) - 1);
    fatal_write_all(message, message_length);
    if (message == NULL || message_length == 0 || message[message_length - 1] != '\n') {
        fatal_write_all(newline, sizeof(newline) - 1);
    }
    _exit(1);
}
