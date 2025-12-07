package types

import (
	"fmt"

	"fortio.org/safecast"
)

// TupleInfo stores the element types for a tuple type.
type TupleInfo struct {
	Elems []TypeID
}

// RegisterTuple creates or finds an existing tuple type with the given elements.
func (in *Interner) RegisterTuple(elems []TypeID) TypeID {
	slot := in.appendTupleInfo(TupleInfo{Elems: cloneTypeArgs(elems)})
	return in.internRaw(Type{Kind: KindTuple, Payload: slot})
}

// TupleInfo returns the element types for a tuple TypeID.
func (in *Interner) TupleInfo(id TypeID) (*TupleInfo, bool) {
	tt, ok := in.Lookup(id)
	if !ok || tt.Kind != KindTuple {
		return nil, false
	}
	if int(tt.Payload) >= len(in.tuples) {
		return nil, false
	}
	return &in.tuples[tt.Payload], true
}

func (in *Interner) appendTupleInfo(info TupleInfo) uint32 {
	if in.tuples == nil {
		in.tuples = append(in.tuples, TupleInfo{})
	}
	in.tuples = append(in.tuples, TupleInfo{
		Elems: cloneTypeArgs(info.Elems),
	})
	slot, err := safecast.Conv[uint32](len(in.tuples) - 1)
	if err != nil {
		panic(fmt.Errorf("tuple info overflow: %w", err))
	}
	return slot
}
