#ifndef SURGE_RUNTIME_NATIVE_RT_VALUE_OPS_H
#define SURGE_RUNTIME_NATIVE_RT_VALUE_OPS_H

#include "rt_typed_carrier_abi.generated.h"

#include <stddef.h>

// rt_value_copy_init_unbound_trap is declared by the generated ABI header,
// because binding it is part of the compiler/runtime contract: a descriptor that
// sets RT_VALUE_FLAG_COPY without a per-type copy fills copy_init with that
// symbol, and the manifest hash in the link sentinel is what refuses a runtime
// that does not carry it.
//
// This header holds the runtime-internal half of the same decision. The symbol
// is a trap and not a copy: the frozen rt_value_copy_init_fn signature is
// (void* dst, const void* src) and carries no width, so no callback can copy on
// its own, and the width is the descriptor's rt_value_layout.size. Every
// ordinary copy therefore goes through rt_value_copy_init, which still holds the
// descriptor at the call site and never dispatches the trap. Dispatching it
// straight through rt_value_ops.copy_init aborts the process rather than
// performing a silent zero-byte copy into storage the owner will treat as
// initialized.

#ifdef __cplusplus
extern "C" {
#endif

// Performs one ordinary copy for `operations`, initializing `dst` from `src`.
//
// The descriptor must be one rt_slot_control already accepted: RT_VALUE_FLAG_COPY
// set and copy_init non-null, which rt_slot_operations_preflight requires of every
// descriptor it admits. When copy_init is the unbound-dispatch trap the copy is
// exactly `operations->layout.size` bytes, performed here; any other copy_init is
// a backend's own specialization for that type and is dispatched through the
// pointer. It is the only place in the runtime that dispatches the slot.
//
// It performs no allocation, invokes no user code, and must run outside the owner
// lock like every other rt_value_ops operation.
void rt_value_copy_init(const rt_value_ops* operations, void* dst, const void* src);

// Reports whether `operations` takes its copy width from rt_value_layout.size —
// that is, whether copy_init holds the unbound-dispatch trap rather than a
// backend-emitted specialization. It answers 0 for a null or non-Copy descriptor
// rather than refusing, so a caller can ask before it knows.
int rt_value_copy_uses_runtime_width(const rt_value_ops* operations);

// Transfers one obligation from `src` into the empty `dst`.
//
// `move_init` is always present, so unlike a copy there is no flag to consult
// and no width to supply: the callback carries the whole transfer. What this
// adds is the lane check. Every rt_value_ops operation must run OUTSIDE the
// owner lock, and until now nothing enforced that — the requirement lived in a
// comment while the only dispatch helper was for copies. A caller that reaches
// here holding control or a shard is refused rather than dispatched, because a
// generated callback under a runtime lock is the failure the epic's §8 P2 names
// and it is not one a later check can see.
void rt_value_move_init_detached(const rt_value_ops* operations, void* dst, void* src);

// Builds an independent value in the empty `dst` out of the initialized `src`,
// which stays owned by its holder.
//
// This is the one that can run USER code -- a `__clone` body -- so the lane
// check matters most here: a clone under a runtime lock is the failure §8 P2
// names, and a user body is exactly the code most likely to reenter the
// runtime and deadlock rather than misbehave visibly. A descriptor whose
// RT_VALUE_FLAG_CLONABLE is clear has no clone to dispatch, and reaching here
// with one is the flag/callback disagreement the manifest forbids, so it fails
// closed rather than leaving `dst` uninitialized for its owner to publish.
void rt_value_clone_init_detached(const rt_value_ops* operations, void* dst, const void* src);

// Dispatches a duplication that a CALL SITE supplied rather than a descriptor,
// under the same rule: no runtime lock may be held while generated code runs.
//
// It exists because one duplication is not the value's own property but the
// obligation of an operation performed on it -- cloning a task handle takes on
// serving a second asker, and the body that serves them is chosen there. The
// refusal is the same one, because the reason is the same one.
void rt_value_duplicate_detached(rt_value_clone_init_fn duplicate, void* dst, const void* src);

// Destroys the one obligation `value` holds.
//
// A descriptor whose RT_VALUE_FLAG_DROPPABLE is clear has NO obligation, and
// this returns without dispatching — that is the flag's meaning rather than a
// swallowed error, and the manifest's own biconditional ("present exactly when
// the flag is set") is what makes the two readings the same. Everything else is
// the move helper's contract: fail closed on a lock, dispatch otherwise.
void rt_value_drop_in_place_detached(const rt_value_ops* operations, void* value);

// The descriptor for an opaque machine word: what a far channel holds today,
// and what a C stand uses when no compiled code supplies one.
const rt_value_ops* rt_channel_opaque_word_ops(void);

#ifdef __cplusplus
}
#endif

#endif
