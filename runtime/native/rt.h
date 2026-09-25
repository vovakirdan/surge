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
// Relinquish-emission barrier for an OWNED dynamic array about to leave its
// shard: hands every element slot (data + i * stride, i < len) to `walk`,
// which makes that one element's counted leaves private, with no lock held.
// `array_slot` addresses the slot holding the header, as rt_array_concat's
// slots do; a NULL slot or a NULL header walks nothing. A view, or a base
// some view still reads, is refused by name (VM1003): their slots are read by
// a holder on the origin shard, and no in-place rewrite can make those
// private to the destination. RV2_ARRAY_UNSHARE_WALK_NEGATIVE_CONTROL cuts the
// refusal and never the walk. Defined in rt_array_reclaim.c.
void rt_array_unshare_walk(void* array_slot, uint64_t elem_stride, void (*walk)(void*));
// Debug observability for the deferred-reclamation float.
uint64_t rt_array_debug_deferred_base_drops(void);
// Debug barrier for an observer of a process-wide counter: returns once no
// task but the caller's own is running, nothing is runnable or being
// published on any shard, no inbound envelope is undrained and the blocking
// pool is idle; answers the number of samples it took; fails closed after a
// bounded wait (rt_debug_quiesce.c).
uint64_t rt_debug_quiesce(void);
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

// Which KIND the two bound words hold, for the bounds shape. One constructor
// family serves every element type -- the bounds arrive as bare void* -- so
// without this byte nothing in the object says whether a word is a SurgeBigInt,
// a SurgeBigUint or a SurgeBigFloat, whose counts sit at three different
// offsets (8, 4 and 0). Every reader of a bound word needs the answer: a
// release has to pick a lifecycle entry point, and a relinquishing walk has to
// pick an unshare.
//
// INT is 0 so that it is also what a forgotten write leaves behind. That is the
// honest default rather than the safe-looking one: the language's range-literal
// constructors are declared `-> Range<int>`, and `int`'s entry points test the
// fixnum tag before any load, so a bound that is a small integer is not touched
// at all.
#define SURGE_RANGE_BOUND_INT 0
#define SURGE_RANGE_BOUND_UINT 1
#define SURGE_RANGE_BOUND_FLOAT 2

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
    // SURGE_RANGE_BOUND_*. Meaningful only for SURGE_RANGE_KIND_BOUNDS: a
    // cursor's two slots hold an element data pointer and a stride, which are
    // neither counted nor a bound. It costs no growth -- the four padding bytes
    // the struct already carried are where it went.
    uint8_t bound;
    uint8_t _pad[3];
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
SURGE_RT_STATIC_ASSERT(offsetof(SurgeRange, bound) == 20,
                       "the bound-kind byte must match the emitter, and must stay inside the "
                       "padding so the cursor's index at 24 is untouched");

// Builds one bounded range, taking a reference to each bound as it stores it.
//
// The retain and the kind are written by ONE hand on purpose. A constructor
// that stored the words and left the retain to its caller is what this family
// used to be, and it made the range a borrower of two blocks the creating frame
// was about to release: `let r = 1.5..2.5` released both bounds before the loop
// began, and the first comparison read freed memory. And a retain cannot be
// fixed up after the call, because picking WHICH retain needs the kind -- which
// is the same byte this stores.
//
// The four `rt_range_int_*` entry points below are the language's range-literal
// spelling, `[a..b]` and its open-ended forms. Their name is not decoration:
// the type checker holds those bounds to `int`, so they are this constructor
// with SURGE_RANGE_BOUND_INT bound, and they are the only shape whose bounds
// the compiler does not spell at the call. The operator spelling `a..b` is
// generic over its bound type and reaches this one directly.
void* rt_range_bounds_new(void* start, void* end, bool inclusive, uint8_t bound);

// Takes a second reference to each bound a bounds-shaped range holds.
//
// The for-loop cursor is a BYTE COPY of the range it walks, so the copy names
// the same two blocks. That sharing is not transient -- the step overwrites
// only `start`, so range and cursor hold the same `end` for the whole loop, and
// both objects are freed at the end of it. Without this the second free is a
// double release; with it each object owns what it points at and frees it once.
//
// A cursor is returned untouched, by the shape test: its two slots hold an
// element data pointer and a stride, and retaining a stride is a wild write.
// The emitter cannot make that test itself -- the static type Range<T> covers
// both shapes -- so it calls this unconditionally and the byte decides.
void rt_range_bounds_retain(void* handle);

// Relinquish emission: makes a range's bounds private before the range is given
// up across a shard or thread boundary. Takes the SLOT holding the handle, the
// convention the array walk uses, though it needs no per-element callback: a
// range's payload is by construction one of exactly three counted scalars, all
// of which export `_unshare`, and the bound byte says which.
//
// An ARRAY_ITER cursor is REFUSED BY NAME rather than walked, the way
// rt_array_unshare_walk refuses a view. A cursor holds a raw interior pointer
// into an array buffer the origin shard keeps reading; no rewrite of the two
// slots can make those elements private, and the type cannot tell the two
// shapes apart, so the runtime is the only place the question can be asked.
void rt_range_unshare(void* range_slot);

// Reclaims ONE Range object, of either shape, sizing it off its own kind byte.
// Null-safe: a released slot is nulled and a second release must not read a
// kind byte out of nothing.
//
// It RELEASES each bound it holds, through the bound byte. Four facts had to be
// true before it could, and all four now are: the constructors above take a
// reference with the kind rather than storing a borrowed word; the byte says
// which of the three lifecycles to call; the shape byte keeps a cursor's data
// pointer and stride away from a bignum release, which would be a wild free
// rather than a double one; and the for-loop cursor takes its own references
// through rt_range_bounds_retain, so the two objects that share a bound release
// it once each.
//
// The has_start/has_end flags are consulted first, so an open-ended range never
// hands a null to a lifecycle entry point, and a cursor -- whose flags are both
// clear -- would read as holding no bounds even if the shape test above it were
// ever removed.
void rt_range_free(void* handle);

// Refuses a NULL range handle, and returns when the handle names a range.
//
// NULL is the default of a `Range<T>` (the emitter's null sentinel for every
// runtime handle), and a null range names no range at all: slicing by it or
// stepping it is a runtime error, not "the whole range" and not a load through
// NULL. Every entry point that reads a range calls this first -- the two slice
// bounds readers in rt_array.c and rt_string.c, and the emitter before it loads
// the kind byte of a range a `for` or a `.next()` is handed. The words and the
// code are the VM's, so both backends report one failure: `panic VM1203: null
// range handle`. Dropping, retaining or unsharing a null range is still nothing.
void rt_range_require(const void* handle);

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
// float and needs no allocation -- which is why a cross plan that charges a
// fixed sidecar for a float leaf is wrong for the zero, and why a non-zero
// float costs TWO allocations (the block and its mantissa). The copy starts at
// count 1 and shares nothing with its source, which is what would make it the
// leaf of the deep copy a crossing installs at a shard boundary.
//
// Its caller is rt_bigfloat_unshare below and the emitted crossing-clone walk.
void* rt_bigfloat_clone(const void* a);

// Make a bigfloat reference PRIVATE, for a value about to be relinquished
// across a shard or thread boundary. NULL-safe.
//
// At count one the caller holds the only reference and the block travels with
// the value: the same pointer comes back. Above one somebody else on this
// shard still holds it, so the caller receives a duplicate at count one and
// the reference it held is given up here -- on this thread, which is the only
// one allowed to touch a count that is not atomic.
//
// This is the crossing barrier's leaf. It reads the count without atomics, and
// may: the invariant it rests on is that a block is not reachable from two
// shards BEFORE the barrier runs, which is what the barrier itself preserves.
// A caller that is not the owning thread would be reading a count somebody
// else may be writing, so the relinquishing frame is the only correct site.
//
// Defined in rt_bigfloat_unshare.c. Its clone branch writes the
// `unshare_clones` field on the TRACE_RESIDENT line (rt_resident_bytes.h), as
// does the clone branch of every other counted scalar leaf's unshare;
// RV2_BIGFLOAT_UNSHARE_NEGATIVE_CONTROL makes this one the identity, which is
// how a row shows the barrier is what keeps a shared block off two threads.
void* rt_bigfloat_unshare(void* a);

// Destroy a bigfloat block unconditionally, IGNORING its count. This is the
// zero-count tail of a release, not an ownership operation: the runtime's own
// arithmetic uses it on temporaries it exclusively owns, and the compiled
// release path calls it once the count reaches zero. Compiled code must not
// call it to give up a reference — that is what rt_bigfloat_release is for.
void rt_bigfloat_free(void* a);

// Ownership operations on a reference-counted bigfloat. Both are NULL-safe.
// The count is NON-ATOMIC, so these are sound only while a block stays within
// one shard. Two things keep that true: a value relinquished across a boundary
// -- a capture, a far-select SEND payload, a crossing or blocking body's
// result, an anchored body's `ch.send(own f)` (which gives away the capture
// the caller already made private) -- has its counted leaves made private
// first (rt_bigfloat_unshare above, reaching a dynamic array's elements one by
// one through rt_array_unshare_walk), and a shape that walk cannot reach (a
// map's table, a channel's ring) is refused at compile time, at every gate --
// capture, channel element and reply alike.
//
// The LLVM backend inlines both as IR at the use site rather than calling
// these, so that a float copy costs a predictable not-taken branch instead of
// a call (the whole point of counting rather than cloning). These entry points
// are the reference semantics and the out-of-line form.
void rt_bigfloat_retain(void* a);
void rt_bigfloat_release(void* a);

// The same lifecycle for the heap halves of `int` and `uint`, all ten defined
// in rt_bignum_lifecycle.c. Read the bigfloat block above for what retain,
// release, free and unshare each mean; everything there applies here, including
// that the count is NON-ATOMIC and sound only while a block stays on one shard.
//
// What is different, and what makes these ten functions rather than five casts
// away from the bigfloat ones: an `int` or `uint` arrives as a TAGGED WORD. Low
// bit set means the value rode inline and there is no block; low bit clear
// means a heap block or NULL, which is the canonical zero. Every one of these
// tests that tag BEFORE it touches memory, so all ten are safe on an inline
// word as well as on NULL -- and a caller must not "optimise" the guard down to
// a NULL test the way the bigfloat emitter can, because an inline word is not a
// pointer and is neither aligned nor mapped.
//
// The counts sit at DIFFERENT OFFSETS per kind -- bigfloat 0, biguint 4,
// bigint 8 -- because a biguint view of a bigint's tail (bi_as_uint) has to
// keep matching field for field, which a count in the prefix position would
// break. rt_bignum_internal.h pins all three with _Static_assert. Those
// assertions pin the C side only: a backend that inlines retain and release as
// IR prints the offsets as literals, and nothing yet holds those literals
// against these numbers. Whichever lane teaches the emitter to call or inline
// these owes that test, as `internal/backend/llvm/range_layout_test.go` already
// does for the Range layout.
//
// Free is unconditional and IGNORES the count, exactly as rt_bigfloat_free
// does: it is the zero-count tail of a release and the reclamation the
// runtime's own arithmetic performs on temporaries it exclusively owns
// (bi_finish, bu_finish and the operand releases in rt_bignum_api.c still call
// the internal inlines on blocks that now start at count one, which is correct
// because those blocks never escape the call that made them). Compiled code
// gives up a reference with release and never with free; unifying the two would
// be a double free.
void* rt_bigint_clone(const void* a);
void rt_bigint_free(void* a);
void rt_bigint_retain(void* a);
void rt_bigint_release(void* a);
void* rt_bigint_unshare(void* a);
void* rt_biguint_clone(const void* a);
void rt_biguint_free(void* a);
void rt_biguint_retain(void* a);
void rt_biguint_release(void* a);
void* rt_biguint_unshare(void* a);

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
// parent -- a pin at the spawn's wake would come from whatever thread spawns.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void* __task_create_affine(uint64_t poll_fn_id, void* state, const rt_value_ops* result_ops);
// The two above publish the task at once: they are a stand driver's "create
// and spawn". Compiled code creates every task COLD (RV2-DEBT-370): recorded
// exactly as the constructors above record it -- membership, owning shard,
// parent, slot, and the carrier pin for the affine one -- and made runnable
// only by its first spawn, await, cancel or scope join. `frame_ops` is the
// start frame's descriptor: a task whose last handle is dropped while it is
// still cold is ended without a poll, and its frame, PACKED since the
// constructor built it, is released through it (rt_task_cold.c).
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void* __task_create_cold(uint64_t poll_fn_id,
                         void* state,
                         const rt_value_ops* result_ops,
                         const rt_value_ops* frame_ops);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void* __task_create_cold_affine(uint64_t poll_fn_id,
                                void* state,
                                const rt_value_ops* result_ops,
                                const rt_value_ops* frame_ops);
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
// Async send/recv callers keep the channel live across Pending and repolls,
// through Ready or the cancelled rt_async_yield boundary. Compiled code keeps
// that hold in its suspension frame or a structurally held owning activation.
bool rt_channel_send(void* channel, void* src);
bool rt_channel_send_yield(void* channel, void* src);
// Requires a live typed channel and writable disposable storage of its element
// type. Every normal return transfers or drops one offered reference; the bool
// means Ready/Pending only. No source address survives the call.
bool rt_channel_send_offer(void* channel, void* src);
bool rt_channel_send_yield_offer(void* channel, void* src);
uint8_t rt_channel_recv(void* channel, void* dst);
void rt_channel_send_blocking(void* channel, void* src);
uint8_t rt_channel_recv_blocking(void* channel, void* dst);
bool rt_channel_try_send(void* channel, void* src);
bool rt_channel_try_recv(void* channel, void* dst);
void rt_channel_close(void* channel);
// Handle copies retain; drops release. The last release destroys all remaining
// payloads. NULL is a no-op for moved-from container slots.
void rt_channel_handle_retain(void* channel);
void rt_channel_handle_drop(void* channel);
// Reclaims the header and inline buffer, dropping buffered and parked payloads
// exactly once through their element descriptor.
//
// Requires no live handle, waiter, subscription or in-flight operation; refuses
// and names any remaining holder. Normally reached by dropping the last handle.
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

uint64_t rt_scope_enter(bool failfast);
void rt_scope_register_child(uint64_t scope_id, void* task);
void rt_scope_cancel_all(uint64_t scope_id);
bool rt_scope_join_all(uint64_t scope_id, uint64_t* pending, bool* failfast);
void rt_scope_exit(uint64_t scope_id);

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
