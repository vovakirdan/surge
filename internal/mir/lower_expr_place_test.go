package mir

import (
	"testing"

	"surge/internal/hir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestLowerPlaceMaterializesReferenceReturningCall(t *testing.T) {
	typesIn := types.NewInterner()
	refType := typesIn.Intern(types.MakeReference(typesIn.Builtins().Int, false))
	l := &funcLowerer{
		types:      typesIn,
		symToLocal: make(map[symbols.SymbolID]LocalID),
		nextTemp:   1,
		f:          &Func{Name: "test"},
	}
	l.cur = l.newBlock()
	l.f.Entry = l.cur
	expr := &hir.Expr{
		Kind: hir.ExprCall,
		Type: refType,
		Data: hir.CallData{
			Callee: &hir.Expr{
				Kind: hir.ExprVarRef,
				Data: hir.VarRefData{Name: "ref_fn"},
			},
		},
	}

	place, err := l.lowerPlace(expr)
	if err != nil {
		t.Fatalf("lower reference-returning call as place: %v", err)
	}
	if place.Local == NoLocalID || len(place.Proj) != 0 {
		t.Fatalf("call place = %+v, want materialized reference local", place)
	}
	block := &l.f.Blocks[l.cur]
	if len(block.Instrs) != 1 || block.Instrs[0].Kind != InstrCall || !block.Instrs[0].Call.HasDst || block.Instrs[0].Call.Dst.Local != place.Local {
		t.Fatalf("reference call materialization = %+v", block.Instrs)
	}
}
