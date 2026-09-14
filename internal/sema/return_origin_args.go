package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
)

// Each entry is a declaration slot, including holes for omitted defaults.
// A variadic slot owns all of its actual expressions; none shift later slots.
type returnOriginArgument struct {
	exprs     []ast.ExprID
	defaulted bool
}

func mapReturnOriginArguments(sig *symbols.FunctionSignature, call *ast.ExprCallData, receiver ast.ExprID) ([]returnOriginArgument, error) {
	if sig == nil || call == nil || len(sig.ParamNames) != len(sig.Params) {
		return nil, fmt.Errorf("return origins: missing formal argument metadata")
	}
	slots := make([]returnOriginArgument, len(sig.Params))
	names := make(map[source.StringID]int, len(sig.Params))
	variadic := -1
	for i, name := range sig.ParamNames {
		if name != source.NoStringID {
			names[name] = i
		}
		if i < len(sig.Variadic) && sig.Variadic[i] {
			variadic = i
		}
	}
	next := 0
	if receiver.IsValid() {
		if !sig.HasSelf || len(slots) == 0 {
			return nil, fmt.Errorf("return origins: receiver has no self slot")
		}
		slots[0].exprs = []ast.ExprID{receiver}
		next = 1
	}
	for _, arg := range call.Args {
		slot := next
		if arg.Name != source.NoStringID {
			var ok bool
			slot, ok = names[arg.Name]
			if !ok {
				return nil, fmt.Errorf("return origins: unknown named argument")
			}
		} else if variadic >= 0 && slot >= variadic {
			slot = variadic
		}
		if slot < 0 || slot >= len(slots) || (len(slots[slot].exprs) > 0 && slot != variadic) {
			return nil, fmt.Errorf("return origins: missing or multiply filled formal slot")
		}
		slots[slot].exprs = append(slots[slot].exprs, arg.Value)
		if arg.Name == source.NoStringID && slot != variadic {
			next++
		}
	}
	for i := range slots {
		if len(slots[i].exprs) > 0 || i == variadic {
			continue
		}
		if i >= len(sig.Defaults) || !sig.Defaults[i] {
			return nil, fmt.Errorf("return origins: required formal slot %d is absent", i)
		}
		slots[i].defaulted = true
	}
	return slots, nil
}
