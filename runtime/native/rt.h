#ifndef SURGE_RUNTIME_NATIVE_RT_H
#define SURGE_RUNTIME_NATIVE_RT_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

void* rt_alloc(uint64_t size, uint64_t align);
void rt_free(uint8_t* ptr, uint64_t size, uint64_t align);
void* rt_realloc(uint8_t* ptr, uint64_t old_size, uint64_t new_size, uint64_t align);
void rt_memcpy(uint8_t* dst, const uint8_t* src, uint64_t n);
void rt_memmove(uint8_t* dst, const uint8_t* src, uint64_t n);
void rt_array_forget_allocation(const void* ptr);
bool rt_array_is_view(const void* header);
// Drop-emission reclamation: drops one owned array value. Views free their
// header and unlink; a base with live views defers header+data until the
// last view drops; the element layout describes the dropped handle.
void rt_array_free(void* array_header, uint64_t elem_stride, uint64_t elem_align);
void rt_array_free_elems(void* array_header,
                         uint64_t elem_stride,
                         uint64_t elem_align,
                         void (*drop_elem)(void*));
// Debug observability for the deferred-reclamation float.
uint64_t rt_array_debug_deferred_base_drops(void);
void* rt_array_slice(void* array_slot, void* r, uint64_t elem_stride);
void* rt_array_slice_fixed(void* data_slot, void* r, uint64_t length, uint64_t elem_stride);
void rt_array_sync_views(void* array_header);
void rt_array_append_raw_bytes(void* array_slot, const uint8_t* src, uint64_t len);
void rt_byte_array_append_range(void* dst_slot,
                                const void* src_array,
                                uint64_t start,
                                uint64_t len);
void rt_byte_array_drop_prefix(void* array_slot, uint64_t count);
bool rt_byte_parse_uint64_token(
    const void* array, uint64_t start, uint64_t end, uint64_t* value_out, uint64_t* next_out);
size_t rt_tag_payload_offset(size_t payload_align);
void* rt_tag_alloc(uint32_t tag, size_t payload_align, size_t payload_size);

uint64_t rt_write_stdout(const uint8_t* ptr, uint64_t length);
uint64_t rt_write_stderr(const uint8_t* ptr, uint64_t length);
void* rt_entropy_bytes(uint64_t len);
void rt_term_enter_alt_screen(void);
void rt_term_exit_alt_screen(void);
void rt_term_set_raw_mode(bool enabled);
void rt_term_hide_cursor(void);
void rt_term_show_cursor(void);
void* rt_term_size(void);
void rt_term_write(const void* bytes);
void rt_term_flush(void);
void* rt_term_read_event(void);
void* rt_readline(void);
void rt_exit(int64_t code);
void rt_panic(const uint8_t* ptr, uint64_t length);
void rt_panic_numeric(const uint8_t* ptr, uint64_t length);
void rt_panic_bounds(uint64_t kind, int64_t index, int64_t length);
int64_t rt_monotonic_now(void);
uint64_t rt_worker_count(void);
void* rt_heap_stats(void);
void rt_exec_trace_dump(void);
void rt_sched_trace_dump(void);

void* rt_argv(void);
void* rt_stdin_read_all(void);

void* rt_fs_cwd(void);
void* rt_fs_metadata(void* path);
void* rt_fs_read_dir(void* path);
void* rt_fs_mkdir(void* path, bool recursive);
void* rt_fs_remove_file(void* path);
void* rt_fs_remove_dir(void* path, bool recursive);
void* rt_fs_open(void* path, uint32_t flags);
void* rt_fs_close(void* file);
void* rt_fs_read(void* file, uint8_t* buf, uint64_t cap);
void* rt_fs_write(void* file, const uint8_t* buf, uint64_t len);
void* rt_fs_seek(void* file, int64_t offset, int64_t whence);
void* rt_fs_flush(void* file);
void* rt_fs_read_file(void* path);
void* rt_fs_write_file(void* path, const uint8_t* data, uint64_t len, uint32_t flags);
void* rt_fs_file_name(const void* file);
void* rt_fs_file_type(const void* file);
void* rt_fs_file_metadata(void* file);

void* rt_net_listen(void* addr, uint64_t port);
void* rt_net_connect(void* addr, uint64_t port);
void* rt_net_close_listener(void* listener);
void* rt_net_close_conn(void* conn);
void* rt_net_accept(const void* listener);
void* rt_net_read(const void* conn, uint8_t* buf, uint64_t cap);
void* rt_net_write(const void* conn, const uint8_t* buf, uint64_t len);
void* rt_net_read_bytes(const void* conn, uint64_t cap);
void* rt_net_write_bytes(const void* conn, const void* bytes, uint64_t offset, uint64_t len);
bool rt_net_wait_accept(const void* listener);
bool rt_net_wait_readable(const void* conn);
bool rt_net_wait_writable(const void* conn);

typedef struct SurgeRange {
    void* start;
    void* end;
    uint8_t has_start;
    uint8_t has_end;
    uint8_t inclusive;
    uint8_t _pad[5];
} SurgeRange;

void* rt_string_from_bytes(const uint8_t* ptr, uint64_t len);
// Drop-emission reclamation: frees one owned string (unconditional; every
// string value is a single heap allocation).
void rt_string_free(void* handle);
void* rt_string_clone(void* handle);
bool rt_utf8_valid(const uint8_t* ptr, uint64_t len);
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
void* rt_bigint_add(const void* a, const void* b);
void* rt_bigint_sub(const void* a, const void* b);
void* rt_bigint_mul(const void* a, const void* b);
void* rt_bigint_div(const void* a, const void* b);
void* rt_bigint_mod(const void* a, const void* b);
void* rt_bigint_neg(const void* a);
void* rt_bigint_abs(const void* a);
int32_t rt_bigint_cmp(const void* a, const void* b);
void* rt_bigint_bit_and(const void* a, const void* b);
void* rt_bigint_bit_or(const void* a, const void* b);
void* rt_bigint_bit_xor(const void* a, const void* b);
void* rt_bigint_shl(const void* a, const void* b);
void* rt_bigint_shr(const void* a, const void* b);
void* rt_biguint_add(const void* a, const void* b);
void* rt_biguint_sub(const void* a, const void* b);
void* rt_biguint_mul(const void* a, const void* b);
void* rt_biguint_div(const void* a, const void* b);
void* rt_biguint_mod(const void* a, const void* b);
int32_t rt_biguint_cmp(const void* a, const void* b);
void* rt_biguint_bit_and(const void* a, const void* b);
void* rt_biguint_bit_or(const void* a, const void* b);
void* rt_biguint_bit_xor(const void* a, const void* b);
void* rt_biguint_shl(const void* a, const void* b);
void* rt_biguint_shr(const void* a, const void* b);
void* rt_bigfloat_add(const void* a, const void* b);
void* rt_bigfloat_sub(const void* a, const void* b);
void* rt_bigfloat_mul(const void* a, const void* b);
void* rt_bigfloat_div(const void* a, const void* b);
void* rt_bigfloat_mod(const void* a, const void* b);
void* rt_bigfloat_neg(const void* a);
void* rt_bigfloat_abs(const void* a);
int32_t rt_bigfloat_cmp(const void* a, const void* b);
// Deep-copy / free a heap bigfloat (WidthAny `float`). Both are NULL-safe
// (NULL is the zero float and needs no allocation). Emitted by the compiler
// so a Copy-semantics float value is cloned when duplicated and freed on
// scope exit, matching the string/composite reclamation model.
void* rt_bigfloat_clone(const void* a);
void rt_bigfloat_free(void* a);
void* rt_bigint_to_biguint(const void* a);
void* rt_biguint_to_bigint(const void* a);
void* rt_bigint_to_bigfloat(const void* a);
void* rt_biguint_to_bigfloat(const void* a);
void* rt_bigfloat_to_bigint(const void* a);
void* rt_bigfloat_to_biguint(const void* a);

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void* __task_create(uint64_t poll_fn_id, void* state);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void* __task_state(void);
void rt_task_wake(void* task);
uint8_t rt_task_poll(void* task, uint64_t* out_bits);
void rt_task_await(void* task, uint8_t* out_kind, uint64_t* out_bits);
void rt_task_cancel(void* task);
void* rt_task_clone(void* task);
void* rt_blocking_submit(uint64_t fn_id, void* state, uint64_t state_size, uint64_t state_align);
uint8_t rt_timeout_poll(void* task, uint64_t ms, uint64_t* out_bits);
int64_t rt_select_poll_tasks(uint64_t count, void** tasks, int64_t default_index);
int64_t rt_select_poll(uint64_t count,
                       const uint8_t* kinds,
                       void** handles,
                       const uint64_t* values,
                       const uint64_t* ms,
                       int64_t default_index);
void rt_async_yield(void* state);
void rt_async_return(void* state, uint64_t bits);
void rt_async_return_cancelled(void* state);

void* rt_channel_new(uint64_t capacity);
bool rt_channel_send(void* channel, uint64_t value_bits);
bool rt_channel_send_yield(void* channel, uint64_t value_bits);
uint8_t rt_channel_recv(void* channel, uint64_t* out_bits);
void rt_channel_send_blocking(void* channel, uint64_t value_bits);
uint8_t rt_channel_recv_blocking(void* channel, uint64_t* out_bits);
bool rt_channel_try_send(void* channel, uint64_t value_bits);
bool rt_channel_try_recv(void* channel, uint64_t* out_bits);
void rt_channel_close(void* channel);
// Reclaims a channel object's memory (header + inline buffer, one
// allocation). Callers must already know no other holder can reach this
// channel: buffered Copy-payload bits need no separate release (raw bits
// own no heap state), but the caller is responsible for having drained
// any owned/heap-carrying buffered payloads first if the element type is
// not Copy (RV2-DEBT-048's residual, non-Copy buffer draining, is not
// handled here). Never call this on a channel another live handle can
// still resolve.
void rt_channel_free(void* channel);

void* rt_map_new(uint64_t key_kind);
uint64_t rt_map_len(const void* map);
bool rt_map_contains(const void* map, uint64_t key_bits);
bool rt_map_get_ref(void* map, uint64_t key_bits, uint64_t* out_bits);
bool rt_map_get_mut(void* map, uint64_t key_bits, uint64_t* out_bits);
bool rt_map_insert(void* map, uint64_t key_bits, uint64_t value_bits, uint64_t* out_prev);
bool rt_map_remove(void* map, uint64_t key_bits, uint64_t* out_prev);
void* rt_map_keys(const void* map, uint64_t elem_size, uint64_t elem_align);

void* rt_scope_enter(bool failfast);
void rt_scope_register_child(const void* scope, void* task);
void rt_scope_cancel_all(const void* scope);
bool rt_scope_join_all(const void* scope, uint64_t* pending, bool* failfast);
void rt_scope_exit(const void* scope);

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
