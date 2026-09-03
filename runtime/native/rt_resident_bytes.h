#ifndef SURGE_RUNTIME_NATIVE_RT_RESIDENT_BYTES_H
#define SURGE_RUNTIME_NATIVE_RT_RESIDENT_BYTES_H

#include <stdint.h>

typedef struct rt_value_ops rt_value_ops;

// What a cross-shard crossing keeps resident, and for how long, counted at
// the physical owner of each byte rather than at the language syntax. Every
// figure is a process-wide live balance with its own high-water mark and a
// running total of what was ever acquired, so a reader can tell "nothing is
// held now" from "nothing was ever held".
//
//   ENVELOPE  the fields of one rt_transport_msg per envelope sitting in a
//             shard's data or control lane, from push to pop -- the fixed
//             record the ruling of 2026-08-29 says a pointer message is
//             charged for;
//   PADDING   the bytes of that envelope no field occupies (alignment slack
//             the struct layout inserts), counted beside the envelope so the
//             header figure reads as fields plus padding, sizeof the struct;
//   RECORD    the pending that tracks one crossing on the source side (a
//             remote-task pending or a spawn pending), from allocation to
//             its last release;
//   PAYLOAD   the body state block a crossing ships, at the width its type
//             descriptor states, while the pending still owns it -- from
//             submission to the publication-accepted handoff (after which the
//             body owns it and it is a task's frame, not transport) or to the
//             pending's drop of an unshipped state; plus a remote select's
//             staged SEND payloads that did not fit an arm cell inline;
//   SIDECAR   a remote select's arm table, from allocation to free.
//
// A crossing CLONE is the one duplication a crossing performs: compiled code
// copies every capture whose type is Copy into the state block. That is a
// total, not a balance -- the copy is owned by the state block and its bytes
// are already inside PAYLOAD.
typedef enum rt_resident_kind {
    RT_RESIDENT_ENVELOPE = 0,
    RT_RESIDENT_PADDING = 1,
    RT_RESIDENT_RECORD = 2,
    RT_RESIDENT_PAYLOAD = 3,
    RT_RESIDENT_SIDECAR = 4,
    RT_RESIDENT_KIND_COUNT = 5,
} rt_resident_kind;

struct rt_resident_bytes_snapshot {
    uint64_t live[RT_RESIDENT_KIND_COUNT];
    uint64_t peak[RT_RESIDENT_KIND_COUNT];
    uint64_t acquired[RT_RESIDENT_KIND_COUNT];
    uint64_t live_total;
    uint64_t peak_total;
    uint64_t crossing_clone_bytes;
    uint64_t crossing_clones;
    // Releases that found less held than they gave back. Always zero on a
    // correct runtime; the balance is clamped rather than wrapped so a
    // reader sees one number that says "the bookkeeping disagreed".
    uint64_t underflows;
};

void rt_resident_bytes_acquire(rt_resident_kind kind, uint64_t bytes);
void rt_resident_bytes_release(rt_resident_kind kind, uint64_t bytes);
// A shipped state block at its descriptor's width. A NULL descriptor is a
// state with no bytes.
void rt_resident_payload_acquire(const rt_value_ops* operations);
void rt_resident_payload_release(const rt_value_ops* operations);
void rt_resident_bytes_record_crossing_clone(uint64_t bytes);
struct rt_resident_bytes_snapshot rt_resident_bytes_snapshot(void);
const char* rt_resident_kind_name(rt_resident_kind kind);
// One TRACE_RESIDENT line on stderr, with the exec trace's other dumps.
void rt_resident_bytes_dump(const char* reason);

#endif
