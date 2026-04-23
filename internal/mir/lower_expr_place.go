package mir

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/types"
)

// lowerPlace lowers a HIR expression to a place (assignable location).
func (l *funcLowerer) lowerPlace(e *hir.Expr) (Place, error) {
	if l == nil || e == nil {
		return Place{Local: NoLocalID}, fmt.Errorf("mir: expected place, got <nil>")
	}
	switch e.Kind {
	case hir.ExprVarRef:
		data, ok := e.Data.(hir.VarRefData)
		if !ok {
			return Place{Local: NoLocalID}, fmt.Errorf("mir: var ref: unexpected payload %T", e.Data)
		}
		if !data.SymbolID.IsValid() {
			return Place{Local: NoLocalID}, fmt.Errorf("mir: var ref %q has no symbol id", data.Name)
		}
		if l.consts != nil {
			if decl := l.consts[data.SymbolID]; decl != nil {
				return Place{Local: NoLocalID}, fmt.Errorf("mir: const %q is not assignable", decl.Name)
			}
		}
		local, ok := l.symToLocal[data.SymbolID]
		if ok {
			return Place{Local: local}, nil
		}
		if l.symToGlobal != nil {
			if global, ok := l.symToGlobal[data.SymbolID]; ok {
				return Place{Kind: PlaceGlobal, Global: global}, nil
			}
		}
		funcName := ""
		if l.f != nil {
			funcName = l.f.Name
		}
		if funcName != "" {
			return Place{Local: NoLocalID}, fmt.Errorf("mir: unknown local symbol %d (%s) in %s", data.SymbolID, data.Name, funcName)
		}
		return Place{Local: NoLocalID}, fmt.Errorf("mir: unknown local symbol %d (%s)", data.SymbolID, data.Name)

	case hir.ExprUnaryOp:
		data, ok := e.Data.(hir.UnaryOpData)
		if !ok {
			return Place{Local: NoLocalID}, fmt.Errorf("mir: unary: unexpected payload %T", e.Data)
		}
		if data.Op != ast.ExprUnaryDeref {
			return Place{Local: NoLocalID}, fmt.Errorf("mir: expected place, got UnaryOp %s", data.Op)
		}
		if data.Operand != nil && data.Operand.Kind == hir.ExprIndex {
			// Index expressions already lower to element places, so deref is redundant here.
			return l.lowerPlace(data.Operand)
		}
		base, err := l.lowerPlace(data.Operand)
		if err != nil {
			return Place{Local: NoLocalID}, err
		}
		base.Proj = append(base.Proj, PlaceProj{Kind: PlaceProjDeref})
		return base, nil

	case hir.ExprFieldAccess:
		data, ok := e.Data.(hir.FieldAccessData)
		if !ok {
			return Place{Local: NoLocalID}, fmt.Errorf("mir: field: unexpected payload %T", e.Data)
		}
		base, err := l.lowerPlace(data.Object)
		if err != nil {
			return Place{Local: NoLocalID}, err
		}
		if l.needsDerefForRefObject(data.Object) {
			base.Proj = append(base.Proj, PlaceProj{Kind: PlaceProjDeref})
		}
		base.Proj = append(base.Proj, PlaceProj{
			Kind:      PlaceProjField,
			FieldName: data.FieldName,
			FieldIdx:  data.FieldIdx,
		})
		return base, nil

	case hir.ExprIndex:
		data, ok := e.Data.(hir.IndexData)
		if !ok {
			return Place{Local: NoLocalID}, fmt.Errorf("mir: index: unexpected payload %T", e.Data)
		}
		if l.types != nil && e.Type != types.NoTypeID {
			if tt, ok := l.types.Lookup(resolveAlias(l.types, e.Type)); ok && tt.Kind != types.KindReference {
				return Place{Local: NoLocalID}, fmt.Errorf("mir: expected place, got index result type %s", tt.Kind.String())
			}
		}
		base, err := l.lowerPlace(data.Object)
		if err != nil {
			return Place{Local: NoLocalID}, err
		}
		if l.needsDerefForRefObject(data.Object) {
			base.Proj = append(base.Proj, PlaceProj{Kind: PlaceProjDeref})
		}
		idxOp, err := l.lowerValueExpr(data.Index, true)
		if err != nil {
			return Place{Local: NoLocalID}, err
		}

		idxTmp := l.newTemp(idxOp.Type, "idx", e.Span)
		l.emit(&Instr{
			Kind: InstrAssign,
			Assign: AssignInstr{
				Dst: Place{Local: idxTmp},
				Src: RValue{Kind: RValueUse, Use: idxOp},
			},
		})

		base.Proj = append(base.Proj, PlaceProj{
			Kind:       PlaceProjIndex,
			IndexLocal: idxTmp,
		})
		return base, nil

	default:
		return Place{Local: NoLocalID}, fmt.Errorf("mir: expected place, got %s", e.Kind)
	}
}

func (l *funcLowerer) needsDerefForRefObject(e *hir.Expr) bool {
	if l == nil || l.types == nil || e == nil || e.Type == types.NoTypeID {
		return false
	}
	tt, ok := l.types.Lookup(resolveAlias(l.types, e.Type))
	if !ok || tt.Kind != types.KindReference {
		return false
	}
	switch e.Kind {
	case hir.ExprIndex, hir.ExprFieldAccess:
		if elem, ok := l.types.Lookup(resolveAlias(l.types, tt.Elem)); ok && elem.Kind == types.KindReference {
			return true
		}
		return false
	default:
		return true
	}
}

func (l *funcLowerer) reborrowPlaceNeedsDeref(place Place) bool {
	ty, ok := l.placeType(place)
	if !ok {
		return true
	}
	tt, ok := l.types.Lookup(resolveAlias(l.types, ty))
	return ok && tt.Kind == types.KindReference && tt.Mutable
}

func (l *funcLowerer) placeType(place Place) (types.TypeID, bool) {
	if l == nil || l.types == nil || !place.IsValid() {
		return types.NoTypeID, false
	}

	var cur types.TypeID
	switch place.Kind {
	case PlaceLocal:
		idx := int(place.Local)
		if l.f == nil || idx < 0 || idx >= len(l.f.Locals) {
			return types.NoTypeID, false
		}
		cur = l.f.Locals[idx].Type
	case PlaceGlobal:
		idx := int(place.Global)
		if l.out == nil || idx < 0 || idx >= len(l.out.Globals) {
			return types.NoTypeID, false
		}
		cur = l.out.Globals[idx].Type
	default:
		return types.NoTypeID, false
	}

	for _, proj := range place.Proj {
		next, ok := l.projectedPlaceType(cur, proj)
		if !ok {
			return types.NoTypeID, false
		}
		cur = next
	}
	return cur, true
}

func (l *funcLowerer) projectedPlaceType(cur types.TypeID, proj PlaceProj) (types.TypeID, bool) {
	switch proj.Kind {
	case PlaceProjDeref:
		return l.derefPlaceType(cur)
	case PlaceProjField:
		return l.fieldPlaceType(cur, proj)
	case PlaceProjIndex:
		return l.indexPlaceType(cur)
	default:
		return types.NoTypeID, false
	}
}

func (l *funcLowerer) derefPlaceType(id types.TypeID) (types.TypeID, bool) {
	tt, ok := l.types.Lookup(resolveAlias(l.types, id))
	if !ok {
		return types.NoTypeID, false
	}
	switch tt.Kind {
	case types.KindOwn, types.KindPointer, types.KindReference:
		return tt.Elem, true
	default:
		return types.NoTypeID, false
	}
}

func (l *funcLowerer) fieldPlaceType(id types.TypeID, proj PlaceProj) (types.TypeID, bool) {
	id = resolveAliasType(l.types, id)
	if info, ok := l.types.StructInfo(id); ok && info != nil {
		fieldIdx := proj.FieldIdx
		if fieldIdx >= 0 && fieldIdx < len(info.Fields) {
			return info.Fields[fieldIdx].Type, true
		}
		if proj.FieldName != "" && l.types.Strings != nil {
			for i, field := range info.Fields {
				name, ok := l.types.Strings.Lookup(field.Name)
				if ok && name == proj.FieldName {
					fieldIdx = i
					break
				}
			}
		}
		if fieldIdx >= 0 && fieldIdx < len(info.Fields) {
			return info.Fields[fieldIdx].Type, true
		}
	}
	if info, ok := l.types.TupleInfo(id); ok && info != nil {
		fieldIdx := proj.FieldIdx
		if fieldIdx >= 0 && fieldIdx < len(info.Elems) {
			return info.Elems[fieldIdx], true
		}
	}
	return types.NoTypeID, false
}

func (l *funcLowerer) indexPlaceType(id types.TypeID) (types.TypeID, bool) {
	id = resolveAliasType(l.types, id)
	if elem, ok := l.types.ArrayInfo(id); ok {
		return elem, true
	}
	if elem, _, ok := l.types.ArrayFixedInfo(id); ok {
		return elem, true
	}
	tt, ok := l.types.Lookup(id)
	if !ok || tt.Kind != types.KindArray {
		return types.NoTypeID, false
	}
	return tt.Elem, true
}
