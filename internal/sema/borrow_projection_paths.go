package sema

import (
	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A `&mut` reference a call returns from a reference to a RECORD -- `fn
// it0(k: &mut H) -> &mut int { return &mut k.items[0]; }` -- points somewhere
// inside the referent, and the signature does not say where. Standing on a
// reborrow of the whole referent, it was excused as part of the reference's
// own chain, so `k.grow()`, `setH(k)`, `*k = mkH()` or `app(k.items)` grew or
// replaced the buffer it pointed into and the store through it wrote freed
// memory (valgrind: invalid read and write).
//
// Without a summary of the callee the path is unknown, so every place of the
// result's referent type inside the argument's referent is a candidate. A
// candidate reached through fields alone is a slot of the referent: a replace
// of the referent writes the slot anew and the reference still points at
// live storage. A candidate reached through a dynamic buffer (a dynamic
// array's element, a map's entry) or a tag's payload may be freed by a grow or
// a replace, and holds an exclusive loan on that element, as `let e = &mut
// k.items[0]` does. A reference taken later through the result (`&mut q[0]`,
// `firstm(q)`) is taken on each candidate.

// maxProjectionCandidates bounds the candidates named one by one. A referent
// with more (a record of records of the result's type) is answered by one
// interior loan on the whole referent instead: coarser, never absent.
const maxProjectionCandidates = 32

// projectionPaths names the candidate places a `&mut result` may point at
// inside a value of type given: slots (fields only) and buffers (cut at the
// first element of a dynamic buffer or a tag's payload). The given type itself
// is no candidate: a result of the argument's own referent type is the alias
// the reborrow rules already answer. overflow reports more candidates than
// maxProjectionCandidates, and then none are named.
func (tc *typeChecker) projectionPaths(given, result types.TypeID) (paths [][]PlaceSegment, overflow bool) {
	w := projectionWalk{
		tc:       tc,
		result:   tc.resolveAlias(result),
		counts:   make(map[types.TypeID]int),
		contains: make(map[types.TypeID]bool),
	}
	given = tc.resolveAlias(given)
	if given == w.result {
		return nil, false
	}
	if w.count(given) > maxProjectionCandidates {
		return nil, true
	}
	w.walk(given, nil)
	return append(w.slots, w.buffers...), false
}

// projectionWalk answers each type once: how many candidates it holds
// (saturated past the bound) and whether it may hold the result at all, so a
// record whose fields repeat one type is not walked once per path to it.
type projectionWalk struct {
	tc       *typeChecker
	result   types.TypeID
	counts   map[types.TypeID]int
	contains map[types.TypeID]bool
	slots    [][]PlaceSegment
	buffers  [][]PlaceSegment
}

// parts classifies t for the walk: an inline container (fixed array, record,
// tuple) whose parts are walked below their segments, or a buffer (dynamic
// array, map, tag payload) that may hold the result or not.
func (w *projectionWalk) parts(t types.TypeID) (inner []types.TypeID, segs []PlaceSegment, buffer, walk bool) {
	tc := w.tc
	if elem, _, fixed := tc.arrayFixedInfo(t); fixed {
		return []types.TypeID{elem}, []PlaceSegment{{Kind: PlaceSegmentIndex}}, false, true
	}
	if key, value, isMap := tc.types.MapInfo(t); isMap {
		return nil, nil, w.holds(key) || w.holds(value), false
	}
	if elem, isArray := tc.arrayElemType(t); isArray {
		return nil, nil, w.holds(elem), false
	}
	typ, ok := tc.types.Lookup(t)
	if !ok {
		return nil, nil, false, false
	}
	switch typ.Kind {
	case types.KindStruct:
		if tc.types.IsBorrowedView(t) || tc.types.IsRuntimeHandleType(t) {
			return nil, nil, false, false
		}
		for _, field := range tc.types.StructFields(t) {
			inner = append(inner, field.Type)
			segs = append(segs, PlaceSegment{Kind: PlaceSegmentField, Name: field.Name})
		}
		return inner, segs, false, true
	case types.KindTuple:
		if info, found := tc.types.TupleInfo(t); found && info != nil {
			for i, elem := range info.Elems {
				pos, err := safecast.Conv[uint32](i)
				if err != nil {
					break
				}
				inner = append(inner, elem)
				segs = append(segs, PlaceSegment{Kind: PlaceSegmentTupleIndex, Elem: pos})
			}
		}
		return inner, segs, false, true
	case types.KindUnion:
		// A tag's payload has no place segment of its own; a replace may
		// change the tag under the reference, so it is held as an element.
		return nil, nil, w.holds(t), false
	}
	return nil, nil, false, false
}

// count: the candidates inside t, saturated at maxProjectionCandidates+1.
func (w *projectionWalk) count(t types.TypeID) int {
	t = w.tc.resolveAlias(t)
	if t == types.NoTypeID {
		return 0
	}
	if t == w.result {
		return 1
	}
	if n, done := w.counts[t]; done {
		return n
	}
	w.counts[t] = 0 // a type met again below itself names nothing new
	inner, _, buffer, walk := w.parts(t)
	n := 0
	if buffer {
		n = 1
	}
	if walk {
		for _, part := range inner {
			n = min(n+w.count(part), maxProjectionCandidates+1)
		}
	}
	w.counts[t] = n
	return n
}

func (w *projectionWalk) walk(t types.TypeID, path []PlaceSegment) {
	t = w.tc.resolveAlias(t)
	if w.count(t) == 0 {
		return
	}
	if t == w.result {
		w.slots = append(w.slots, path)
		return
	}
	inner, segs, buffer, walk := w.parts(t)
	if buffer {
		w.buffers = append(w.buffers, extendPath(path, PlaceSegment{Kind: PlaceSegmentIndex}))
	}
	if walk {
		for i, part := range inner {
			w.walk(part, extendPath(path, segs[i]))
		}
	}
}

// holds: a value of type t may hold a value of the result's type.
func (w *projectionWalk) holds(t types.TypeID) bool {
	t = w.tc.resolveAlias(t)
	if found, done := w.contains[t]; done {
		return found
	}
	found := w.tc.typeContains(t, w.result)
	w.contains[t] = found
	return found
}

func extendPath(path []PlaceSegment, seg PlaceSegment) []PlaceSegment {
	out := make([]PlaceSegment, 0, len(path)+1)
	out = append(out, path...)
	return append(out, seg)
}

// typeContains: a value of type t may hold a value of type want.
func (tc *typeChecker) typeContains(t, want types.TypeID) bool {
	want = tc.resolveAlias(want)
	return tc.typeFinds(t, func(id types.TypeID, _ types.Type) (bool, bool) {
		return tc.resolveAlias(id) == want, true
	}, nil)
}

// placeValueType is the type of the value a loan place names: the base
// binding's value (through its reference) and each segment below it, or
// NoTypeID when a step cannot be told.
func (tc *typeChecker) placeValueType(place Place) types.TypeID {
	if tc.borrow == nil || !place.IsValid() {
		return types.NoTypeID
	}
	t := tc.valueType(tc.bindingType(place.Base))
	for _, seg := range tc.borrow.placeSegments(place) {
		if t == types.NoTypeID {
			return types.NoTypeID
		}
		switch seg.Kind {
		case PlaceSegmentField:
			t = tc.structFieldType(t, seg.Name)
		case PlaceSegmentIndex:
			if _, value, isMap := tc.types.MapInfo(t); isMap {
				t = value
			} else if elem, ok := tc.arrayElemType(t); ok {
				t = elem
			} else {
				return types.NoTypeID
			}
		case PlaceSegmentTupleIndex:
			info, ok := tc.types.TupleInfo(tc.resolveAlias(t))
			if !ok || info == nil || int(seg.Elem) >= len(info.Elems) {
				return types.NoTypeID
			}
			t = info.Elems[seg.Elem]
		case PlaceSegmentDeref:
		}
		t = tc.valueType(t)
	}
	return t
}

func (tc *typeChecker) structFieldType(t types.TypeID, name source.StringID) types.TypeID {
	for _, field := range tc.types.StructFields(tc.resolveAlias(t)) {
		if field.Name == name {
			return field.Type
		}
	}
	return types.NoTypeID
}

// projectionSiblingLoans: a loan the binding holds that was taken through a
// projection with several candidates -- `let e = &mut q[0]`, `firstm(q)`
// with `let q = fsH(k)` -- names one candidate only. It is taken again, with
// the same kind and the same path below the candidate, on each other one, so
// a grow of any candidate's buffer is refused while the binding lives.
func (tc *typeChecker) projectionSiblingLoans(symID symbols.SymbolID, own BorrowID, span source.Span) []BorrowID {
	if len(tc.projectionSiblings) == 0 {
		return nil
	}
	scope := tc.currentScope()
	if !scope.IsValid() {
		return nil
	}
	var out []BorrowID
	for _, bid := range append([]BorrowID{own}, tc.viewLoans[symID]...) {
		info := tc.borrow.Info(bid)
		if info == nil {
			continue
		}
		for anc := tc.borrow.parentOf(bid); anc != NoBorrowID; anc = tc.borrow.parentOf(anc) {
			siblings := tc.projectionSiblings[anc]
			held := tc.borrow.Info(anc)
			if len(siblings) == 0 || held == nil || !placeCovers(held.Place, info.Place) {
				continue
			}
			suffix := tc.borrow.placeSegments(info.Place)[len(tc.borrow.placeSegments(held.Place)):]
			for _, sib := range siblings {
				at := tc.borrow.Info(sib)
				if at == nil {
					continue
				}
				place := tc.borrow.CanonicalPlace(at.Place.Base, append(tc.borrow.placeSegments(at.Place), suffix...))
				loan, issue := tc.beginSideLoan(info.Life.FromExpr, span, info.Kind, place, scope, sib)
				if issue.Kind != BorrowIssueNone {
					tc.reportBorrowConflict(place, span, issue, info.Kind)
					return out
				}
				out = append(out, loan)
			}
			break
		}
	}
	return out
}

// beginSideLoan takes a loan keyed by an expression that already names another
// loan (the one it accompanies), and leaves that expression naming the other.
func (tc *typeChecker) beginSideLoan(key ast.ExprID, span source.Span, kind BorrowKind, place Place, scope symbols.ScopeID, parent BorrowID) (BorrowID, BorrowIssue) {
	prev, had := tc.borrow.exprBorrow[key]
	bid, issue := tc.borrow.BeginBorrow(key, span, kind, place, scope, parent)
	if had {
		tc.borrow.exprBorrow[key] = prev
	} else if bid != NoBorrowID {
		delete(tc.borrow.exprBorrow, key)
	}
	return bid, issue
}
