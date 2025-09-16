#ifndef SURGE_DIAGNOSTICS_H
#define SURGE_DIAGNOSTICS_H

#include <stddef.h>
#include <stdarg.h>

// Source position (1-based line/col)
typedef struct SurgeSrcPos {
    const char *file;
    size_t line;
    size_t col;
} SurgeSrcPos;

// Simple diagnostic printer (stderr)
void surge_diag_errorf(SurgeSrcPos pos, const char *fmt, ...);
void surge_diag_warningf(SurgeSrcPos pos, const char *fmt, ...);

#endif // SURGE_DIAGNOSTICS_H