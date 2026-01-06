package types

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/source"
)

// UnionMemberKind captures the nature of a union variant.
type UnionMemberKind uint8

const (
	// UnionMemberType represents a value variant in a union.
	UnionMemberType UnionMemberKind = iota
	UnionMemberNothing
	UnionMemberTag
)

// UnionMember describes a single variant inside a union.
type UnionMember struct {
	Kind    UnionMemberKind
	Type    TypeID
	TagName source.StringID
	TagArgs []TypeID
}

// UnionInfo stores metadata for a union type.
type UnionInfo struct {
	Name     source.StringID
	Decl     source.Span
	Members  []UnionMember
	TypeArgs []TypeID
}

// RegisterUnion allocates a nominal union type slot and returns its TypeID.
func (in *Interner) RegisterUnion(name source.StringID, decl source.Span) TypeID {
	slot := in.appendUnionInfo(UnionInfo{Name: name, Decl: decl})
	return in.internRaw(Type{Kind: KindUnion, Payload: slot})
}

// RegisterUnionInstance allocates a union instantiation with concrete type arguments.
func (in *Interner) RegisterUnionInstance(name source.StringID, decl source.Span, args []TypeID) TypeID {
	slot := in.appendUnionInfo(UnionInfo{Name: name, Decl: decl, TypeArgs: cloneTypeArgs(args)})
	return in.internRaw(Type{Kind: KindUnion, Payload: slot})
}

// SetUnionMembers stores the resolved members for the union type.
func (in *Interner) SetUnionMembers(typeID TypeID, members []UnionMember) {
	info := in.unionInfo(typeID)
	if info == nil {
		return
	}
	info.Members = cloneUnionMembers(members)
}

// UnionInfo returns metadata for the provided union TypeID.
func (in *Interner) UnionInfo(typeID TypeID) (*UnionInfo, bool) {
	info := in.unionInfo(typeID)
	if info == nil {
		return nil, false
	}
	return info, true
}

// UnionArgs returns type arguments for the union instantiation.
func (in *Interner) UnionArgs(typeID TypeID) []TypeID {
	info := in.unionInfo(typeID)
	if info == nil || len(info.TypeArgs) == 0 {
		return nil
	}
	return cloneTypeArgs(info.TypeArgs)
}

func (in *Interner) unionInfo(typeID TypeID) *UnionInfo {
	if typeID == NoTypeID {
		return nil
	}
	tt, ok := in.Lookup(typeID)
	if !ok || tt.Kind != KindUnion {
		return nil
	}
	if tt.Payload == 0 || int(tt.Payload) >= len(in.unions) {
		return nil
	}
	return &in.unions[tt.Payload]
}

func (in *Interner) appendUnionInfo(info UnionInfo) uint32 {
	if in.unions == nil {
		in.unions = append(in.unions, UnionInfo{})
	}
	in.unions = append(in.unions, UnionInfo{
		Name:     info.Name,
		Decl:     info.Decl,
		Members:  cloneUnionMembers(info.Members),
		TypeArgs: cloneTypeArgs(info.TypeArgs),
	})
	slot, err := safecast.Conv[uint32](len(in.unions) - 1)
	if err != nil {
		panic(fmt.Errorf("union info overflow: %w", err))
	}
	return slot
}

func cloneUnionMembers(members []UnionMember) []UnionMember {
	if len(members) == 0 {
		return nil
	}
	result := make([]UnionMember, len(members))
	copy(result, members)
	for i := range result {
		result[i].TagArgs = cloneTypeArgs(result[i].TagArgs)
	}
	return result
}
