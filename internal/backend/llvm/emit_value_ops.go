package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
	"surge/internal/valueops"
)

// The descriptor writer: one rt_value_ops constant per exact type in the
// module's operation registry, plus the per-type move body each one points at.
//
// Wave B item 3 says the compiler GENERATES the concrete value operations, and
// until this file nothing did — the backend knew rt_value_ops only as the shape
// of a record. A registry row that names a filler nothing emits is the failure
// mode flags.go records verbatim, so the row and its body land together.
//
// Nothing reads these constants yet. That is deliberate: an emitted descriptor
// changes no program's behaviour until an owner binds one, which makes this the
// one step in the chain that can be proven in isolation — the runtime's own slot
// preflight either admits it or does not.

// valueOpsSymbol names the descriptor for one exact type.
//
// It is C-spellable, unlike the `drop.type%d` glue family, because the proof
// links a C probe that must write `extern const rt_value_ops __surge_value_ops_type7;`.
func valueOpsSymbol(id types.TypeID) string { return fmt.Sprintf("__surge_value_ops_type%d", id) }

// moveInitName names the per-type move body.
//
// The id is the EXACT registry id, not the resolved one dropGlueName uses.
// Entry.Type is the exact semantic id and Entry.Facts was fetched with it, so
// resolving here would let two entries carrying different facts collide on one
// body.
func moveInitName(id types.TypeID) string { return fmt.Sprintf("move_init.type%d", id) }

// valueOpsSlotOrder is the rt_value_ops field order after layout, exactly as the
// frozen record declares it. The initializer is positional, so this list is the
// contract: a field appearing here in the wrong place emits a descriptor whose
// slots are silently transposed.
var valueOpsSlotOrder = [...]string{
	"move_init",
	"copy_init",
	"clone_init",
	"drop_in_place",
	"trace",
	"plan_cross",
	"cross_move_init",
	"cross_clone_init",
}

// requireValueOpsDropBodies asks for the drop body every emittable descriptor
// will point at, and it has to run BEFORE the glue pass, because that pass
// closes its own demand: a body first asked for while descriptors are being
// written would be named and never defined.
func (e *Emitter) requireValueOpsDropBodies() {
	if e == nil || e.mod == nil || e.mod.Meta == nil || e.mod.Meta.Operations == nil {
		return
	}
	registry := e.mod.Meta.Operations
	for _, id := range registry.TypeIDs() {
		entry, err := registry.Value(id)
		if err != nil || entry.Flags&valueops.FlagDroppable == 0 {
			continue
		}
		if !e.valueOpsEmittable(&entry) {
			continue
		}
		e.requireDropGlue(entry.Type)
	}
}

// emitValueOpsDescriptors writes every descriptor the registry can back.
//
// It runs after the glue passes, in the tail slot this file reserves for a pass
// that writes what earlier passes named.
func (e *Emitter) emitValueOpsDescriptors() {
	if e == nil || e.mod == nil || e.mod.Meta == nil || e.mod.Meta.Operations == nil {
		// No whole-program capability authority ran, so no registry was
		// published. Absence publishes nothing rather than an empty claim.
		return
	}
	registry := e.mod.Meta.Operations
	for _, id := range registry.TypeIDs() {
		entry, err := registry.Value(id)
		if err != nil {
			continue
		}
		if !e.valueOpsEmittable(&entry) {
			continue
		}
		e.emitMoveInitBody(&entry)
	}
	for _, id := range registry.TypeIDs() {
		entry, err := registry.Value(id)
		if err != nil {
			continue
		}
		if !e.valueOpsEmittable(&entry) {
			continue
		}
		e.emitValueOpsConstant(&entry)
	}
}

// valueOpsEmittable reports whether every slot this entry needs has a filler the
// backend can name today.
//
// The rule is mechanical rather than a hardcoded bit: a slot answered
// FillRegistryNamedBody wants a symbol the registry carries and the backend
// cannot mint, which today is clone_init under FlagClonable. Skipping is honest;
// emitting a descriptor with a null clone_init while the flag is set would ship
// exactly the flag/callback disagreement the runtime refuses.
func (e *Emitter) valueOpsEmittable(entry *valueops.Entry) bool {
	for _, slot := range valueOpsSlotOrder {
		filler, err := valueops.SlotFiller(slot, entry.Flags)
		if err != nil {
			return false
		}
		if slot == "drop_in_place" && filler.Kind == valueops.FillBackendDerivedBody {
			// The bit means the manifest's sentence: drop_in_place is present
			// AND has work to do. The registry's verdict is about the carrier's
			// obligation, and for a leaf family this backend cannot reclaim --
			// a far lease, an opaque runtime resource -- the two answers differ.
			// Skipping the descriptor is the only honest option left: a set bit
			// over a body that frees nothing passes the runtime's preflight and
			// then leaks in silence, which nothing downstream can catch.
			if !e.typeOwnsHeap(entry.Type) {
				return false
			}
		}
		if filler.Kind == valueops.FillRegistryNamedBody {
			// The registry carries the symbol; the backend must be able to name
			// the function it became. A monomorphized clone whose instance is
			// absent from this module cannot be bound, and a descriptor that
			// claimed the capability with a null callback is the flag/callback
			// disagreement the runtime refuses.
			if e.registryNamedSymbol(entry, slot) == "" {
				return false
			}
		}
	}
	return true
}

// emitMoveInitBody writes the per-type transfer initializer.
//
// A move is a byte transfer: the destination becomes the sole owner and the
// source is logically empty afterwards, so there is no per-member work and no
// user code to run. A zero-sized type gets the same definition with an empty
// body, because a value of no bytes still has an obligation to transfer.
//
// There is no null guard, unlike the drop and clone glue. The frozen ABI
// declares both parameters nonnull, and the runtime's only dispatcher refuses
// null before it dispatches; a guard here would be a second, weaker statement of
// a precondition that already holds.
func (e *Emitter) emitMoveInitBody(entry *valueops.Entry) {
	align := entry.Facts.Align
	if align == 0 {
		align = 1
	}
	fmt.Fprintf(&e.buf, "define void @%s(ptr %%dst, ptr %%src) {\nentry:\n", moveInitName(entry.Type))
	e.emitGlueStorageCopy("%dst", "%src", entry.Facts.Size, align)
	e.buf.WriteString("  ret void\n}\n\n")
}

// emitValueOpsConstant writes one descriptor.
//
// Every pointer operand is resolved through the registry's slot table rather
// than decided here. The emitter holding its own opinion about which slot takes
// which symbol is how the two sides drift, and the table is the only place the
// contract is written down.
func (e *Emitter) emitValueOpsConstant(entry *valueops.Entry) {
	operands := make([]string, 0, len(valueOpsSlotOrder))
	for _, slot := range valueOpsSlotOrder {
		filler, err := valueops.SlotFiller(slot, entry.Flags)
		if err != nil {
			return
		}
		operands = append(operands, e.valueOpsOperand(entry, slot, filler))
	}
	align := entry.Facts.Align
	if align == 0 {
		align = 1
	}
	stride := entry.Facts.Stride
	if emitDescriptorDefect == defectOverStride {
		stride += align
	}
	fmt.Fprintf(&e.buf,
		"@%s = constant %%struct.rt_value_ops { %%struct.rt_value_layout { i64 %d, i64 %d, i64 %d, i64 %d }",
		valueOpsSymbol(entry.Type), entry.Facts.Size, align, stride, uint64(entry.Flags))
	for _, operand := range operands {
		fmt.Fprintf(&e.buf, ", %s", operand)
	}
	e.buf.WriteString(" }\n\n")
}

// descriptorDefect plants a deliberate fault in an emitted descriptor.
//
// It exists so the proof's negative control travels the SAME code path as the
// real emission: a defect written by hand into C would prove the runtime rejects
// a bad descriptor, not that this writer produces a good one.
type descriptorDefect uint8

const (
	defectNone descriptorDefect = iota
	// defectNullPlanCross is the control RV2-DEBT-230 names by name.
	defectNullPlanCross
	// defectOverStride breaks the layout arithmetic instead of a slot, so the
	// proof cannot pass by checking pointers alone.
	defectOverStride
	// defectReturningPlanCrossStub makes the plan_cross stub RETURN a status
	// rather than trap. It is the control for the stub's terminality: a trap
	// that returns is not a trap, and a test that cannot see the difference is
	// not proving anything.
	defectReturningPlanCrossStub
)

// valueOpsOperand renders one slot of the descriptor.
func (e *Emitter) valueOpsOperand(entry *valueops.Entry, slot string, filler valueops.Filler) string {
	if emitDescriptorDefect == defectNullPlanCross && slot == "plan_cross" {
		return "ptr null"
	}
	switch filler.Kind {
	case valueops.FillRuntimeSymbol, valueops.FillModuleStub:
		return "ptr @" + filler.Symbol
	case valueops.FillBackendDerivedBody:
		if slot == "move_init" {
			return "ptr @" + moveInitName(entry.Type)
		}
		if slot == "drop_in_place" {
			// Named on the RESOLVED type, because that is the namespace glue
			// bodies live in: requireDropGlue resolves on the way in and
			// emitDropGlueBody resolves again before printing its own name.
			// The descriptor stays keyed on the exact type, the way move_init
			// is -- the two namespaces meet here rather than being merged.
			return "ptr @" + dropGlueName(resolveValueType(e.types, entry.Type))
		}
		// The only other backend-derived body is plan_cross for a type that can
		// cross, and no such descriptor is reachable while both cross bits are
		// filled nowhere. Reaching here means a bit was un-staged without giving
		// this writer the body it now needs.
		return "ptr null"
	case valueops.FillRegistryNamedBody:
		if name := e.registryNamedSymbol(entry, slot); name != "" {
			return "ptr @" + name
		}
		return "ptr null"
	case valueops.FillNone:
		return "ptr null"
	default:
		return "ptr null"
	}
}

// emitModuleWithDescriptorDefect is EmitModule with a planted descriptor fault.
// It exists for the descriptor proof's negative control and is unexported, so a
// defect cannot be requested from outside this package.
func emitModuleWithDescriptorDefect(
	mod *mir.Module,
	typesIn *types.Interner,
	symTable *symbols.Table,
	files *source.FileSet,
	defect descriptorDefect,
) (string, error) {
	emitDescriptorDefect = defect
	defer func() { emitDescriptorDefect = defectNone }()
	return EmitModule(mod, typesIn, symTable, files)
}

// emitDescriptorDefect carries the planted fault into the emitter. It is a
// package variable rather than a parameter because EmitModule's signature is the
// backend's public entry point and a test-only argument does not belong in it.
var emitDescriptorDefect = defectNone

// registryNamedSymbol resolves the LLVM name of a per-type body the REGISTRY
// carries, as opposed to one the backend derives.
//
// Today that is clone_init alone: mir resolved the monomorphized instance and
// stored its symbol, and this maps that symbol to the function this module
// emits. It returns "" when the instance is not in this module, which is a
// reason to skip the descriptor rather than to ship a null against a set bit.
func (e *Emitter) registryNamedSymbol(entry *valueops.Entry, slot string) string {
	if slot != "clone_init" {
		return ""
	}
	if e == nil || e.mod == nil || !entry.CloneInit.IsValid() {
		return ""
	}
	id, ok := e.mod.FuncBySym[entry.CloneInit]
	if !ok {
		return ""
	}
	name, ok := e.funcNames[id]
	if !ok {
		return ""
	}
	return name
}

// descriptorReferencedFuncs lists the functions a descriptor will bind, so
// reachability treats a descriptor as the reference it is.
//
// The emitted constant is a use: a type whose clone no source line calls still
// has a clone the runtime may reach through its descriptor. Leaving these out of
// the root set produced a descriptor naming an undefined symbol.
func (e *Emitter) descriptorReferencedFuncs() []mir.FuncID {
	if e == nil || e.mod == nil || e.mod.Meta == nil || e.mod.Meta.Operations == nil {
		return nil
	}
	var roots []mir.FuncID
	for _, id := range e.mod.Meta.Operations.TypeIDs() {
		entry, err := e.mod.Meta.Operations.Value(id)
		if err != nil || !entry.CloneInit.IsValid() {
			continue
		}
		if funcID, ok := e.mod.FuncBySym[entry.CloneInit]; ok {
			roots = append(roots, funcID)
		}
	}
	return roots
}
