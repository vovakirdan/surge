package mir

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/types"
)

func (l *funcLowerer) lowerStructLitExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.StructLitData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: struct lit: unexpected payload %T", e.Data)
	}
	fields := make([]StructLitField, 0, len(data.Fields))
	for _, f := range data.Fields {
		if f.Value == nil {
			continue
		}
		val, err := l.lowerExpr(f.Value, true)
		if err != nil {
			return Operand{}, err
		}
		fields = append(fields, StructLitField{Name: f.Name, Value: val})
	}
	tmp := l.newTemp(e.Type, "struct", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueStructLit, StructLit: StructLit{TypeID: data.TypeID, Fields: fields}},
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

// lowerArrayLitExpr lowers an array literal expression.
func (l *funcLowerer) lowerArrayLitExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.ArrayLitData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: array lit: unexpected payload %T", e.Data)
	}
	elems := make([]Operand, 0, len(data.Elements))
	for _, el := range data.Elements {
		op, err := l.lowerExpr(el, true)
		if err != nil {
			return Operand{}, err
		}
		elems = append(elems, op)
	}
	tmp := l.newTemp(e.Type, "arr", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueArrayLit, ArrayLit: ArrayLit{Elems: elems}},
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

// lowerMapLitExpr lowers a map literal expression by emitting rt_map_* calls.
func (l *funcLowerer) lowerMapLitExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.MapLitData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: map lit: unexpected payload %T", e.Data)
	}

	mapType := e.Type
	tmp := l.newTemp(mapType, "map", e.Span)
	l.emit(&Instr{
		Kind: InstrCall,
		Call: CallInstr{
			HasDst: true,
			Dst:    Place{Local: tmp},
			Callee: Callee{Kind: CalleeSym, Name: "rt_map_new"},
		},
	})

	refType := mapType
	if l.types != nil && mapType != types.NoTypeID {
		refType = l.types.Intern(types.MakeReference(mapType, true))
	}
	mapRef := Operand{Kind: OperandAddrOfMut, Type: refType, Place: Place{Local: tmp}}

	for _, entry := range data.Entries {
		keyOp, err := l.lowerExpr(entry.Key, true)
		if err != nil {
			return Operand{}, err
		}
		valOp, err := l.lowerExpr(entry.Value, true)
		if err != nil {
			return Operand{}, err
		}
		l.emit(&Instr{
			Kind: InstrCall,
			Call: CallInstr{
				HasDst: false,
				Callee: Callee{Kind: CalleeSym, Name: "rt_map_insert"},
				Args:   []Operand{mapRef, keyOp, valOp},
			},
		})
	}

	return l.placeOperand(Place{Local: tmp}, mapType, consume), nil
}

// lowerTupleLitExpr lowers a tuple literal expression.
func (l *funcLowerer) lowerTupleLitExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.TupleLitData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: tuple lit: unexpected payload %T", e.Data)
	}
	elems := make([]Operand, 0, len(data.Elements))
	for _, el := range data.Elements {
		op, err := l.lowerExpr(el, true)
		if err != nil {
			return Operand{}, err
		}
		elems = append(elems, op)
	}
	tmp := l.newTemp(e.Type, "tup", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueTupleLit, TupleLit: TupleLit{Elems: elems}},
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}
