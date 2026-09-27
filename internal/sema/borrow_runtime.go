package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
)

type placeDescriptor struct {
	Base     symbols.SymbolID
	Segments []PlaceSegment
}

func (tc *typeChecker) canonicalPlace(desc placeDescriptor) Place {
	if tc.borrow == nil || !desc.Base.IsValid() {
		return Place{}
	}
	return tc.borrow.CanonicalPlace(desc.Base, desc.Segments)
}

// loanPlace is the place a loan is taken on, or a borrow conflict is asked
// about, for a place as written: expanded through the loans its reference
// bindings stand on, with each reference's explicit `*` dropped. The
// dereference of a reference is implicit at an index or a field -- `r[0]` IS
// `(*r)[0]` -- and keeping the `*` named one element by two paths no prefix
// relates (`r[..]` and `r.*[..]`), so the view held by one did not refuse the
// exclusive borrow of the other.
//
// Only loans and their conflicts are asked in this spelling. A binding's own
// move, drop and initialization state keeps canonicalPlace: `*r = v` stores
// into r's referent and must not read as a store over the binding r, which
// would revive a reference that was moved or dropped.
func (tc *typeChecker) loanPlace(desc placeDescriptor) (Place, BorrowID) {
	desc, parent := tc.expandPlaceDescriptorWith(desc, true)
	desc.Segments = tc.referentSegments(desc.Base, desc.Segments)
	return tc.canonicalPlace(desc), parent
}

func (tc *typeChecker) referentSegments(base symbols.SymbolID, segs []PlaceSegment) []PlaceSegment {
	if len(segs) > 0 && segs[0].Kind == PlaceSegmentDeref && tc.isReferenceType(tc.bindingType(base)) {
		return segs[1:]
	}
	return segs
}

func (tc *typeChecker) expandPlaceDescriptor(desc placeDescriptor) (placeDescriptor, BorrowID) {
	return tc.expandPlaceDescriptorWith(desc, false)
}

// expandPlaceDescriptorWith follows the loans reference bindings stand on;
// referent drops each reference's explicit `*` on the way (loanPlace).
func (tc *typeChecker) expandPlaceDescriptorWith(desc placeDescriptor, referent bool) (placeDescriptor, BorrowID) {
	if tc == nil || tc.borrow == nil {
		return desc, NoBorrowID
	}
	if tc.bindingBorrow == nil {
		return desc, NoBorrowID
	}
	visited := make(map[symbols.SymbolID]struct{})
	var parent BorrowID
	for {
		if !desc.Base.IsValid() {
			return desc, parent
		}
		if _, ok := visited[desc.Base]; ok {
			return desc, parent
		}
		visited[desc.Base] = struct{}{}
		bid := tc.bindingBorrow[desc.Base]
		if bid == NoBorrowID {
			return desc, parent
		}
		info := tc.borrow.Info(bid)
		if info == nil {
			return desc, parent
		}
		if parent == NoBorrowID {
			parent = bid
		}
		segs := desc.Segments
		if referent {
			segs = tc.referentSegments(desc.Base, segs)
		}
		baseSegs := tc.borrow.placeSegments(info.Place)
		desc = placeDescriptor{
			Base:     info.Place.Base,
			Segments: append(baseSegs, segs...),
		}
	}
}

func (tc *typeChecker) exprSpan(id ast.ExprID) source.Span {
	if !id.IsValid() || tc.builder == nil || tc.builder.Exprs == nil {
		return source.Span{}
	}
	expr := tc.builder.Exprs.Get(id)
	if expr == nil {
		return source.Span{}
	}
	return expr.Span
}

func (tc *typeChecker) resolvePlace(expr ast.ExprID) (placeDescriptor, bool) {
	if !expr.IsValid() || tc.builder == nil {
		return placeDescriptor{}, false
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return placeDescriptor{}, false
	}
	switch node.Kind {
	case ast.ExprIdent:
		symID := tc.symbolForExpr(expr)
		if !symID.IsValid() {
			return placeDescriptor{}, false
		}
		sym := tc.symbolFromID(symID)
		if sym == nil {
			return placeDescriptor{}, false
		}
		if sym.Kind != symbols.SymbolLet && sym.Kind != symbols.SymbolParam {
			return placeDescriptor{}, false
		}
		return placeDescriptor{Base: symID}, true
	case ast.ExprMember:
		member, ok := tc.builder.Exprs.Member(expr)
		if !ok || member == nil {
			return placeDescriptor{}, false
		}
		desc, ok := tc.resolvePlace(member.Target)
		if !ok {
			return placeDescriptor{}, false
		}
		desc.Segments = append(desc.Segments, PlaceSegment{
			Kind: PlaceSegmentField,
			Name: member.Field,
		})
		return desc, true
	case ast.ExprTupleIndex:
		// A tuple element is a projection like any other. Without this case
		// `resolvePlace` gives up on `p.0`, and every rule that reasons about
		// places — the borrow table and the partial-move gate alike — simply
		// never sees it.
		tup, ok := tc.builder.Exprs.TupleIndex(expr)
		if !ok || tup == nil {
			return placeDescriptor{}, false
		}
		desc, ok := tc.resolvePlace(tup.Target)
		if !ok {
			return placeDescriptor{}, false
		}
		desc.Segments = append(desc.Segments, PlaceSegment{Kind: PlaceSegmentTupleIndex, Elem: uint32(tup.Index)})
		return desc, true
	case ast.ExprIndex:
		index, ok := tc.builder.Exprs.Index(expr)
		if !ok || index == nil {
			return placeDescriptor{}, false
		}
		desc, ok := tc.resolvePlace(index.Target)
		if !ok {
			return placeDescriptor{}, false
		}
		desc.Segments = append(desc.Segments, PlaceSegment{Kind: PlaceSegmentIndex})
		return desc, true
	case ast.ExprGroup:
		group, ok := tc.builder.Exprs.Group(expr)
		if !ok || group == nil {
			return placeDescriptor{}, false
		}
		return tc.resolvePlace(group.Inner)
	case ast.ExprUnary:
		unary, ok := tc.builder.Exprs.Unary(expr)
		if !ok || unary == nil {
			return placeDescriptor{}, false
		}
		if unary.Op == ast.ExprUnaryOwn {
			return tc.resolvePlace(unary.Operand)
		}
		if unary.Op != ast.ExprUnaryDeref {
			return placeDescriptor{}, false
		}
		desc, ok := tc.resolvePlace(unary.Operand)
		if !ok {
			return placeDescriptor{}, false
		}
		desc.Segments = append(desc.Segments, PlaceSegment{Kind: PlaceSegmentDeref})
		return desc, true
	default:
		return placeDescriptor{}, false
	}
}

func (tc *typeChecker) symbolForExpr(id ast.ExprID) symbols.SymbolID {
	if tc.symbols == nil || tc.symbols.ExprSymbols == nil {
		return symbols.NoSymbolID
	}
	if sym, ok := tc.symbols.ExprSymbols[id]; ok {
		return sym
	}
	return symbols.NoSymbolID
}
