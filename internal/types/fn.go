package types //nolint:revive

import (
	"fmt"
	"slices"

	"fortio.org/safecast"
)

// FnInfo stores metadata for function types.
type FnInfo struct {
	Params []TypeID // Parameter types (in order)
	Result TypeID   // Return type
}

// RegisterFn creates or finds a function type.
func (in *Interner) RegisterFn(params []TypeID, result TypeID) TypeID {
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
			if info.Result == result && slices.Equal(info.Params, params) {
				return id
			}
		}
	}
	slot := in.appendFnInfo(FnInfo{
		Params: cloneTypeArgs(params),
		Result: result,
	})
	return in.internRaw(Type{Kind: KindFn, Payload: slot})
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
		Params: cloneTypeArgs(info.Params),
		Result: info.Result,
	})
	slot, err := safecast.Conv[uint32](len(in.fns) - 1)
	if err != nil {
		panic(fmt.Errorf("fn info overflow: %w", err))
	}
	return slot
}
