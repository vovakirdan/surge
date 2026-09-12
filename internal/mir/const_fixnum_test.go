package mir

import (
	"testing"

	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/types"
)

func TestConstFixnumClassificationKeepsTypeAndText(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	b := in.Builtins()
	alias := in.RegisterAlias(in.Strings.Intern("Count"), source.Span{})
	in.SetAliasTarget(alias, b.Int)
	for _, row := range []struct {
		name string
		kind ConstKind
		ty   types.TypeID
		text string
		want bool
	}{
		{"int", ConstInt, b.Int, "42", true},
		{"uint", ConstUint, b.Uint, "42", true},
		{"alias", ConstInt, alias, "42", true},
		{"own", ConstInt, in.Intern(types.Type{Kind: types.KindOwn, Elem: b.Int}), "42", true},
		{"reference", ConstInt, in.Intern(types.MakeReference(b.Int, false)), "42", false},
		{"pointer", ConstInt, in.Intern(types.Type{Kind: types.KindPointer, Elem: b.Int}), "42", false},
		{"fixed-int", ConstInt, b.Int64, "42", false},
		{"fixed-uint", ConstUint, b.Uint64, "42", false},
		{"float", ConstFloat, b.Float, "0", false},
		{"signed-kind-uint-type", ConstInt, b.Uint, "42", false},
		{"unsigned-kind-int-type", ConstUint, b.Int, "42", false},
		{"int-overflow-zero-field", ConstInt, b.Int, "18446744073709551616", false},
		{"uint-overflow-zero-field", ConstUint, b.Uint, "18446744073709551616", false},
		{"missing-type", ConstInt, types.NoTypeID, "0", false},
		{"synthesized-zero", ConstInt, b.Int, "", true},
	} {
		t.Run(row.name, func(t *testing.T) {
			c := Const{Kind: row.kind, Type: row.ty, Text: row.text}
			if got := ConstFoldsToFixnum(in, &c); got != row.want {
				t.Fatalf("classification=%v, want %v for %+v", got, row.want, c)
			}
		})
	}
	if ConstFoldsToFixnum(nil, &Const{}) || ConstFoldsToFixnum(in, nil) {
		t.Fatal("absent type/constant cannot prove a fixnum")
	}
}

func TestMaterializeNumericLiteralOwnership(t *testing.T) {
	in := types.NewInterner()
	b := in.Builtins()
	for _, row := range []struct {
		name         string
		kind         ConstKind
		ty           types.TypeID
		text         string
		materialized bool
	}{
		{"inline-int", ConstInt, b.Int, "4611686018427387903", false},
		{"inline-uint", ConstUint, b.Uint, "9223372036854775807", false},
		{"heap-int", ConstInt, b.Int, "4611686018427387904", true},
		{"heap-uint", ConstUint, b.Uint, "9223372036854775808", true},
		{"wide-int", ConstInt, b.Int, "18446744073709551616", true},
		{"wide-uint", ConstUint, b.Uint, "18446744073709551616", true},
		{"float", ConstFloat, b.Float, "1.5", true},
		{"fixed", ConstInt, b.Int64, "42", false},
	} {
		t.Run(row.name, func(t *testing.T) {
			l := &funcLowerer{types: in, sema: &sema.Result{TypeInterner: in},
				f: &Func{Name: "literal_owner"}, pendingReleaseGuard: NoLocalID}
			l.cur = l.newBlock()
			l.pushTempDropFrame()
			op := Operand{Kind: OperandConst, Type: row.ty, Const: Const{Kind: row.kind, Type: row.ty, Text: row.text}}
			got := l.materializeOwnedConst(&op, source.Span{}, false)
			if !row.materialized {
				if got.Kind != OperandConst || len(l.f.Locals) != 0 || len(l.curBlock().Instrs) != 0 || l.hasPendingTempDrops() {
					t.Fatal("inline literal acquired temporary storage or an owner")
				}
				return
			}
			// consume=false reads the owner without transferring another count.
			// The consuming read is checked separately against the same place.
			if got.Kind != OperandCopy || len(l.f.Locals) != 1 || len(l.curBlock().Instrs) != 1 || !l.hasPendingTempDrops() {
				t.Fatalf("allocating literal lacks one temporary owner: operand=%v locals=%d instructions=%d", got.Kind, len(l.f.Locals), len(l.curBlock().Instrs))
			}
			if l.placeOperand(got.Place, got.Type, true).Kind != OperandRetain {
				t.Fatal("consuming the counted literal did not acquire its reference")
			}
			local := l.f.Locals[got.Place.Local]
			if local.Flags&(LocalFlagCopy|LocalFlagOwnsHeap) != LocalFlagCopy|LocalFlagOwnsHeap {
				t.Fatalf("literal owner flags=%v", local.Flags)
			}
			l.flushTempDropFrame()
			ins := l.curBlock().Instrs
			if len(ins) != 2 || ins[1].Kind != InstrDrop || ins[1].Drop.Place.Local != got.Place.Local {
				t.Fatal("literal owner did not receive exactly one statement-end release")
			}
		})
	}
}
