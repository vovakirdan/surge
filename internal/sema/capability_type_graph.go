package sema

import "surge/internal/types"

// handleKind separates the handle-backed families, which
// types.RuntimeHandlePayloads reports as one.
//
// They are one family physically — a pointer-sized handle naming storage it
// owns — and three semantically. A string and a container duplicate by design;
// an opaque runtime resource (a task, a channel, a range) does not; and
// Placement is a plain intrinsic value that merely lives in the same list.
type handleKind uint8

const (
	handleNone handleKind = iota
	handleString
	handleElements
	handleResource
	handlePlacement
)

// isNominalArray reports the built-in Array<T> and ArrayFixed<T, N> shapes,
// which are nominal structs rather than KindArray. The attribute validator's
// arrayInfo accepts both spellings, so this mirror has to as well.
func (c *CapabilityClassifier) isNominalArray(id types.TypeID) bool {
	if _, ok := c.types.ArrayInfo(id); ok {
		return true
	}
	_, _, ok := c.types.ArrayFixedInfo(id)
	return ok
}

func (c *CapabilityClassifier) handleKind(id types.TypeID) (handleKind, []types.TypeID) {
	if tt, ok := c.types.Lookup(id); ok && tt.Kind == types.KindString {
		return handleString, nil
	}
	payloads, ok := c.types.RuntimeHandlePayloads(id)
	if !ok {
		return handleNone, nil
	}
	if c.types.IsRuntimePlacementType(id) {
		return handlePlacement, nil
	}
	if c.types.IsRuntimeHandleType(id) {
		return handleResource, payloads
	}
	return handleElements, payloads
}

// components returns, in a stable order, the resolved types a value of this
// type is built from. It is the single edge relation: the cycle pass and all
// four axes walk exactly this graph, so no axis can follow an edge the cycle
// pass did not know about.
func (c *CapabilityClassifier) components(id types.TypeID) []types.TypeID {
	tt, ok := c.types.Lookup(id)
	if !ok {
		return nil
	}
	switch tt.Kind {
	case types.KindReference, types.KindPointer, types.KindFar, types.KindFn:
		// These name storage they do not carry; the pointee is its own value
		// with its own answers.
		return nil
	case types.KindOwn:
		return c.resolveAll([]types.TypeID{tt.Elem})
	}
	if kind, payloads := c.handleKind(id); kind != handleNone {
		return c.resolveAll(payloads)
	}
	// A nominal array is a struct with no fields, so the struct arm below would
	// answer about an empty type and every axis would call it inert. The
	// spelling is not a different kind of value from the structural one -- it is
	// the same elements written another way -- and the classifier already reads
	// it a few lines up for the shard axis, which is what makes this a missing
	// edge rather than a decision.
	if elem, ok := c.types.ArrayInfo(id); ok {
		return c.resolveAll([]types.TypeID{elem})
	}
	if elem, _, ok := c.types.ArrayFixedInfo(id); ok {
		return c.resolveAll([]types.TypeID{elem})
	}
	switch tt.Kind {
	case types.KindStruct:
		fields := c.types.StructFields(id)
		out := make([]types.TypeID, 0, len(fields))
		for i := range fields {
			out = append(out, fields[i].Type)
		}
		return c.resolveAll(out)
	case types.KindTuple:
		if info, found := c.types.TupleInfo(id); found && info != nil {
			return c.resolveAll(info.Elems)
		}
		return nil
	case types.KindUnion:
		return c.resolveAll(unionMemberTypes(c.types, id))
	case types.KindArray:
		return c.resolveAll([]types.TypeID{tt.Elem})
	}
	return nil
}

// Components publishes the same edge relation, for one type, to callers outside
// this package.
//
// It exists so that an agreement test can walk the graph the verdicts were
// computed over instead of a second graph of its own. Comparing what the
// classifier REACHES against what another leg of the same question reaches is
// only meaningful if both walks follow one relation; a private copy of the walk
// would agree by construction on the day it was written and drift afterwards,
// which is the failure such a test exists to catch.
//
// The id is resolved first, so an alias asks about the type it names.
func (c *CapabilityClassifier) Components(id types.TypeID) []types.TypeID {
	if c == nil {
		return nil
	}
	resolved := c.resolve(id)
	if resolved == types.NoTypeID {
		return nil
	}
	return c.components(resolved)
}

// unionMemberTypes lists a union's member and tag payload types in declaration
// order.
func unionMemberTypes(in *types.Interner, id types.TypeID) []types.TypeID {
	info, ok := in.UnionInfo(id)
	if !ok || info == nil {
		return nil
	}
	out := make([]types.TypeID, 0, len(info.Members))
	for _, member := range info.Members {
		switch member.Kind {
		case types.UnionMemberType:
			out = append(out, member.Type)
		case types.UnionMemberTag:
			out = append(out, member.TagArgs...)
		}
	}
	return out
}

// resolveAll drops components the interner cannot answer for and the type's own
// alias hops, so every id the walk emits is a node the axes can evaluate.
func (c *CapabilityClassifier) resolveAll(ids []types.TypeID) []types.TypeID {
	out := make([]types.TypeID, 0, len(ids))
	for _, id := range ids {
		resolved := c.resolve(id)
		if resolved == types.NoTypeID {
			continue
		}
		out = append(out, resolved)
	}
	return out
}

// resolve is the node identity: aliases collapse to what they name, which is
// where the attribute facts are recorded and how the rest of sema asks.
func (c *CapabilityClassifier) resolve(id types.TypeID) types.TypeID {
	if id == types.NoTypeID {
		return types.NoTypeID
	}
	resolved := resolveAlias(c.types, id)
	if _, ok := c.types.Lookup(resolved); !ok {
		return types.NoTypeID
	}
	return resolved
}

// valueIdentity strips the alias hops and the `own` marker, leaving the type a
// method is declared for.
func (c *CapabilityClassifier) valueIdentity(id types.TypeID) types.TypeID {
	for range 32 {
		resolved := c.resolve(id)
		if resolved == types.NoTypeID {
			return types.NoTypeID
		}
		tt, ok := c.types.Lookup(resolved)
		if !ok || tt.Kind != types.KindOwn || tt.Elem == types.NoTypeID || tt.Elem == resolved {
			return resolved
		}
		id = tt.Elem
	}
	return c.resolve(id)
}
