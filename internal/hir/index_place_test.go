package hir_test

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/hir"
)

func TestCustomIndexExplicitBorrowKeepsExactCallAndReborrowsAliasCarrierPointee(t *testing.T) {
	module, _, err := parseAndLower(t, `
type Bag = { marker: int };
type IntRef = &int;

extern<Bag> {
    fn __index(self: &Bag, index: IntRef) -> IntRef {
        let _ = self;
        return index;
    }
}

fn read(bag: &Bag, index: IntRef) -> int {
    let value: &int = &bag[index];
    return *value;
}
`)
	if err != nil {
		t.Fatalf("lower custom index borrow: %v", err)
	}

	var read *hir.Func
	for _, fn := range module.Funcs {
		if fn != nil && fn.Name == "read" {
			read = fn
			break
		}
	}
	if read == nil || read.Body == nil || len(read.Body.Stmts) == 0 {
		t.Fatalf("missing read body: %+v", read)
	}
	let, ok := read.Body.Stmts[0].Data.(hir.LetData)
	if !ok || let.Value == nil || let.Value.Kind != hir.ExprUnaryOp {
		t.Fatalf("expected borrowed let value, got %+v", read.Body.Stmts[0])
	}
	outer := let.Value.Data.(hir.UnaryOpData)
	if outer.Op != ast.ExprUnaryRef || outer.Operand == nil || outer.Operand.Kind != hir.ExprUnaryOp {
		t.Fatalf("expected outer shared borrow, got %+v", outer)
	}
	deref := outer.Operand.Data.(hir.UnaryOpData)
	if deref.Op != ast.ExprUnaryDeref || deref.Operand == nil || deref.Operand.Kind != hir.ExprCall {
		t.Fatalf("expected shared reborrow of exact call result, got %+v", deref)
	}
	call := deref.Operand.Data.(hir.CallData)
	if !call.SymbolID.IsValid() {
		t.Fatal("custom __index call lost its selected symbol")
	}
	if call.Callee == nil || call.Callee.Kind != hir.ExprVarRef {
		t.Fatalf("expected exact callable var ref, got %+v", call.Callee)
	}
	callee := call.Callee.Data.(hir.VarRefData)
	if callee.Name != "__index" {
		t.Fatalf("callee = %q, want __index", callee.Name)
	}
}
