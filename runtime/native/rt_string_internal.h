#ifndef SURGE_RT_STRING_INTERNAL_H
#define SURGE_RT_STRING_INTERNAL_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

// The string representation the runtime's own string code shares.
//
// It is internal on purpose: a Surge string is a handle to this header plus
// its bytes, and nothing outside runtime/native should depend on that shape.

typedef struct SurgeString {
    uint64_t len_cp;
    uint64_t len_bytes;
    uint8_t data[];
} SurgeString;

// Borrow a string's bytes without copying. Answers false for a null handle.
bool string_span(void* s, const char** out_ptr, size_t* out_len);

// Narrow [start, end) past leading and trailing ASCII whitespace.
void trim_span(const char* data, size_t len, size_t* start, size_t* end);

#endif // SURGE_RT_STRING_INTERNAL_H
