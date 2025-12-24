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

func (tc *typeChecker) expandPlaceDescriptor(desc placeDescriptor) (placeDescriptor, BorrowID) {
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
		baseSegs := tc.borrow.placeSegments(info.Place)
		desc = placeDescriptor{
			Base:     info.Place.Base,
			Segments: append(baseSegs, desc.Segments...),
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
