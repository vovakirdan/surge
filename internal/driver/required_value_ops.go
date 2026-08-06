package driver

import (
	"fmt"

	"surge/internal/mono"
	"surge/internal/sema"
)

// requiredValueOpBudget bounds the accumulated demand, and through it the loop:
// rounds cannot outnumber the operations they discover, because a round that
// discovers nothing new is the last one. A program whose demands genuinely keep
// growing therefore reports the budget it exhausted instead of spinning. It is
// the closure's own instance budget, so both limits describe one program size.
var requiredValueOpBudget = sema.DefaultInstantiationClosureInstanceLimit

// requiredValueOpClosureDepth is the expansion depth the recomputed closures
// use. It mirrors the depth the finalization sites above this one pass, so a
// program that expands acceptably on the first closure expands acceptably on
// every later one and no root can be lost to a shallower recomputation.
const requiredValueOpClosureDepth = 64

// reachRequiredValueOperations drives the instantiation closure to a fixpoint
// over the operations reachable types require.
//
// One pass is not enough on its own: making a clone implementation reachable
// makes its BODY reachable, and that body can name a type nothing else in the
// program named, which may itself be clonable. So the closure is recomputed
// until a round discovers no operation an earlier round had not.
//
// It is a step of its own rather than a tail of closure finalization because
// finalization happens first, while diagnosing, and the capability authority
// this needs does not exist that early. Monomorphization runs once, after all
// of this, and there is no second pass behind it — an operation discovered late
// would have nothing left to emit it, which is why the loop lives here rather
// than after MIR.
//
// It runs only at the two whole-program authority sites, and which site this is
// has to be told rather than guessed: the per-file path can be handed a
// semantic result an earlier whole-program pass already classified, and
// deriving there would answer for the program out of one file's view.
func reachRequiredValueOperations(res *DiagnoseResult) error {
	if res == nil || !res.wholeProgramAuthority || res.Sema == nil {
		return nil
	}
	if res.Sema.Capabilities == nil || res.Sema.InstantiationIdentity == nil {
		return nil
	}
	identity := *res.Sema.InstantiationIdentity
	reached := make(map[string]struct{})
	changed := false
	for {
		installReachableBodyTypes(res)
		ops, err := res.Sema.DeriveRequiredValueOps(res.Sema.Capabilities)
		if err != nil {
			return err
		}
		fresh := ops[:0:0]
		for i := range ops {
			key := ops[i].RequiredValueOpKey()
			if _, known := reached[key]; known {
				continue
			}
			reached[key] = struct{}{}
			fresh = append(fresh, ops[i])
		}
		if len(fresh) == 0 {
			break
		}
		if len(reached) > requiredValueOpBudget {
			return fmt.Errorf(
				"required value operations exceeded the concrete-instance budget %d; note: the operations a program's types require must converge to a finite set",
				requiredValueOpBudget,
			)
		}
		res.Sema.InjectRequiredValueOpRoots(fresh)
		if err := res.Sema.FinalizeInstantiationClosure(identity, requiredValueOpClosureDepth); err != nil {
			return err
		}
		changed = true
	}
	if !changed || res.Instantiations == nil {
		return nil
	}
	// The compatibility instantiation view is derived from the closure and was
	// built from the smaller one. Published decisions need no such repair: a
	// required operation adds instances, it never rebinds a use site.
	rebuilt, err := mono.RebuildInstantiationMapFromClosure(
		res.Instantiations,
		res.Sema.InstantiationClosure,
		res.Sema.InstantiationIdentity,
	)
	if err != nil {
		return err
	}
	res.Instantiations = rebuilt
	return nil
}
