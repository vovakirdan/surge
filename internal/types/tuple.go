package types //nolint:revive

import (
	"fmt"
	"strconv"
	"strings"

	"fortio.org/safecast"
)

// TupleInfo stores the element types for a tuple type.
type TupleInfo struct {
	Elems []TypeID
}

// RegisterTuple creates or finds an existing tuple type with the given elements.
//
// Finding is what the name promised and did not do. The general index is keyed
// on Type.Payload, and a tuple's payload is the slot this function is about to
// append, so two spellings of the same tuple could never collide there and every
// syntactic occurrence of `(int, int)` used to mint its own TypeID. Sema does not
// mind — it compares tuples element-wise — but the VM identifies a value's shape
// by its type id, so copying one spelling into the other was refused as a type
// mismatch at run time.
//
// Deduping is sound because a tuple has nothing to distinguish two instances by:
// no name, no declaration site, and no per-type layout attributes.
func (in *Interner) RegisterTuple(elems []TypeID) TypeID {
	key := tupleKey(elems)
	if id, ok := in.tupleIndex[key]; ok {
		return id
	}
	slot := in.appendTupleInfo(TupleInfo{Elems: cloneTypeArgs(elems)})
	id := in.internRaw(Type{Kind: KindTuple, Payload: slot})
	if in.tupleIndex == nil {
		in.tupleIndex = make(map[string]TypeID)
	}
	in.tupleIndex[key] = id
	return id
}

// tupleKey names a tuple by the one thing that identifies it: its elements, in
// order.
func tupleKey(elems []TypeID) string {
	var b strings.Builder
	for _, elem := range elems {
		b.WriteString(strconv.FormatUint(uint64(elem), 10))
		b.WriteByte(',')
	}
	return b.String()
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
