package types //nolint:revive

import (
	"fmt"
	"slices"

	"fortio.org/safecast"
)

// FnInfo stores metadata for function types.
type FnInfo struct {
	Params        []TypeID // Parameter types (in order)
	Result        TypeID   // Return type
	returnSources ReturnSources
}

// ReturnSources returns the immutable declared contract, not inferred body facts.
func (info *FnInfo) ReturnSources() ReturnSources {
	return info.returnSources
}

// RegisterFn creates or finds a function type.
func (in *Interner) RegisterFn(params []TypeID, result TypeID) TypeID {
	return in.RegisterFnWithReturnSources(params, result, ReturnSources{})
}

// RegisterFnWithReturnSources interns a declared function contract. Source-level
// validation belongs to sema; an out-of-range slot here is an internal invariant
// violation and must not silently become the default contract.
func (in *Interner) RegisterFnWithReturnSources(params []TypeID, result TypeID, sources ReturnSources) TypeID {
	sources.validateArity(len(params))
	if in != nil {
		for id := TypeID(1); int(id) < len(in.types); id++ {
			tt := in.types[id]
			if tt.Kind != KindFn {
				continue
			}
			if int(tt.Payload) >= len(in.fns) {
				continue
			}
			info := in.fns[tt.Payload]
			if info.Result == result && slices.Equal(info.Params, params) && info.returnSources.Equal(sources) {
				return id
			}
		}
	}
	slot := in.appendFnInfo(FnInfo{
		Params:        cloneTypeArgs(params),
		Result:        result,
		returnSources: sources,
	})
	return in.internRaw(Type{Kind: KindFn, Payload: slot})
}

// RebuildFn substitutes a signature while preserving its declared input slots.
// Substitution changes types, never the number or meaning of formal parameters.
func (in *Interner) RebuildFn(originalFn TypeID, params []TypeID, result TypeID) TypeID {
	info, ok := in.FnInfo(originalFn)
	if !ok || info == nil {
		panic(fmt.Errorf("function rebuild: type#%d is not a function", originalFn))
	}
	if len(params) != len(info.Params) {
		panic(fmt.Errorf("function rebuild: arity changed from %d to %d for type#%d", len(info.Params), len(params), originalFn))
	}
	return in.RegisterFnWithReturnSources(params, result, info.returnSources)
}

// FnInfo retrieves function type metadata by TypeID.
func (in *Interner) FnInfo(id TypeID) (*FnInfo, bool) {
	tt, ok := in.Lookup(id)
	if !ok || tt.Kind != KindFn {
		return nil, false
	}
	if int(tt.Payload) >= len(in.fns) {
		return nil, false
	}
	return &in.fns[tt.Payload], true
}

func (in *Interner) appendFnInfo(info FnInfo) uint32 {
	in.fns = append(in.fns, FnInfo{
		Params:        cloneTypeArgs(info.Params),
		Result:        info.Result,
		returnSources: info.returnSources,
	})
	slot, err := safecast.Conv[uint32](len(in.fns) - 1)
	if err != nil {
		panic(fmt.Errorf("fn info overflow: %w", err))
	}
	return slot
}
