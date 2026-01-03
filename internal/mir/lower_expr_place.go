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
		return Place{Local: NoLocalID}, fmt.Errorf("mir: unknown local symbol %d (%s)", data.SymbolID, data.Name)

	case hir.ExprUnaryOp:
		data, ok := e.Data.(hir.UnaryOpData)
		if !ok {
			return Place{Local: NoLocalID}, fmt.Errorf("mir: unary: unexpected payload %T", e.Data)
		}
		if data.Op != ast.ExprUnaryDeref {
			return Place{Local: NoLocalID}, fmt.Errorf("mir: expected place, got UnaryOp %s", data.Op)
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
