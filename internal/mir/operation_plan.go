package mir

import (
	"fmt"

	"surge/internal/layout"
	"surge/internal/mono"
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
	"surge/internal/valueops"
)

// noDescriptorRecipeIndex is the recipe index every entry carries while no
// module descriptor recipe table exists. Recipes arrive with the wave that emits
// descriptors; until then there is one index and it means "no table yet", which
// is why it is written once here instead of being spelled as a bare 0 per entry.
const noDescriptorRecipeIndex = 0

// OperationPlanInput is everything beyond physical layout that publishing a
// module's operation plans requires.
//
// It is ONE parameter rather than three because the three are one decision: a
// plan is built from a capability authority, the clone bodies that authority's
// verdicts imply, and the instances monomorphization emitted for them. Passing
// two of the three is never meaningful, so the type does not allow it.
//
// A nil *OperationPlanInput means "publish layouts only". That is the shape unit
// tests and the single-file MIR dump hold: neither runs the whole-program
// capability classification an operation plan is derived from, and a registry
// built from a partial view would answer for the program out of one file's
// ignorance of it. Absence publishes nothing at all, so a reader gets the
// registry's own fail-closed "missing finalized operation registry" rather than
// a plausible wrong answer.
type OperationPlanInput struct {
	// Capabilities is the whole-program capability authority. Its verdicts are
	// the semantic half of every entry.
	Capabilities *sema.CapabilityClassifier
	// Clones answers which clone bodies those verdicts require emitted. It is
	// the same index the required-operation derivation consulted before
	// monomorphization, so what the registry demands is what the closure was
	// asked to reach.
	Clones sema.CloneEmissionIndex
	// Callables maps a canonical callable identity to the instance symbol
	// monomorphization emitted for it.
	Callables mono.CallableMap
}

// NewOperationPlanInput assembles the plan input the production pipelines pass.
//
// Both of them call this rather than composing the struct themselves: the two
// sites lower MIR through the same sequence of passes, and an input built two
// ways is the beginning of two registries for one program.
//
// It returns nil when the result carries no capability authority. That is not a
// failure — it is the honest answer for a path that never reached a
// whole-program authority site, and it publishes no operation registry at all.
func NewOperationPlanInput(res *sema.Result, mm *mono.MonoModule) *OperationPlanInput {
	if res == nil || res.Capabilities == nil || mm == nil {
		return nil
	}
	return &OperationPlanInput{
		Capabilities: res.Capabilities,
		Clones:       res.NewCloneEmissionIndex(),
		Callables:    mm.Callables,
	}
}

// finalizeOperationPlans turns one census plus its frozen layouts into a frozen
// operation registry.
//
// It fails closed at every step. An input missing any of its three parts is
// refused by name; a root whose layout, capability or required clone cannot be
// resolved refuses the whole registry rather than contributing an entry that
// guesses. The registry itself then applies the slot invariant, which is the
// last of these checks and the only one that knows about ABI bits.
func finalizeOperationPlans(
	census *RootCensus,
	layouts *layout.Registry,
	in *OperationPlanInput,
	typesIn *types.Interner,
) (*valueops.Registry, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	roots := census.Values()
	input := valueops.Input{
		Roots:  make([]valueops.Root, 0, len(roots)),
		Values: make(map[types.TypeID]valueops.Entry, len(roots)),
		Keys:   make(map[types.TypeID]valueops.KeyOps),
	}
	for _, id := range roots {
		role, err := operationRole(census.Role(id), id)
		if err != nil {
			return nil, err
		}
		entry, err := in.entryFor(id, layouts, typesIn)
		if err != nil {
			return nil, err
		}
		input.Roots = append(input.Roots, valueops.Root{Type: id, Role: role})
		input.Values[id] = entry
		if role&valueops.RoleKey != 0 {
			// Hash and equal are staged: the manifest requires both, nothing
			// derives either yet, and an absent map entry is an error rather than
			// an implied zero, so every key root is recorded as explicitly absent.
			input.Keys[id] = valueops.KeyOps{Hash: symbols.NoSymbolID, Equal: symbols.NoSymbolID}
		}
	}
	registry, err := valueops.Finalize(input)
	if err != nil {
		return nil, fmt.Errorf("mir: finalize operation plans: %w", err)
	}
	return registry, nil
}

// validate refuses an input that cannot answer for the whole program.
func (in *OperationPlanInput) validate() error {
	if in == nil {
		return fmt.Errorf("mir: operation plan publication needs an operation plan input")
	}
	if in.Capabilities == nil {
		return fmt.Errorf("mir: operation plan publication needs the program's capability authority")
	}
	if !in.Clones.Built() {
		return fmt.Errorf("mir: operation plan publication needs the program's clone emission index")
	}
	if !in.Callables.HasCanonicalIdentity() {
		return fmt.Errorf(
			"mir: operation plan publication needs a callable map carrying the canonical instantiation identity; " +
				"note: a monomorphized module built without a finalized instantiation closure keys callables by raw symbol and cannot answer for the program",
		)
	}
	return nil
}

// entryFor builds one root's frozen operation plan.
func (in *OperationPlanInput) entryFor(
	id types.TypeID,
	layouts *layout.Registry,
	typesIn *types.Interner,
) (valueops.Entry, error) {
	facts, err := layouts.Require(id)
	if err != nil {
		return valueops.Entry{}, fmt.Errorf("mir: operation plan for %s: %w", typeLabel(typesIn, id), err)
	}
	capability, err := in.Capabilities.Classify(id)
	if err != nil {
		return valueops.Entry{}, fmt.Errorf("mir: operation plan for %s: %w", typeLabel(typesIn, id), err)
	}
	flags, cloneInit, err := in.cloneSlot(&capability, typesIn)
	if err != nil {
		return valueops.Entry{}, err
	}
	if capability.Copy {
		flags |= valueops.FlagCopy
	}
	// Droppable is an ABI bit now, not a capability: the backend names the drop
	// body from the type id. The verdict still has exactly one source -- this
	// classifier -- which is what keeps the registry and the emitter from
	// answering the same question twice.
	if capability.CarrierDroppable {
		flags |= valueops.FlagDroppable
	}
	// ShardMovable is an ABI bit too, since Epic 22's move half gave its slot a
	// body. The verdict is the classifier's alone (owned `own` move hands over
	// one obligation rather than sharing a counted block, so `float` keeps it),
	// and it is NOT also recorded in Capabilities: the owner's ruling of
	// 2026-09-04 is that flag, descriptor, registry hash and Dump derive from
	// one backed state, never from a verdict plus a late mask in the backend.
	if capability.ShardMovable {
		flags |= valueops.FlagShardMovable
	}
	return valueops.Entry{
		Type:  id,
		Facts: facts,
		Flags: flags,
		Capabilities: valueops.Capabilities{
			Traceable:     capability.Traceable,
			CrossClonable: capability.CrossClonable,
		},
		CloneInit:   cloneInit,
		RecipeIndex: noDescriptorRecipeIndex,
		Evidence: valueops.Evidence{
			CloneMethodKey:   capability.Clone.MethodKey,
			CloneReason:      capability.Clone.Reason,
			DroppableReason:  capability.DroppableReason,
			TraceableReason:  capability.TraceableReason,
			ShardReason:      capability.ShardReason,
			CrossCloneReason: capability.CrossCloneReason,
			ShardPath:        capability.ShardPath,
			CrossClonePath:   capability.CrossClonePath,
		},
	}, nil
}

// cloneSlot answers whether one type's plan carries the CLONABLE bit, and with
// which emitted symbol.
//
// The bit is set exactly when the compiler emitted a clone_init for the type. A
// type whose canonical clone is an intrinsic or a builtin is clonable in the
// language and carries no bit here: its duplication is the runtime's own, there
// is no emitted symbol to name, and the frozen manifest ties clone_init's
// presence to this bit. The evidence still records which body was selected, so
// the plan says "clonable through the runtime" rather than saying nothing.
//
// A type whose clone body IS emitted but whose instance the callable map does
// not hold is a fail-closed error. The required-operation fixpoint injected
// exactly these bodies as instantiation roots before monomorphization ran, so a
// miss here is that guarantee having failed, not a program that needs allowing.
func (in *OperationPlanInput) cloneSlot(
	capability *sema.Capability,
	typesIn *types.Interner,
) (valueops.Flags, symbols.SymbolID, error) {
	op, required, err := in.Clones.RequiredValueOpFor(capability)
	if err != nil {
		return 0, symbols.NoSymbolID, fmt.Errorf(
			"mir: operation plan for %s: %w", typeLabel(typesIn, capability.Type), err)
	}
	if !required {
		return 0, symbols.NoSymbolID, nil
	}
	instance, found, err := in.Callables.LookupChecked(op.Template, op.TemplateArgs)
	if err != nil {
		return 0, symbols.NoSymbolID, fmt.Errorf(
			"mir: resolve %s for %s: %w", op.Reason, typeLabel(typesIn, capability.Type), err)
	}
	if !found {
		return 0, symbols.NoSymbolID, fmt.Errorf(
			"mir: %s needs %s from template sym#%d with type arguments %s, "+
				"which this program monomorphized no instance for; "+
				"note: a clonable type's implementation is made reachable before monomorphization, so every one of them should have an instance",
			typeLabel(typesIn, capability.Type), op.Reason, op.Template, typeArgLabels(typesIn, op.TemplateArgs))
	}
	return valueops.FlagClonable, instance, nil
}

// operationRole translates one census role into the registry's, refusing a role
// the registry has no meaning for rather than dropping the bit quietly.
func operationRole(role RootRole, id types.TypeID) (valueops.Role, error) {
	if unknown := role &^ (RootValue | RootKey); unknown != 0 {
		return 0, fmt.Errorf("mir: operation root type#%d was reached through unknown roles %#x", id, uint8(unknown))
	}
	if role&RootValue == 0 {
		return 0, fmt.Errorf("mir: operation root type#%d was never reached as a value", id)
	}
	out := valueops.RoleValue
	if role&RootKey != 0 {
		out |= valueops.RoleKey
	}
	return out, nil
}

// typeLabel names a type for a diagnostic, falling back to its id when the
// interner cannot spell it.
func typeLabel(typesIn *types.Interner, id types.TypeID) string {
	if typesIn == nil {
		return fmt.Sprintf("type#%d", id)
	}
	label := types.Label(typesIn, id)
	if label == "" {
		return fmt.Sprintf("type#%d", id)
	}
	return fmt.Sprintf("`%s` (type#%d)", label, id)
}

func typeArgLabels(typesIn *types.Interner, args []types.TypeID) string {
	if len(args) == 0 {
		return "[]"
	}
	out := "["
	for i, arg := range args {
		if i > 0 {
			out += ", "
		}
		out += typeLabel(typesIn, arg)
	}
	return out + "]"
}
