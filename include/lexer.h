#ifndef SURGE_LEXER_H
#define SURGE_LEXER_H

#include <stddef.h>
#include <stdbool.h>
#include "token.h"

// Lexer that owns a copy of the entire input buffer
typedef struct SurgeLexer {
    char   *buf;        // heap copy of source
    size_t  len;        // length in bytes
    size_t  idx;        // current index
    size_t  line;       // 1-based
    size_t  col;        // 1-based
    char   *file;       // optional copy of path (for diagnostics)
    bool    had_error;  // sticky error flag
} SurgeLexer;

// Initialize from C string (copies string); file is optional (can be NULL)
bool surge_lexer_init_from_string(SurgeLexer *lx, const char *source, const char *file_name);

// Initialize from file path
bool surge_lexer_init_from_file(SurgeLexer *lx, const char *path);

// Get next token. Caller must free with surge_token_free()
SurgeToken *surge_lexer_next(SurgeLexer *lx);

// Destroy lexer and free owned memory
void surge_lexer_destroy(SurgeLexer *lx);

#endif // SURGE_LEXER_H