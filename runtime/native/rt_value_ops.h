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

#ifdef __cplusplus
}
#endif

#endif
