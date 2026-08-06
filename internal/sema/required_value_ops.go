package sema

import (
	"fmt"
	"slices"
	"sort"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// requiredCloneOpReason names the operation a clonable type needs emitted.
const requiredCloneOpReason = "clone initialization"

// requiredValueOpWitnessReason is why the instance exists at all. It is the
// only witness reason a root with no source call site can honestly carry: no
// expression asked for this instance, the type's own capability did.
const requiredValueOpWitnessReason = "required value operation"

// RequiredValueOp is one value operation the program must emit for a type
// because the type HAS that capability, whether or not any expression asks.
//
// A type with a valid `__clone` is clonable everywhere it travels — a carrier
// duplicating it calls the implementation through the value's operation table,
// and that table is filled by the compiler, not by a source-level `clone(&x)`.
// So the implementation has to be reachable before anything can call it, which
// is what makes this a root and not an edge.
type RequiredValueOp struct {
	Receiver     types.TypeID
	Template     symbols.SymbolID
	TemplateArgs []types.TypeID
	Reason       string
	Witness      InstantiationWitness
}

// DeriveRequiredValueOps answers which value operations the reachable program
// requires, for the clone axis.
//
// The type universe is the REACHABLE CLOSURE's, never MIR's: the derivation
// runs before monomorphization so that what it finds still has a chance to be
// emitted, and a universe read out of MIR would arrive one pass too late.
//
// The classifier is passed in rather than built here. It is the program's one
// capability authority, and the registry that later demands a resolvable
// clone_init for every CLONABLE type consults that same object; a private
// second classifier would be free to disagree with it, and the disagreement
// would surface as a fail-closed registry error on a valid program.
func (r *Result) DeriveRequiredValueOps(c *CapabilityClassifier) ([]RequiredValueOp, error) {
	if r == nil {
		return nil, fmt.Errorf("required value operations need a semantic result")
	}
	if c == nil {
		return nil, fmt.Errorf("required value operations need the program's capability classifier")
	}
	universe := r.reachableValueTypes(c)
	emitters := r.cloneEmitterIndex()
	ops := make([]RequiredValueOp, 0, 4)
	for _, id := range universe {
		capability, err := c.Classify(id)
		if err != nil {
			return nil, err
		}
		if capability.Clone.State != CloneValidMethod {
			continue
		}
		emitter, known := emitters[capability.Clone.MethodKey]
		if !known {
			return nil, fmt.Errorf(
				"clone implementation %q was selected for type %d but names no callable in this program's merged catalog",
				capability.Clone.MethodKey, uint32(id),
			)
		}
		if !emitter.emits {
			// An intrinsic or builtin clone is already whatever the runtime does
			// with that type. There is no body to reach, so requiring one would
			// name a root that can never be satisfied.
			continue
		}
		ops = append(ops, RequiredValueOp{
			Receiver:     id,
			Template:     emitter.symbol,
			TemplateArgs: slices.Clone(capability.Clone.TemplateArgs),
			Reason:       requiredCloneOpReason,
			Witness: InstantiationWitness{
				Site:      emitter.site,
				SourceKey: emitter.sourceKey,
				Reason:    requiredValueOpWitnessReason,
			},
		})
	}
	sort.SliceStable(ops, func(i, j int) bool { return compareRequiredValueOp(&ops[i], &ops[j]) < 0 })
	return slices.CompactFunc(ops, func(left, right RequiredValueOp) bool {
		return compareRequiredValueOp(&left, &right) == 0
	}), nil
}

// InjectRequiredValueOpRoots makes every derived operation reachable.
//
// The two shapes take different doors on purpose. A generic implementation is
// an instantiation demand and goes through the same root the graph already
// holds for an explicit clone. A non-generic one has no template arguments, so
// the instantiation graph will not hold it — recordRoot refuses an empty
// argument vector — and the root-module seed policy will not take it either,
// because that policy is deliberately about bodies declared in the root module
// and a clone implementation is routinely declared in a dependency. It gets its
// own named input instead: a new kind of root should be visible as one.
//
// Injection is idempotent. Both destinations dedupe, so a fixpoint round that
// rediscovers an operation it already injected changes nothing.
func (r *Result) InjectRequiredValueOpRoots(ops []RequiredValueOp) {
	if r == nil {
		return
	}
	for i := range ops {
		op := &ops[i]
		if !op.Template.IsValid() {
			continue
		}
		if len(op.TemplateArgs) > 0 {
			r.InstantiationGraph.recordRoot(&InstantiationRoot{
				Kind:         InstantiationFunction,
				Template:     op.Template,
				TemplateArgs: op.TemplateArgs,
				Witness:      op.Witness,
			})
			continue
		}
		if r.RequiredValueOpRoots == nil {
			r.RequiredValueOpRoots = make(map[symbols.SymbolID]struct{})
		}
		r.RequiredValueOpRoots[op.Template] = struct{}{}
	}
}

// RequiredValueOpKey is the identity that decides whether a fixpoint round
// found something new. Two operations naming the same implementation for the
// same receiver are the same demand however they were witnessed.
func (op *RequiredValueOp) RequiredValueOpKey() string {
	if op == nil {
		return ""
	}
	return fmt.Sprintf("%d/%d/%v", uint32(op.Receiver), op.Template, op.TemplateArgs)
}

func compareRequiredValueOp(left, right *RequiredValueOp) int {
	if left.Receiver != right.Receiver {
		if left.Receiver < right.Receiver {
			return -1
		}
		return 1
	}
	if left.Template != right.Template {
		if left.Template < right.Template {
			return -1
		}
		return 1
	}
	return slices.Compare(left.TemplateArgs, right.TemplateArgs)
}

// cloneEmitter is what the catalog knows about a selected clone body: which
// symbol carries it, whether the compiler has anything to emit for it, and
// where it was declared.
type cloneEmitter struct {
	symbol    symbols.SymbolID
	site      source.Span
	sourceKey string
	emits     bool
}

// cloneEmitterIndex maps a canonical body key to the callable that carries it.
//
// The join is on the BODY KEY rather than the symbol because the classifier and
// this result can hold the same catalog under two symbol vocabularies: the
// module-record path folds every module's candidates without remapping, while
// the merged result renumbers them into the root table. The body key is the one
// identity both spellings agree on, and it is what the root must be recorded
// against, so it is what selects the symbol here.
func (r *Result) cloneEmitterIndex() map[string]cloneEmitter {
	index := make(map[string]cloneEmitter, 4)
	for i := range r.CallableCandidates {
		candidate := &r.CallableCandidates[i]
		if candidate.Name != "__clone" || candidate.BodyKey == "" || !candidate.Symbol.IsValid() {
			continue
		}
		entry := cloneEmitter{
			symbol:    candidate.Symbol,
			site:      candidate.Source,
			sourceKey: candidate.SourceKey,
			emits:     candidate.HasBody && !candidate.Intrinsic && !candidate.Builtin,
		}
		existing, found := index[candidate.BodyKey]
		if !found || preferCloneEmitter(entry, existing) {
			index[candidate.BodyKey] = entry
		}
	}
	return index
}

// preferCloneEmitter picks between two spellings of one body. A spelling the
// compiler can emit wins over one it cannot; between equals the lowest symbol
// wins, so the choice does not move between runs.
func preferCloneEmitter(candidate, existing cloneEmitter) bool {
	if candidate.emits != existing.emits {
		return candidate.emits
	}
	return candidate.symbol < existing.symbol
}
