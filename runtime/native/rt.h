#ifndef SURGE_RUNTIME_NATIVE_RT_H
#define SURGE_RUNTIME_NATIVE_RT_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "rt_typed_carrier_abi.generated.h"

#ifdef __cplusplus
#define SURGE_RT_NORETURN [[noreturn]]
#define SURGE_RT_STATIC_ASSERT static_assert
#else
#define SURGE_RT_NORETURN _Noreturn
#define SURGE_RT_STATIC_ASSERT _Static_assert
#endif

#ifdef __cplusplus
extern "C" {
#endif

void* rt_alloc(uint64_t size, uint64_t align);
// rt_alloc with the refusal reported here: a block that cannot be had ends
// the process with `message` as the RT_OOM report, so the caller never sees
// NULL. For the entry points whose answer generated code stores untested.
void* rt_alloc_or_report(uint64_t size,
                         uint64_t align,
                         const uint8_t* message,
                         uint64_t message_length);
#ifdef RT_TEST_SYNC_POINTS
// Test seam, stands only: while positive, each rt_alloc decrements it and
// answers NULL, so a refusal can be forced on an exact site.
extern _Atomic uint32_t rt_test_alloc_refusals;
#endif
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
// rt_tag_alloc with the refusal reported here (see rt_alloc_or_report).
void* rt_tag_alloc_or_report(uint32_t tag,
                             size_t payload_align,
                             size_t payload_size,
                             const uint8_t* message,
                             uint64_t message_length);

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

// Process-terminal failures that are not language panics. The explicit-width
// typedef is part of the compiler/runtime ABI: LLVM declares the first
// parameter as i32, independently of a C compiler's enum representation.
typedef uint32_t rt_fatal_code;
enum {
    RT_FATAL_PANIC = 0,
    RT_OOM = 1,
    RT_TRAP = 2,
};

// Writes one fixed-shape fatal report and terminates without allocating.
// The message is a borrowed byte span and need not be NUL-terminated.
SURGE_RT_NORETURN void
rt_fatal_static(rt_fatal_code code, const uint8_t* message, uint64_t message_length);
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
/* Prints the frames beneath the current one, in the shape the VM prints them.
 * `site` is the location the emitter already knew for the innermost frame; a
 * panic raised inside this runtime passes NULL and the innermost Surge frame's
 * own row answers instead. Costs nothing until it is called. */
void rt_panic_write_where(const uint8_t* site, uint64_t site_length);
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

// The array cursor, spelled out on this side because C already depends on it:
// the slice helpers read `kind`, `has_start` and `has_end` out of a cursor, so
// the two shapes were already sharing a layout that only the LLVM emitter
// described. Writing it here lets `rt_range_free` size a cursor with `sizeof`
// instead of a literal.
//
// The assertions below catch drift on THIS side only - a C compiler cannot see
// a Go constant. The other side is pinned by
// `internal/backend/llvm/range_layout_test.go`, which holds the emitter's
// constants against the same numbers. Both halves are needed: a mismatch is not
// a compile error but a heap-accounting corruption, because rt_alloc and rt_free
// reconcile the size they are told rather than measuring the block.
typedef struct SurgeRangeArrayIter {
    SurgeRange header; // start = element data, end = element stride
    uint64_t index;
    uint64_t length;
} SurgeRangeArrayIter;

SURGE_RT_STATIC_ASSERT(sizeof(SurgeRange) == 24,
                       "SurgeRange must stay 24 bytes: the emitter allocates that");
SURGE_RT_STATIC_ASSERT(sizeof(SurgeRangeArrayIter) == 40,
                       "the array cursor must stay 40 bytes: the emitter allocates that");
SURGE_RT_STATIC_ASSERT(offsetof(SurgeRangeArrayIter, index) == 24,
                       "cursor index offset must match the emitter");
SURGE_RT_STATIC_ASSERT(offsetof(SurgeRangeArrayIter, length) == 32,
                       "cursor length offset must match the emitter");

// Reclaims ONE Range object, of either shape, sizing it off its own kind byte.
// Null-safe: a released slot is nulled and a second release must not read a
// kind byte out of nothing.
//
// It does NOT release the bounds. `start`/`end` are words that are either
// fixnum-tagged integers, which own nothing, or heap bignums, which have no
// exported lifecycle in this runtime at all - `bi_free` is a static inline in
// rt_bignum_internal.h and only `float` is reference counted. There is nothing
// this function could legally call, so a bignum-bounded range still leaks its
// two bound boxes and that belongs to the bignum-lifecycle debt rather than
// here.
void rt_range_free(void* handle);

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

// Creates a task, with the descriptor for the result it will produce. A NULL
// descriptor is a task with no result value, which is a shape and not an
// omission: the slot stays empty and rt_async_return refuses to publish into
// it. Everything else about the result -- how wide it is, how it is destroyed,
// how an independent copy of one is made -- the descriptor already knows.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void* __task_create(uint64_t poll_fn_id, void* state, const rt_value_ops* result_ops);
// The same constructor for a task that BORROWS its creator's frame: the task
// is pinned to the worker carrying the creator before it is published, so it
// only ever runs where the borrowed frame is. Creation is the only point that
// knows the carrier, because creation is a synchronous action of the running
// parent and publishes the task at once -- a pin at the spawn's wake would
// come after the first publication.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void* __task_create_affine(uint64_t poll_fn_id, void* state, const rt_value_ops* result_ops);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void* __task_state(void);
void rt_task_wake(void* task);
// The result is moved into `out_dst`, which the caller sizes from the result's
// own type. Nothing is boxed to fit a machine word on the way, which is what
// these two carried before.
uint8_t rt_task_poll(void* task, void* out_dst);
void rt_task_await(void* task, uint8_t* out_kind, void* out_dst);
void rt_task_cancel(void* task);
// A second handle on the same task, and therefore a second asker for the same
// result. It takes no description of how to serve one: the result's descriptor
// says how a value of that type is duplicated and destroyed, and the task holds
// it. What a clone changes is how MANY askers there can be, which is the task's
// own bookkeeping.
// `duplicate` is how a SECOND asker is served: the per-type duplication for the
// result this handle can now be asked for twice, or NULL when the type needs
// none (a result that owns nothing is served by copying its bytes) or has none
// (a buffer the runtime cannot duplicate, which refuses a second asker).
//
// It rides with the clone rather than with the descriptor because the CLONE is
// the operation that took the obligation on: an un-cloned task is asked once
// and moves its result out. What may duplicate a value, and for whom, is the
// entitlement question D4b answers.
void* rt_task_clone(void* task, rt_value_clone_init_fn duplicate);
// A handle the program will never ask through again: an abandoned frame, a
// container torn down by cancellation, shutdown. It gives back this handle's
// reference and nothing else -- the task keeps running if it has not finished,
// and cancelling it stays a separate, task-global operation. When this was the
// last handle on a DONE task, the task and the result nobody took are freed.
void rt_task_handle_drop(void* task);
// `state_type_id` names the type of the captures `state` points at, so the job
// destroys them through their own descriptor instead of freeing the block and
// abandoning whatever was inside it. `result_type_id` names the blocking body's
// result type, so the job and the awaiting task bind the SAME descriptor and
// the value moves between them.
void* rt_blocking_submit(uint64_t fn_id,
                         void* state,
                         uint64_t state_type_id,
                         uint64_t result_type_id);
// `out_dst` is the caller's storage for the result, sized from its own type:
// a timeout poll takes the value out of the same slot an await does.
uint8_t rt_timeout_poll(void* task, uint64_t ms, void* out_dst);
int64_t rt_select_poll_tasks(uint64_t count, void** tasks, int64_t default_index);
int64_t rt_select_poll(uint64_t count,
                       const uint8_t* kinds,
                       void** handles,
                       void* const* values,
                       const uint64_t* ms,
                       int64_t default_index);
void rt_async_yield(void* state, uint64_t state_type_id);
// Completes the current task, moving the value at `src` into the task's own
// result slot. NULL src is a task that produces no value.
void rt_async_return(void* state, void* src);
void rt_async_return_cancelled(void* state, uint64_t state_type_id);

void* rt_channel_new(uint64_t capacity, const rt_value_ops* ops, uint64_t element_type_id);
const rt_value_ops* rt_channel_opaque_word_ops(void);
bool rt_channel_send(void* channel, void* src);
bool rt_channel_send_yield(void* channel, void* src);
uint8_t rt_channel_recv(void* channel, void* dst);
void rt_channel_send_blocking(void* channel, void* src);
uint8_t rt_channel_recv_blocking(void* channel, void* dst);
bool rt_channel_try_send(void* channel, void* src);
bool rt_channel_try_recv(void* channel, void* dst);
void rt_channel_close(void* channel);
// One more copy of a channel handle exists, and one fewer. `Channel<T>` is a
// copyable handle at the language surface, so copying one retains, dropping a
// copy releases, and the last release destroys the object -- which drops every
// payload the channel still owns, because a channel is not a place values go
// to be forgotten. NULL is a no-op at both entries: a container slot the
// handle was moved out of holds NULL and the container's glue still visits it.
void rt_channel_handle_retain(void* channel);
void rt_channel_handle_drop(void* channel);
// Reclaims a channel object's memory (header + inline buffer, one allocation),
// destroying everything it still holds first: the buffered values and whatever
// a park slot was left holding, each exactly once through the element's own
// drop.
//
// Callers must already know no other holder can reach this channel — never
// call this on a channel another live handle, waiter, subscription or
// in-flight operation can still resolve. It does not take that on trust: it
// refuses, naming what it found, and the ordinary way to reach it is to drop
// the last handle rather than to call it.
//
// Takes the channel owner's shard lock for the detaching half of its teardown,
// so it must be called with NO scheduler lock held. Callers that cannot
// promise that go through rt_channel_free_when_unlocked instead.
void rt_channel_free(void* channel);

// A map's keys and values live in exact typed storage, so every entry point
// here takes an ADDRESS: `key` and `value` address storage of the map's own key
// and value type, never a machine word standing in for one. The two descriptors
// are what tell the map how wide an entry is and how to move one, so they are
// given once, at construction.
//
// Ownership: `rt_map_insert` takes the key and the value it is handed;
// `rt_map_remove` moves the value out into `removed` and destroys the stored
// key. `previous` and `removed` are optional -- NULL means the caller does not
// want the displaced or removed value, and the map destroys it rather than
// abandoning it. Lookup transfers nothing: it writes the value's interior
// ADDRESS into `out_value`, and that address does not survive this map's next
// growth or removal.
void* rt_map_new(uint64_t key_kind, const rt_value_ops* key_ops, const rt_value_ops* value_ops);
uint64_t rt_map_len(const void* map);
bool rt_map_contains(const void* map, const void* key);
bool rt_map_get_ref(void* map, const void* key, void** out_value);
bool rt_map_get_mut(void* map, const void* key, void** out_value);
bool rt_map_insert(void* map, void* key, void* value, void* previous);
bool rt_map_remove(void* map, const void* key, void* removed);
// Answers with an INDEPENDENT owning array of the keys. `duplicate` is the
// compiler's recipe for giving the array its own copy of one key; NULL means
// the key carries no obligation and its bytes are the whole value. It is the
// call site's recipe and not the key descriptor's clone, because duplicating a
// key here is an obligation this operation takes on rather than a property of
// the key type.
void* rt_map_keys(const void* map,
                  uint64_t elem_size,
                  uint64_t elem_align,
                  rt_value_clone_init_fn duplicate);
// Reclaims a map: destroys every live key and value through the map's own two
// descriptors, then the entry storage, then the header. Callers must already
// know no other holder can reach this map -- it is reached from generated drop
// glue, where the language has proven exactly that. A null handle is a dropped
// slot that never held one, and is not an error.
void rt_map_free(void* map);

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

#undef SURGE_RT_STATIC_ASSERT
#undef SURGE_RT_NORETURN

#endif
