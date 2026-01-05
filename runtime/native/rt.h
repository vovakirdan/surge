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
void* rt_readline(void);
void rt_exit(int64_t code);
void rt_panic(const uint8_t* ptr, uint64_t length);
void rt_panic_numeric(const uint8_t* ptr, uint64_t length);
void rt_panic_bounds(uint64_t kind, int64_t index, int64_t length);

void* rt_argv(void);
void* rt_stdin_read_all(void);

typedef struct SurgeRange {
    void* start;
    void* end;
    uint8_t has_start;
    uint8_t has_end;
    uint8_t inclusive;
    uint8_t _pad[5];
} SurgeRange;

void* rt_string_from_bytes(const uint8_t* ptr, uint64_t len);
const uint8_t* rt_string_ptr(void* s);
uint64_t rt_string_len(void* s);
uint64_t rt_string_len_bytes(void* s);
uint32_t rt_string_index(void* s, int64_t index);
void* rt_string_slice(void* s, void* r);
void* rt_string_bytes_view(void* s);
void* rt_string_concat(void* a, void* b);
void* rt_string_repeat(void* s, int64_t count);
bool rt_string_eq(void* a, void* b);
void* rt_string_from_int(int64_t value);
void* rt_string_from_uint(uint64_t value);
void* rt_string_from_float(double value);
void* rt_string_from_bigint(void* value);
void* rt_string_from_biguint(void* value);
void* rt_string_from_bigfloat(void* value);
bool rt_parse_int(void* s, int64_t* out);
bool rt_parse_uint(void* s, uint64_t* out);
bool rt_parse_float(void* s, double* out);
bool rt_parse_bool(void* s, uint8_t* out);
bool rt_parse_bigint(void* s, void** out);
bool rt_parse_biguint(void* s, void** out);
bool rt_parse_bigfloat(void* s, void** out);

void* rt_bigint_from_literal(const uint8_t* ptr, uint64_t len);
void* rt_biguint_from_literal(const uint8_t* ptr, uint64_t len);
void* rt_bigfloat_from_literal(const uint8_t* ptr, uint64_t len);
void* rt_bigint_from_i64(int64_t value);
void* rt_bigint_from_u64(uint64_t value);
void* rt_biguint_from_u64(uint64_t value);
void* rt_bigfloat_from_i64(int64_t value);
void* rt_bigfloat_from_u64(uint64_t value);
void* rt_bigfloat_from_f64(double value);
bool rt_bigint_to_i64(void* v, int64_t* out);
bool rt_biguint_to_u64(void* v, uint64_t* out);
bool rt_bigfloat_to_f64(void* v, double* out);
void* rt_bigint_add(void* a, void* b);
void* rt_bigint_sub(void* a, void* b);
void* rt_bigint_mul(void* a, void* b);
void* rt_bigint_div(void* a, void* b);
void* rt_bigint_mod(void* a, void* b);
void* rt_bigint_neg(void* a);
void* rt_bigint_abs(void* a);
int32_t rt_bigint_cmp(void* a, void* b);
void* rt_bigint_bit_and(void* a, void* b);
void* rt_bigint_bit_or(void* a, void* b);
void* rt_bigint_bit_xor(void* a, void* b);
void* rt_bigint_shl(void* a, void* b);
void* rt_bigint_shr(void* a, void* b);
void* rt_biguint_add(void* a, void* b);
void* rt_biguint_sub(void* a, void* b);
void* rt_biguint_mul(void* a, void* b);
void* rt_biguint_div(void* a, void* b);
void* rt_biguint_mod(void* a, void* b);
int32_t rt_biguint_cmp(void* a, void* b);
void* rt_biguint_bit_and(void* a, void* b);
void* rt_biguint_bit_or(void* a, void* b);
void* rt_biguint_bit_xor(void* a, void* b);
void* rt_biguint_shl(void* a, void* b);
void* rt_biguint_shr(void* a, void* b);
void* rt_bigfloat_add(void* a, void* b);
void* rt_bigfloat_sub(void* a, void* b);
void* rt_bigfloat_mul(void* a, void* b);
void* rt_bigfloat_div(void* a, void* b);
void* rt_bigfloat_mod(void* a, void* b);
void* rt_bigfloat_neg(void* a);
void* rt_bigfloat_abs(void* a);
int32_t rt_bigfloat_cmp(void* a, void* b);
void* rt_bigint_to_biguint(void* a);
void* rt_biguint_to_bigint(const void* a);
void* rt_bigint_to_bigfloat(void* a);
void* rt_biguint_to_bigfloat(void* a);
void* rt_bigfloat_to_bigint(void* a);
void* rt_bigfloat_to_biguint(void* a);

void* __task_create(
    uint64_t poll_fn_id,
    void* state);         // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void* __task_state(void); // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void rt_task_wake(void* task);
uint8_t rt_task_poll(void* task, uint64_t* out_bits);
void rt_task_await(void* task, uint8_t* out_kind, uint64_t* out_bits);
void rt_task_cancel(void* task);
void* rt_task_clone(void* task);
uint8_t rt_timeout_poll(void* task, uint64_t ms, uint64_t* out_bits);
int64_t rt_select_poll_tasks(uint64_t count, void** tasks, int64_t default_index);
void rt_async_yield(void* state);
void rt_async_return(void* state, uint64_t bits);
void rt_async_return_cancelled(void* state);

void* rt_channel_new(uint64_t capacity);
bool rt_channel_send(void* channel, uint64_t value_bits);
uint8_t rt_channel_recv(void* channel, uint64_t* out_bits);
bool rt_channel_try_send(void* channel, uint64_t value_bits);
bool rt_channel_try_recv(void* channel, uint64_t* out_bits);
void rt_channel_close(void* channel);

void* rt_scope_enter(bool failfast);
void rt_scope_register_child(void* scope, void* task);
void rt_scope_cancel_all(void* scope);
bool rt_scope_join_all(void* scope, uint64_t* pending, bool* failfast);
void rt_scope_exit(void* scope);

void* checkpoint(void);
void* rt_sleep(uint64_t ms);

void* rt_range_int_new(void* start, void* end, bool inclusive);
void* rt_range_int_from_start(void* start, bool inclusive);
void* rt_range_int_to_end(void* end, bool inclusive);
void* rt_range_int_full(bool inclusive);

#ifdef __cplusplus
}
#endif

#endif
