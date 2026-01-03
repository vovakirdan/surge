#ifndef SURGE_RUNTIME_NATIVE_RT_H
#define SURGE_RUNTIME_NATIVE_RT_H

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
void rt_exit(int64_t code);

void* rt_string_from_bytes(const uint8_t* ptr, uint64_t len);
uint8_t* rt_string_ptr(void* s);
uint64_t rt_string_len(void* s);
uint64_t rt_string_len_bytes(void* s);

#ifdef __cplusplus
}
#endif

#endif
