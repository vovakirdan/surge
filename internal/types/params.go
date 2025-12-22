package types

import (
	"fmt"
	"surge/internal/source"

	"fortio.org/safecast"
)

// TypeParamInfo stores metadata about a generic type parameter.
type TypeParamInfo struct {
	Name      source.StringID
	Owner     uint32
	Index     uint32
	IsConst   bool
	ConstType TypeID
}

// RegisterTypeParam allocates a new generic parameter descriptor.
func (in *Interner) RegisterTypeParam(name source.StringID, owner, index uint32, isConst bool, constType TypeID) TypeID {
	slot := in.appendTypeParamInfo(TypeParamInfo{
		Name:      name,
		Owner:     owner,
		Index:     index,
		IsConst:   isConst,
		ConstType: constType,
	})
	return in.internRaw(Type{
		Kind:    KindGenericParam,
		Count:   owner,
		Payload: slot,
	})
}

// TypeParamInfo returns metadata for the provided generic parameter.
func (in *Interner) TypeParamInfo(id TypeID) (*TypeParamInfo, bool) {
	if id == NoTypeID {
		return nil, false
	}
	tt, ok := in.Lookup(id)
	if !ok || tt.Kind != KindGenericParam {
		return nil, false
	}
	if tt.Payload == 0 || int(tt.Payload) >= len(in.params) {
		return nil, false
	}
	info := in.params[tt.Payload]
	return &info, true
}

// RemapTypeParamOwners updates generic param owner IDs using the provided mapping.
// The mapping is keyed by old owner IDs and yields new owner IDs.
func (in *Interner) RemapTypeParamOwners(mapping map[uint32]uint32) {
	if in == nil || len(mapping) == 0 {
		return
	}
	for i := range in.params {
		if i == 0 {
			continue
		}
		owner := in.params[i].Owner
		if mapped, ok := mapping[owner]; ok {
			in.params[i].Owner = mapped
		}
	}
}

func (in *Interner) appendTypeParamInfo(info TypeParamInfo) uint32 {
	if in.params == nil {
		in.params = append(in.params, TypeParamInfo{})
	}
	in.params = append(in.params, info)
	lenParams, err := safecast.Conv[uint32](len(in.params) - 1)
	if err != nil {
		panic(fmt.Errorf("type param index overflow: %w", err))
	}
	return lenParams
}
