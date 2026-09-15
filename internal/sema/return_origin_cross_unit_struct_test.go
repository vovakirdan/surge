package sema

import (
	"testing"

	"surge/internal/symbols"
	"surge/internal/types"
)

// A consumer with no file of its own proves a foreign struct only through the
// one peer whose file is the declaration's file. Two such peers prove nothing.
func TestReturnOriginPlainStructUniquePeer(t *testing.T) {
	a := returnOriginConditionFixture(t, "pragma no_std;\ntype Plain = { n: uint };\nfn read(p: Plain) -> uint { return p.n; }\n")
	declaring := a.units[0]
	var info *types.StructInfo
	for _, sym := range declaring.Symbols.Table.Symbols.Data() {
		if name, _ := declaring.Builder.StringsInterner.Lookup(sym.Name); sym.Kind == symbols.SymbolType && name == "Plain" {
			info, _ = declaring.Sema.TypeInterner.StructInfo(sym.Type)
		}
	}
	if info == nil {
		t.Fatal("PRECONDITION: the declaring unit has no Plain struct")
	}
	consumer := &returnOriginFunction{unit: &returnOriginUnitIndex{ReturnOriginUnit: ReturnOriginUnit{Builder: declaring.Builder}}}
	if consumer.unit.Builder.Files.Get(consumer.unit.FileID) != nil {
		t.Fatal("PRECONDITION: the consumer must have no file of its own")
	}
	twin := *declaring
	for _, row := range []struct {
		name  string
		peers []*returnOriginUnitIndex
		want  bool
	}{
		{"unique_peer", []*returnOriginUnitIndex{declaring}, true},
		{"no_peer", nil, false},
		{"ambiguous_peers", []*returnOriginUnitIndex{declaring, &twin}, false},
	} {
		consumer.unit.peers = row.peers
		if got := returnOriginPlainStruct(consumer, info); got != row.want {
			t.Errorf("%s: plain struct certificate = %v, want %v", row.name, got, row.want)
		}
	}
}
