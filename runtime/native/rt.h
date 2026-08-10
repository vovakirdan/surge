#ifndef SURGE_RUNTIME_NATIVE_RT_H
#define SURGE_RUNTIME_NATIVE_RT_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "rt_typed_carrier_abi.generated.h"

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
// Joins two arrays into a third that owns its own buffer. `clone_elem`
// duplicates one element slot (dst, src) for an element type that owns
// heap; NULL means the bytes are the whole value and are moved as bytes.
void* rt_array_concat(void* left_slot,
                      void* right_slot,
                      uint64_t elem_stride,
                      uint64_t elem_align,
                      void (*clone_elem)(void*, const void*));
void* rt_array_slice(void* array_slot, void* r, uint64_t elem_stride);
void* rt_array_slice_fixed(void* elems, void* r, uint64_t length, uint64_t elem_stride);
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
/* These two reporters take the source location as a trailing (pointer, length)
 * pair, which the compiler fills in from the instruction that raised the panic:
 * a compiled binary has no frame to ask at run time, so the location has to be
 * baked in where it is still known. A NULL pointer means the caller has no
 * location to give — which is every call from inside this runtime — and then no
 * location line is printed at all. rt_panic above is deliberately not one of
 * them: the VM answers the same panic without naming a location. */
/* Reports under a code the caller names; rt_panic_numeric is this with
 * "VM3202" bound, which is what a failed numeric conversion is. Every other
 * condition names its own, so the two backends cannot disagree about it. */
void rt_panic_code(const uint8_t* code,
                   uint64_t code_length,
                   const uint8_t* ptr,
                   uint64_t length,
                   const uint8_t* span,
                   uint64_t span_length);
void rt_panic_numeric(const uint8_t* ptr,
                      uint64_t length,
                      const uint8_t* span,
                      uint64_t span_length);
void rt_panic_bounds(
    uint64_t kind, int64_t index, int64_t length, const uint8_t* span, uint64_t span_length);
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

// Which shape a Range<T> object is. Both shapes are reached through the same
// language type, so the byte is what tells an iteration step whether it is
// looking at a pair of bounds or at a cursor walking an array's elements.
#define SURGE_RANGE_KIND_BOUNDS 0
#define SURGE_RANGE_KIND_ARRAY_ITER 1

typedef struct SurgeRange {
    void* start;
    void* end;
    uint8_t has_start;
    uint8_t has_end;
    uint8_t inclusive;
    // SURGE_RANGE_KIND_*. The constructors below build bounds; the compiler
    // builds the array cursor for `arr.__range()` and for a loop over an array,
    // reusing this header and keeping its two bound flags clear. The slice
    // helpers in rt_array.c and rt_string.c consult those flags before they
    // read start or end, so a cursor reaching one of them reads as an unbounded
    // range rather than as a pair of bounds it never had.
    uint8_t kind;
    uint8_t _pad[4];
} SurgeRange;

void* rt_string_from_bytes(const uint8_t* ptr, uint64_t len);
// Drop-emission reclamation: frees one owned string (unconditional; every
// string value is a single heap allocation).
void rt_string_free(void* handle);
void* rt_string_clone(void* handle);
bool rt_utf8_valid(const uint8_t* ptr, uint64_t len);
// A no-op here: this runtime has no rope, so every string is already one
// contiguous run. See the definition for why it still exists.
void rt_string_force_flatten(void* s);
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
// Deep-copy a heap bigfloat (WidthAny `float`). NULL-safe: NULL is the zero
// float and needs no allocation. The copy starts at count 1 and shares nothing
// with its source, which is what makes it usable as the deep copy a crossing
// installs at a shard boundary.
void* rt_bigfloat_clone(const void* a);

// Destroy a bigfloat block unconditionally, IGNORING its count. This is the
// zero-count tail of a release, not an ownership operation: the runtime's own
// arithmetic uses it on temporaries it exclusively owns, and the compiled
// release path calls it once the count reaches zero. Compiled code must not
// call it to give up a reference — that is what rt_bigfloat_release is for.
void rt_bigfloat_free(void* a);

// Ownership operations on a reference-counted bigfloat. Both are NULL-safe.
// The count is NON-ATOMIC, so these are sound only while a block stays within
// one shard; every crossing deep-copies at the boundary to keep that true.
//
// The LLVM backend inlines both as IR at the use site rather than calling
// these, so that a float copy costs a predictable not-taken branch instead of
// a call (the whole point of counting rather than cloning). These entry points
// are the reference semantics and the out-of-line form.
void rt_bigfloat_retain(void* a);
void rt_bigfloat_release(void* a);
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
// Builds a second, independently owned value out of a task result the task is
// still holding, and answers with its bits. Generated per result type, because
// what an independent copy costs is what the type owns.
typedef void* (*rt_result_copy_fn)(void*);
// `copy_result` and `result_drop_fn_id` describe how the task must serve a
// result now that more than one handle can ask for it: the copy every asker
// gets, and the release of the one original the task keeps. Both are 0/NULL for
// a result whose bits own nothing and travel in nothing, which any number of
// askers can simply read again.
void* rt_task_clone(void* task, rt_result_copy_fn copy_result, uint64_t result_drop_fn_id);
void* rt_blocking_submit(uint64_t fn_id, void* state, uint64_t state_size, uint64_t state_align);
uint8_t rt_timeout_poll(void* task, uint64_t ms, uint64_t* out_bits);
int64_t rt_select_poll_tasks(uint64_t count, void** tasks, int64_t default_index);
int64_t rt_select_poll(uint64_t count,
                       const uint8_t* kinds,
                       void** handles,
                       const uint64_t* values,
                       const uint64_t* ms,
                       int64_t default_index);
void rt_async_yield(void* state, uint64_t state_drop_fn_id);
void rt_async_return(void* state, uint64_t bits);
void rt_async_return_cancelled(void* state, uint64_t state_drop_fn_id);

void* rt_channel_new(uint64_t capacity, uint64_t payload_drop_fn_id);
bool rt_channel_send(void* channel, uint64_t value_bits);
bool rt_channel_send_yield(void* channel, uint64_t value_bits);
uint8_t rt_channel_recv(void* channel, uint64_t* out_bits);
void rt_channel_send_blocking(void* channel, uint64_t value_bits);
uint8_t rt_channel_recv_blocking(void* channel, uint64_t* out_bits);
bool rt_channel_try_send(void* channel, uint64_t value_bits);
bool rt_channel_try_recv(void* channel, uint64_t* out_bits);
void rt_channel_close(void* channel);
// Reclaims a channel object's memory (header + inline buffer, one
// allocation), draining every still-buffered entry through the channel's
// own payload_drop_fn_id first (a no-op for Copy/inert elements). Callers
// must already know no other holder can reach this channel — never call
// this on a channel another live handle can still resolve.
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
