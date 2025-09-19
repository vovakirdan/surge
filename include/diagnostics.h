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

/**
 * Attach current source to diagnostics to enable line context and carets.
 * 'file' may be NULL; 'src' may be NULL (then no context is printed).
 * The pointers must remain valid while diagnostics are used (no copy).
 */
 void surge_diag_set_source(const char *file, const char *src, size_t len);

// Simple diagnostic printer (stderr)
void surge_diag_errorf(SurgeSrcPos pos, const char *fmt, ...);
void surge_diag_warningf(SurgeSrcPos pos, const char *fmt, ...);

#endif // SURGE_DIAGNOSTICS_H
