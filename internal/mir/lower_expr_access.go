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
	obj, err := l.lowerExpr(data.Object, false)
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
	obj, err := l.lowerExpr(data.Object, false)
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
