package mir

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/types"
)

func (l *funcLowerer) lowerFieldAccessExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.FieldAccessData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: field: unexpected payload %T", e.Data)
	}
	if l.types != nil && e.Type != types.NoTypeID {
		if tt, ok := l.types.Lookup(e.Type); ok && tt.Kind == types.KindReference {
			// If the field itself is already a reference, pass it as a value to avoid &&T.
			fieldTy := l.fieldAccessType(data.Object, data.FieldName, data.FieldIdx)
			if fieldTy == types.NoTypeID || !l.isRefType(fieldTy) {
				place, err := l.lowerPlace(e)
				if err != nil {
					return Operand{}, err
				}
				kind := OperandAddrOf
				if tt.Mutable {
					kind = OperandAddrOfMut
				}
				return Operand{Kind: kind, Type: e.Type, Place: place}, nil
			}
		}
	}
	obj, err := l.lowerValueExpr(data.Object, false)
	if err != nil {
		return Operand{}, err
	}
	tmp := l.newTemp(e.Type, "field", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{
				Kind: RValueField,
				Field: FieldAccess{
					Object:    obj,
					FieldName: data.FieldName,
					FieldIdx:  data.FieldIdx,
				},
			},
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

func (l *funcLowerer) fieldAccessType(obj *hir.Expr, fieldName string, fieldIdx int) types.TypeID {
	if l == nil || l.types == nil || obj == nil || obj.Type == types.NoTypeID {
		return types.NoTypeID
	}
	objType := resolveAlias(l.types, obj.Type)
	if tt, ok := l.types.Lookup(objType); ok && tt.Kind == types.KindReference {
		objType = tt.Elem
	}
	info, ok := l.types.StructInfo(objType)
	if !ok || info == nil {
		return types.NoTypeID
	}
	if fieldIdx >= 0 && fieldIdx < len(info.Fields) {
		return info.Fields[fieldIdx].Type
	}
	if fieldName == "" || l.types.Strings == nil {
		return types.NoTypeID
	}
	for _, f := range info.Fields {
		name, ok := l.types.Strings.Lookup(f.Name)
		if ok && name == fieldName {
			return f.Type
		}
	}
	return types.NoTypeID
}

func (l *funcLowerer) isRefType(id types.TypeID) bool {
	if l == nil || l.types == nil || id == types.NoTypeID {
		return false
	}
	id = resolveAlias(l.types, id)
	if tt, ok := l.types.Lookup(id); ok {
		return tt.Kind == types.KindReference
	}
	return false
}

// lowerIndexExpr lowers an index expression.
func (l *funcLowerer) lowerIndexExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.IndexData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: index: unexpected payload %T", e.Data)
	}
	if l.types != nil && e.Type != types.NoTypeID {
		if tt, ok := l.types.Lookup(e.Type); ok && tt.Kind == types.KindReference {
			place, err := l.lowerPlace(e)
			if err != nil {
				return Operand{}, err
			}
			kind := OperandAddrOf
			if tt.Mutable {
				kind = OperandAddrOfMut
			}
			return Operand{Kind: kind, Type: e.Type, Place: place}, nil
		}
	}
	obj, err := l.lowerValueExpr(data.Object, false)
	if err != nil {
		return Operand{}, err
	}
	idx, err := l.lowerValueExpr(data.Index, false)
	if err != nil {
		return Operand{}, err
	}
	tmp := l.newTemp(e.Type, "idx", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueIndex, Index: IndexAccess{Object: obj, Index: idx}},
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

// lowerStructLitExpr lowers a struct literal expression.
