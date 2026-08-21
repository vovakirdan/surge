package llvm

import (
	"fmt"
	"sort"

	"surge/internal/types"
)

// emitValueOpsLookup emits `__surge_value_ops_for`: the runtime's way to get a
// type's descriptor from the type's id.
//
// WHY AN ID AND NOT A POINTER. A descriptor is a process-static address, so a
// caller that already has one can pass it directly. A far channel cannot: the
// far side names a payload type by id, that id travels across the boundary, and
// the local side then has to build a channel for it. A pointer would be
// meaningless there, and an id is meaningful on both sides -- so the id is what
// the channel constructor takes, and this is the one place that turns it back
// into a descriptor.
//
// A type with no descriptor answers null rather than panicking. "Does this type
// have one" is a legitimate question with a legitimate negative answer -- the
// registry skips a type whose slots the backend cannot fill honestly -- and a
// caller that needs one checks. That is the opposite of the drop dispatch next
// door, where an id is only ever produced for a type that HAS a wrapper, so
// arriving with an unknown one is a defect and panics.
func (e *Emitter) emitValueOpsLookup() {
	ids := e.emittedValueOpsTypeIDs()

	fmt.Fprintf(&e.buf, "define ptr @__surge_value_ops_for(i64 %%id) {\n")
	fmt.Fprintf(&e.buf, "entry:\n")
	if len(ids) == 0 {
		// Nothing was emitted, so every id answers the same way. A switch with
		// no cases is legal but says less than this does.
		fmt.Fprintf(&e.buf, "  ret ptr null\n")
		fmt.Fprintf(&e.buf, "}\n\n")
		return
	}
	fmt.Fprintf(&e.buf, "  switch i64 %%id, label %%value_ops_absent [\n")
	for _, id := range ids {
		fmt.Fprintf(&e.buf, "    i64 %d, label %%value_ops.%d\n", id, id)
	}
	fmt.Fprintf(&e.buf, "  ]\n")
	for _, id := range ids {
		fmt.Fprintf(&e.buf, "value_ops.%d:\n", id)
		fmt.Fprintf(&e.buf, "  ret ptr @%s\n", valueOpsSymbol(id))
	}
	fmt.Fprintf(&e.buf, "value_ops_absent:\n")
	fmt.Fprintf(&e.buf, "  ret ptr null\n")
	fmt.Fprintf(&e.buf, "}\n\n")
}

// emittedValueOpsTypeIDs lists, in a stable order, exactly the types whose
// descriptor this module defines. It asks the same question emitValueOpsDescriptors
// answers, so a type skipped there cannot be routed to here.
func (e *Emitter) emittedValueOpsTypeIDs() []types.TypeID {
	if e == nil || e.mod == nil || e.mod.Meta == nil || e.mod.Meta.Operations == nil {
		return nil
	}
	registry := e.mod.Meta.Operations
	ids := make([]types.TypeID, 0, len(registry.TypeIDs()))
	for _, id := range registry.TypeIDs() {
		entry, err := registry.Value(id)
		if err != nil {
			continue
		}
		if !e.valueOpsEmittable(&entry) {
			continue
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
