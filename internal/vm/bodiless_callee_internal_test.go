package vm

import (
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/symbols"
)

// A call of a function declared without a body that is not @intrinsic stops
// with a named error instead of running whichever builtin shares its name
// (a user `__index` used to run the array indexer on a struct).
func TestBodilessCalleeIsRefusedByName(t *testing.T) {
	const declared, intrinsic = symbols.SymbolID(7), symbols.SymbolID(8)
	vm := &VM{M: &mir.Module{BodilessDecls: map[symbols.SymbolID]string{declared: "__index"}}}
	vm.eb = &errorBuilder{vm: vm}
	err := vm.refuseBodilessCallee(declared)
	if err == nil || err.Code != PanicBodilessCallee || !strings.Contains(err.Message, "__index") {
		t.Fatalf("body-less callee was not refused by name: %+v", err)
	}
	if err := vm.refuseBodilessCallee(intrinsic); err != nil {
		t.Fatalf("a callee outside the set was refused: %+v", err)
	}
	if err := vm.refuseBodilessCallee(symbols.NoSymbolID); err != nil {
		t.Fatalf("a call without a symbol was refused: %+v", err)
	}
}
