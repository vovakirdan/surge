package mir

import (
	"reflect"
	"testing"
)

// A moved return already owns the value that would be handed to its caller.
// A failed implicit join consumes that owner instead. Other operand forms
// only produce or duplicate their result if the successful return executes.
func TestScopeJoinDropsTransferredReturnOnCancellation(t *testing.T) {
	local := Place{Local: 2}
	field := Place{Local: 2, Proj: []PlaceProj{{Kind: PlaceProjField, FieldName: "value", FieldIdx: 0}}}
	for _, tc := range []struct {
		name     string
		operand  Operand
		hasValue bool
		wantDrop bool
	}{
		{"move-local", Operand{Kind: OperandMove, Place: local}, true, true},
		{"move-field", Operand{Kind: OperandMove, Place: field}, true, true},
		{"borrow", Operand{Kind: OperandCopy, Place: local}, true, false},
		{"lazy-copy", Operand{Kind: OperandCopyValue, Place: local}, true, false},
		{"lazy-retain", Operand{Kind: OperandRetain, Place: local}, true, false},
		{"lazy-constant", Operand{Kind: OperandConst, Const: Const{Kind: ConstInt, IntValue: 7}}, true, false},
		{"address", Operand{Kind: OperandAddrOf, Place: local}, true, false},
		{"no-value", Operand{Kind: OperandMove, Place: local}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := ReturnTerm{HasValue: tc.hasValue, Value: tc.operand}
			f := &Func{Blocks: []Block{{Term: Terminator{Kind: TermReturn, Return: original}}}}
			insertScopeJoins(f, 0, 1)
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
				!reflect.DeepEqual(cancel.Instrs[0].Drop.Place, tc.operand.Place) || cancel.Instrs[0].Drop.Shallow {
				t.Fatal("cancelled join must deep-drop the prepared return owner exactly once")
			}
		})
	}
}
