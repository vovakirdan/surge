package mir

import (
	"reflect"
	"testing"

	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/types"
)

// A failed join cleans up a prepared move only when its payload needs Drop.
// The public plain-composite source test proves real lowering supplies these
// type flags; these rows also check the lazy and projected operand boundaries.
func TestScopeJoinDropsTransferredReturnOnCancellation(t *testing.T) {
	typesIn := types.NewInterner()
	typesIn.Strings = source.NewInterner()
	semaRes := &sema.Result{TypeInterner: typesIn}
	builtins := typesIn.Builtins()
	valueName := typesIn.Strings.Intern("value")
	makeStruct := func(name string, fields []types.StructField, isCopy bool) types.TypeID {
		ty := typesIn.RegisterStruct(typesIn.Strings.Intern(name), source.Span{})
		typesIn.SetStructFields(ty, fields)
		if isCopy {
			typesIn.MarkCopyType(ty)
		}
		return ty
	}
	counted := makeStruct("Counted", []types.StructField{{Name: valueName, Type: builtins.Int}}, true)
	plain := makeStruct("Plain", []types.StructField{{Name: valueName, Type: builtins.Int64}}, true)
	moveOnly := makeStruct("MoveOnly", []types.StructField{{Name: valueName, Type: builtins.Int64}}, false)
	mixed := makeStruct("Mixed", []types.StructField{
		{Name: valueName, Type: builtins.Int64},
		{Name: typesIn.Strings.Intern("owner"), Type: builtins.Int},
	}, true)
	ref := typesIn.Intern(types.MakeReference(counted, false))
	refMut := typesIn.Intern(types.MakeReference(counted, true))
	local := Place{Local: 2}
	field := Place{Local: 2, Proj: []PlaceProj{{Kind: PlaceProjField, FieldName: "value", FieldIdx: 0}}}
	for _, tc := range []struct {
		name       string
		kind       OperandKind
		place      Place
		localType  types.TypeID
		resultType types.TypeID
		wantFlags  LocalFlags
		hasValue   bool
		wantDrop   bool
	}{
		{"move-local", OperandMove, local, counted, counted, LocalFlagCopy | LocalFlagOwnsHeap, true, true},
		{"move-field", OperandMove, field, counted, builtins.Int, LocalFlagCopy | LocalFlagOwnsHeap, true, true},
		{"plain-copy-local", OperandMove, local, plain, plain, LocalFlagCopy, true, false},
		{"plain-copy-field", OperandMove, field, mixed, builtins.Int64, LocalFlagCopy, true, false},
		{"move-only-no-heap", OperandMove, local, moveOnly, moveOnly, 0, true, true},
		{"reference", OperandMove, local, ref, ref, LocalFlagCopy | LocalFlagRef, true, false},
		{"reference-mut", OperandMove, local, refMut, refMut, LocalFlagRefMut, true, false},
		{"borrow", OperandCopy, local, counted, counted, LocalFlagCopy | LocalFlagOwnsHeap, true, false},
		{"lazy-copy", OperandCopyValue, local, counted, counted, LocalFlagCopy | LocalFlagOwnsHeap, true, false},
		{"lazy-retain", OperandRetain, local, builtins.Int, builtins.Int, LocalFlagCopy | LocalFlagOwnsHeap, true, false},
		{"lazy-constant", OperandConst, local, builtins.Int, builtins.Int, LocalFlagCopy | LocalFlagOwnsHeap, true, false},
		{"address", OperandAddrOf, local, counted, ref, LocalFlagCopy | LocalFlagRef, true, false},
		{"no-value", OperandMove, local, counted, builtins.Nothing, LocalFlagCopy, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op := Operand{Kind: tc.kind, Type: tc.resultType, Place: tc.place}
			if tc.kind == OperandConst {
				op.Const = Const{Kind: ConstInt, Type: tc.resultType, IntValue: 7}
			}
			original := ReturnTerm{HasValue: tc.hasValue, Value: op}
			f := &Func{
				Result: tc.resultType,
				Locals: []Local{
					{Name: "scope", Type: builtins.Uint64, Flags: LocalFlagCopy},
					{Name: "join_failed", Type: builtins.Bool, Flags: LocalFlagCopy},
					{Name: "prepared", Type: tc.localType, Flags: localFlagsFor(typesIn, semaRes, tc.localType)},
				},
				Blocks: []Block{{Term: Terminator{Kind: TermReturn, Return: original}}},
			}
			flags := localFlagsFor(typesIn, semaRes, f.Result)
			if flags != tc.wantFlags {
				t.Fatalf("fixture result flags=%v, want %v", flags, tc.wantFlags)
			}
			if len(tc.place.Proj) != 0 {
				if ty, ok := placeTypeIn(typesIn, f, nil, tc.place); !ok || ty != f.Result {
					t.Fatal("projected fixture does not name its result type")
				}
			}
			insertScopeJoins(f, 0, 1, flags)
			if err := validateDrop(f, nil); err != nil {
				t.Fatalf("join inserted an invalid Drop: %v", err)
			}
			if f.Blocks[0].Term.Kind != TermGoto {
				t.Fatal("return did not pass through its implicit join")
			}
			join := f.Blocks[f.Blocks[0].Term.Goto.Target]
			if len(join.Instrs) != 1 || join.Instrs[0].Kind != InstrJoinAll {
				t.Fatal("missing implicit join instruction")
			}
			done := f.Blocks[join.Instrs[0].JoinAll.ReadyBB]
			if done.Term.Kind != TermIf {
				t.Fatal("join outcome did not select success or cancellation")
			}
			success := f.Blocks[done.Term.If.Else]
			cancel := f.Blocks[done.Term.If.Then]
			if success.Term.Kind != TermReturn || !reflect.DeepEqual(success.Term.Return, original) || len(success.Instrs) != 0 {
				t.Fatal("successful join changed the original return transfer")
			}
			if cancel.Term.Kind != TermReturn || !cancel.Term.Return.Cancelled || cancel.Term.Return.HasValue {
				t.Fatal("failed join did not return cancellation without a value")
			}
			if !tc.wantDrop {
				if len(cancel.Instrs) != 0 {
					t.Fatal("cancelled join consumed a value it did not own")
				}
				return
			}
			if len(cancel.Instrs) != 1 || cancel.Instrs[0].Kind != InstrDrop ||
				!reflect.DeepEqual(cancel.Instrs[0].Drop.Place, tc.place) || cancel.Instrs[0].Drop.Shallow {
				t.Fatal("cancelled join must deep-drop the prepared return owner exactly once")
			}
		})
	}
}
