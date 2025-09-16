#include "diagnostics.h"
#include <stdio.h>
#include <stdarg.h>

static void vprint_diag(const char *level, SurgeSrcPos pos, const char *fmt, va_list ap) {
    if (pos.file) {
        fprintf(stderr, "%s:%zu:%zu: %s: ", pos.file, pos.line, pos.col, level);
    } else {
        fprintf(stderr, "%zu:%zu: %s: ", pos.line, pos.col, level);
    }
    vfprintf(stderr, fmt, ap);
    fputc('\n', stderr);
}

void surge_diag_errorf(SurgeSrcPos pos, const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    vprint_diag("error", pos, fmt, ap);
    va_end(ap);
}

void surge_diag_warningf(SurgeSrcPos pos, const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    vprint_diag("warning", pos, fmt, ap);
    va_end(ap);
}