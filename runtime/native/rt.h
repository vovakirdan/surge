#ifndef SURGE_RUNTIME_NATIVE_RT_H
#define SURGE_RUNTIME_NATIVE_RT_H

#include <stdbool.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

void* rt_alloc(uint64_t size, uint64_t align);
void rt_free(uint8_t* ptr, uint64_t size, uint64_t align);
void* rt_realloc(uint8_t* ptr, uint64_t old_size, uint64_t new_size, uint64_t align);
void rt_memcpy(uint8_t* dst, const uint8_t* src, uint64_t n);
void rt_memmove(uint8_t* dst, const uint8_t* src, uint64_t n);

uint64_t rt_write_stdout(const uint8_t* ptr, uint64_t length);
uint64_t rt_write_stderr(const uint8_t* ptr, uint64_t length);
void rt_exit(int64_t code);
void rt_panic(const uint8_t* ptr, uint64_t length);
void rt_panic_bounds(uint64_t kind, int64_t index, int64_t length);

void* rt_argv(void);
void* rt_stdin_read_all(void);

typedef struct SurgeRange {
    int64_t start;
    int64_t end;
    uint8_t has_start;
    uint8_t has_end;
    uint8_t inclusive;
    uint8_t _pad[5];
} SurgeRange;

void* rt_string_from_bytes(const uint8_t* ptr, uint64_t len);
uint8_t* rt_string_ptr(void* s);
uint64_t rt_string_len(void* s);
uint64_t rt_string_len_bytes(void* s);
uint32_t rt_string_index(void* s, int64_t index);
void* rt_string_slice(void* s, void* r);
void* rt_string_bytes_view(void* s);
void* rt_string_concat(void* a, void* b);
bool rt_string_eq(void* a, void* b);
void* rt_string_from_int(int64_t value);
void* rt_string_from_uint(uint64_t value);
void* rt_string_from_float(double value);
bool rt_parse_int(void* s, int64_t* out);
bool rt_parse_uint(void* s, uint64_t* out);
bool rt_parse_float(void* s, double* out);
bool rt_parse_bool(void* s, uint8_t* out);

void* rt_range_int_new(int64_t start, int64_t end, bool inclusive);
void* rt_range_int_from_start(int64_t start, bool inclusive);
void* rt_range_int_to_end(int64_t end, bool inclusive);
void* rt_range_int_full(bool inclusive);

#ifdef __cplusplus
}
#endif

#endif
