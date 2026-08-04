#ifndef SURGE_RUNTIME_NATIVE_RT_SLOT_CONTROL_H
#define SURGE_RUNTIME_NATIVE_RT_SLOT_CONTROL_H

#include "rt_typed_carrier_abi.generated.h"

#include <stddef.h>
#include <stdint.h>

// Owner-private lifecycle state for one typed-carrier slot. `slot` is the
// owner's sole authoritative rt_slot_header, and that generated header stays
// state-only. Every other field below is protected by the owner's existing
// lock and is deliberately outside the frozen B2 ABI.

typedef enum {
    RT_SLOT_CONTROL_OK = 0,
    RT_SLOT_CONTROL_INVALID_ARGUMENT = 1,
    RT_SLOT_CONTROL_INVALID_STATE = 2,
    RT_SLOT_CONTROL_STALE = 3,
    RT_SLOT_CONTROL_BUSY = 4,
    RT_SLOT_CONTROL_EPOCH_EXHAUSTED = 5,
    RT_SLOT_CONTROL_LOGICAL_SELF = 6,
    RT_SLOT_CONTROL_STORAGE_OVERLAP = 7,
    RT_SLOT_CONTROL_STORAGE_OVERFLOW = 8,
    RT_SLOT_CONTROL_STORAGE_MISALIGNED = 9,
    RT_SLOT_CONTROL_OPERATIONS_MISMATCH = 10,
    RT_SLOT_CONTROL_INVALID_TOKEN = 11,
    RT_SLOT_CONTROL_INVARIANT = 12,
    RT_SLOT_CONTROL_LAYOUT_MISMATCH = 13,
    RT_SLOT_CONTROL_INVALID_OPERATIONS = 14,
    RT_SLOT_CONTROL_UNSUPPORTED_OPERATION = 15,
} rt_slot_control_status;

typedef enum {
    RT_SLOT_CLAIM_INVALID = 0,
    RT_SLOT_CLAIM_COPY = 1,
    RT_SLOT_CLAIM_CLONE = 2,
    RT_SLOT_CLAIM_TRACE = 3,
    RT_SLOT_CLAIM_MOVE = 4,
    RT_SLOT_CLAIM_DROP = 5,
    RT_SLOT_CLAIM_CROSS_MOVE = 6,
    RT_SLOT_CLAIM_CROSS_CLONE = 7,
} rt_slot_claim_kind;

typedef enum {
    RT_SLOT_RESERVATION_NONE = 0,
    RT_SLOT_RESERVATION_FALLIBLE = 1,
    RT_SLOT_RESERVATION_IRREVOCABLE = 2,
    RT_SLOT_RESERVATION_CLEANUP = 3,
} rt_slot_reservation_phase;

typedef struct rt_slot_control rt_slot_control;

// Allocation-free reader record supplied by the owner. Its address and fields
// must remain stable while active; initialize it only before linking a claim.
typedef struct rt_slot_read_claim {
    struct rt_slot_read_claim* next;
    const rt_slot_control* source_control;
    const rt_slot_control* destination_control;
    uint64_t epoch;
    uint64_t destination_identity;
    uint64_t destination_generation;
    uint64_t destination_epoch;
    rt_slot_claim_kind kind;
    uint8_t has_destination;
    uint8_t active;
} rt_slot_read_claim;

// Written once by a successful claim operation. A failed claim clears its
// non-null output to a zero token. All completion APIs accept a const token and
// never mutate it; copying a token cannot create another entitlement because
// the owner validates its registered epoch exactly once.
typedef struct {
    const rt_slot_control* source_control;
    const rt_slot_control* destination_control;
    const rt_value_ops* operations;
    uint64_t source_identity;
    uint64_t source_generation;
    uint64_t source_epoch;
    uint64_t destination_identity;
    uint64_t destination_generation;
    uint64_t destination_epoch;
    rt_slot_claim_kind kind;
    uint8_t has_destination;
} rt_claim_token;

struct rt_slot_control {
    rt_slot_header slot;
    // The process-static B2 descriptor outlives this control and every token.
    const rt_value_ops* operations;
    uintptr_t storage_address;
    size_t storage_size;
    size_t storage_alignment;
    uint64_t logical_identity;
    uint64_t generation;
    uint64_t next_epoch;
    rt_slot_read_claim* readers;
    uint64_t exclusive_epoch;
    const rt_slot_control* reservation_source_control;
    const rt_slot_control* reservation_destination_control;
    uint64_t reservation_source_identity;
    uint64_t reservation_source_generation;
    uint64_t reservation_source_epoch;
    uint64_t reservation_epoch;
    uint32_t reader_pins;
    rt_slot_claim_kind exclusive_kind;
    rt_slot_claim_kind reservation_kind;
    rt_slot_reservation_phase reservation_phase;
};

rt_slot_control_status rt_slot_storage_preflight(const rt_value_ops* operations,
                                                 uintptr_t address,
                                                 size_t size,
                                                 size_t alignment,
                                                 uintptr_t* out_end);
rt_slot_control_status rt_slot_pair_preflight(const rt_slot_control* source,
                                              const rt_slot_control* destination);

rt_slot_control_status rt_slot_control_init(rt_slot_control* control,
                                            uint64_t logical_identity,
                                            const rt_value_ops* operations,
                                            uint64_t generation,
                                            uintptr_t storage_address,
                                            size_t storage_size,
                                            size_t storage_alignment);
void rt_slot_read_claim_init(rt_slot_read_claim* claim);

// Every function suffixed _locked requires the owner lock(s) protecting all
// supplied controls. No function here acquires a lock or invokes a callback.
// Controls must stay at stable addresses while any claim token is live.
rt_slot_control_status rt_slot_publish_initial_locked(rt_slot_control* control,
                                                      uint64_t generation);
rt_slot_control_status rt_slot_begin_generation_locked(rt_slot_control* control,
                                                       uint64_t generation,
                                                       uintptr_t storage_address,
                                                       size_t storage_size,
                                                       size_t storage_alignment);

rt_slot_control_status rt_slot_claim_read_locked(rt_slot_control* source,
                                                 rt_slot_control* destination,
                                                 rt_slot_claim_kind kind,
                                                 uint64_t source_generation,
                                                 uint64_t destination_generation,
                                                 rt_slot_read_claim* registration,
                                                 rt_claim_token* out_token);
rt_slot_control_status rt_slot_retire_read_locked(rt_slot_control* source,
                                                  const rt_claim_token* token);

rt_slot_control_status rt_slot_claim_exclusive_locked(rt_slot_control* source,
                                                      rt_slot_control* destination,
                                                      rt_slot_claim_kind kind,
                                                      uint64_t source_generation,
                                                      uint64_t destination_generation,
                                                      rt_claim_token* out_token);

// After a read callback returns, retiring its source pin and resolving its
// destination reservation are independent and may occur in either order. The
// exact reservation remains valid if the source later moves or begins a newer
// generation; it never consults the live source. A rejected initialized
// destination stays reserved through the required out-of-lock drop until
// finish_destination_cleanup clears it.
rt_slot_control_status rt_slot_publish_destination_locked(rt_slot_control* destination,
                                                          const rt_claim_token* token);
rt_slot_control_status rt_slot_reject_initialized_destination_locked(rt_slot_control* destination,
                                                                     const rt_claim_token* token);
rt_slot_control_status rt_slot_finish_destination_cleanup_locked(rt_slot_control* destination,
                                                                 const rt_claim_token* token);
rt_slot_control_status rt_slot_release_empty_destination_locked(rt_slot_control* destination,
                                                                const rt_claim_token* token);

// After a successful move/drop callback these are infallible publication
// points. Any non-OK result is RT_SLOT_CONTROL_INVARIANT and must take the
// runtime's terminal internal-error path; it is never a recoverable refusal.
rt_slot_control_status rt_slot_commit_move_locked(rt_slot_control* source,
                                                  rt_slot_control* destination,
                                                  const rt_claim_token* token);
// DROP is a lifecycle transition for every initialized value. When the
// descriptor is not DROPPABLE, the owner performs no callback and commits the
// claim directly; trivial scalars and ZSTs therefore remain deletable.
rt_slot_control_status rt_slot_commit_drop_locked(rt_slot_control* source,
                                                  const rt_claim_token* token);

// The only recoverable exclusive rollback. It is legal solely after the B2
// cross-move callback returned failure and proved source unchanged/dst empty.
rt_slot_control_status rt_slot_cross_move_failed_locked(rt_slot_control* source,
                                                        rt_slot_control* destination,
                                                        const rt_claim_token* token,
                                                        int source_unchanged,
                                                        int destination_empty);

#endif
