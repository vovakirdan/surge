package sema

import (
	"cmp"
	"fmt"
	"slices"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// ReturnSourceRequirement identifies an original contract member, not just its
// deduplicated contract owner. Contract and Member belong to the source-owning
// unit; importing/canonical symbol IDs must not replace that association.
type ReturnSourceRequirement struct {
	Contract symbols.SymbolID
	Member   source.Span
	Sources  types.ReturnSources
	// Sources retains original slots; this counts only physically prepended self.
	ReceiverPrefix uint8
}

// ReturnSourceRequirements returns detached values; Sources is itself immutable.
func (req *DeferredCallableRequirement) ReturnSourceRequirements() []ReturnSourceRequirement {
	if req == nil {
		return nil
	}
	return slices.Clone(req.returnSourceRequirements)
}

// Only a physical prepend changes the mapping. The offset used to compare
// user arguments after skipping self never changes these declaration slots.
func alignContractMethodRequirement(req *methodRequirement, target types.TypeID, arity int) (methodRequirement, bool) {
	if req == nil {
		return methodRequirement{}, false
	}
	aligned := *req
	switch arity {
	case len(req.params):
		return aligned, true
	case len(req.params) + 1:
		aligned.params = append([]types.TypeID{target}, req.params...)
		aligned.receiverPrefix++
		return aligned, true
	default:
		return methodRequirement{}, false
	}
}

func mergeReturnSourceRequirements(left, right []ReturnSourceRequirement) []ReturnSourceRequirement {
	merged := append(slices.Clone(left), right...)
	slices.SortFunc(merged, func(a, b ReturnSourceRequirement) int {
		return cmp.Or(cmp.Compare(a.Contract, b.Contract), cmp.Compare(a.Member.File, b.Member.File),
			cmp.Compare(a.Member.Start, b.Member.Start), cmp.Compare(a.Member.End, b.Member.End),
			cmp.Compare(a.ReceiverPrefix, b.ReceiverPrefix))
	})
	for i := 1; i < len(merged); i++ {
		a, b := merged[i-1], merged[i]
		if a.Contract == b.Contract && a.Member == b.Member && !a.Sources.Equal(b.Sources) {
			panic(fmt.Sprintf("return-source metadata conflict for contract %d member %v", a.Contract, a.Member))
		}
	}
	return slices.CompactFunc(merged, returnSourceRequirementsEqual)
}

func returnSourceRequirementsEqual(a, b ReturnSourceRequirement) bool {
	return a.Contract == b.Contract && a.Member == b.Member && a.ReceiverPrefix == b.ReceiverPrefix && a.Sources.Equal(b.Sources)
}
