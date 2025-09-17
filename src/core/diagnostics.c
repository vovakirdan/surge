#include "diagnostics.h"
#include <stdio.h>
#include <stdarg.h>
#include <string.h>

// Global (per-process) view of current source for context printing.
static const char *g_diag_file = NULL;
static const char *g_diag_src  = NULL;
static size_t      g_diag_len  = 0;

void surge_diag_set_source(const char *file, const char *src, size_t len) {
    g_diag_file = file;
    g_diag_src  = src;
    g_diag_len  = len;
}

static void vprint_header(const char *level, SurgeSrcPos pos, const char *fmt, va_list ap) {
    const char *fname = pos.file ? pos.file : (g_diag_file ? g_diag_file : "<input>");
    fprintf(stderr, "%s:%zu:%zu: %s: ", fname, pos.line, pos.col, level);
    vfprintf(stderr, fmt, ap);
    fputc('\n', stderr);
}

static void print_line_context(SurgeSrcPos pos) {
    if (!g_diag_src || g_diag_len == 0 || pos.line == 0) return;

    // Find start and end of the requested line (1-based).
    size_t line = 1;
    size_t i = 0, start = 0, end = 0;

    while (i < g_diag_len && line < pos.line) {
        if (g_diag_src[i++] == '\n') line++;
    }
    if (line != pos.line) return; // out of range
    start = i;
    while (i < g_diag_len && g_diag_src[i] != '\n' && g_diag_src[i] != '\r') i++;
    end = i;

    // Print the line exactly as-is (no ANSI, no tabs expansion on this line).
    // Add two spaces for readability.
    fputs("  ", stderr);
    fwrite(g_diag_src + start, 1, end - start, stderr);
    fputc('\n', stderr);

    // Print caret '^' under the column. We compute column as 1-based byte offset.
    // To align tabs reasonably, we expand '\t' to 4 spaces in the caret line only.
    fputs("  ", stderr);
    size_t col = pos.col ? pos.col : 1;
    size_t caret_pos = 1;
    for (size_t j = start; j < end && caret_pos < col; ++j) {
        char c = g_diag_src[j];
        if (c == '\t') {
            fputs("    ", stderr); // 4 spaces
            caret_pos++;
        } else {
            fputc(' ', stderr);
            caret_pos++;
        }
    }
    fputc('^', stderr);
    fputc('\n', stderr);
}

static void vprint_diag(const char *level, SurgeSrcPos pos, const char *fmt, va_list ap) {
    vprint_header(level, pos, fmt, ap);
    print_line_context(pos);
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
