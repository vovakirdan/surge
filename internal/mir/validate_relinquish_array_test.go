package mir

import (
	"strings"
	"testing"

	"surge/internal/types"
)

// The second reason a value is subject to the relinquishing rules: it carries a
// DYNAMIC ARRAY. Nothing about privacy is at stake -- an `int[]`'s elements are
// plain words -- but only the runtime's view registry can say whether the array
// in hand is a view into a buffer the origin shard keeps reading, so the sink
// must still carry the instruction that shows it to the runtime.
//
// The rows are built by hand for the same reason the counted rows next door
// are: what they check is the RULE, and a passing program would only show one
// lowering's habit.

// arrayRelinquishFixture is one function whose L0 is an `int[]`, L1 an `int`
// and L2 a second `int[]`; the caller fills bb0 with the instructions its row
// is about. It hands back the interner, the array type and the plain type.
func arrayRelinquishFixture(t *testing.T) (*Func, *types.Interner, types.TypeID, types.TypeID) {
	t.Helper()
	ot := newOwnershipTestTypes(t)
	intArray := ot.in.Intern(types.MakeArray(ot.in.Builtins().Int, types.ArrayDynamicLength))
	// The premise every row below rests on: the array is exactly the shape the
	// old counted-block gate could not see, so a row that passed for the wrong
	// reason would be caught here rather than read as green.
	if mayShareCountedBlockIn(ot.in, intArray) {
		t.Fatalf("int[] must share no counted block, or these rows pin the counted gate instead")
	}
	if !needsRelinquishWalkIn(ot.in, intArray) {
		t.Fatalf("int[] must need the relinquishing walk; the widening is what these rows check")
	}
	f := &Func{
		ID:   0,
		Name: "relinquish_array",
		Locals: []Local{
			{Type: intArray, Flags: LocalFlagOwnsHeap, Name: "xs"},
			{Type: ot.plain, Flags: LocalFlagCopy, Name: "plain"},
			{Type: intArray, Flags: LocalFlagOwnsHeap, Name: "ys"},
		},
		Blocks: []Block{{ID: 0, Term: Terminator{Kind: TermUnreachable}}},
	}
	return f, ot.in, intArray, ot.plain
}

func TestRelinquishedArrayOperandsAreSubjectToBothRules(t *testing.T) {
	move := func(ty types.TypeID, id LocalID) Operand {
		return Operand{Kind: OperandMove, Type: ty, Place: relinquishLocal(id)}
	}
	rows := []struct {
		name    string
		instrs  func(arrayTy types.TypeID, plain types.TypeID) []Instr
		wantAct string
		wantShp string
	}{
		{
			name: "an int array capture with no walk is refused by the act rule",
			instrs: func(arrayTy, _ types.TypeID) []Instr {
				return []Instr{relinquishCrossing(StructLitField{Name: "__cap0", Value: move(arrayTy, 0)})}
			},
			wantAct: "bb0 instr 0: state field __cap0 (L0, [int]) reaches the boundary without an un-share " +
				"in its block; it may share a counted block, or it carries a dynamic array the runtime must inspect",
		},
		{
			name: "the walk immediately before the sink is the act",
			instrs: func(arrayTy, _ types.TypeID) []Instr {
				return []Instr{unshareOf(0), relinquishCrossing(StructLitField{Name: "__cap0", Value: move(arrayTy, 0)})}
			},
		},
		{
			name: "a bare copy of an array at a far-select send is the aliasing bug by name",
			instrs: func(arrayTy, _ types.TypeID) []Instr {
				return []Instr{unshareOf(0), relinquishSelect(
					Operand{Kind: OperandCopy, Type: arrayTy, Place: relinquishLocal(0)})}
			},
			wantAct: "reaches the boundary as Copy L0",
			wantShp: "carries a dynamic array the runtime must inspect",
		},
		{
			name: "a blocking state field carrying an array is a sink too",
			instrs: func(arrayTy, _ types.TypeID) []Instr {
				return []Instr{{Kind: InstrBlocking, Blocking: BlockingInstr{State: StructLit{Fields: []StructLitField{
					{Name: "__cap0", Value: move(arrayTy, 2)},
				}}}}}
			},
			wantAct: "bb0 instr 0: blocking state field __cap0 (L2, [int]) reaches the boundary without an un-share",
		},
		{
			name: "an int field carries no array and is not asked",
			instrs: func(_, plain types.TypeID) []Instr {
				return []Instr{relinquishCrossing(StructLitField{Name: "__cap0",
					Value: Operand{Kind: OperandCopy, Type: plain, Place: relinquishLocal(1)}})}
			},
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			f, in, arrayTy, plainTy := arrayRelinquishFixture(t)
			f.Blocks[0].Instrs = row.instrs(arrayTy, plainTy)
			act := validateRelinquishedOperandsArePrivate(f, in)
			switch {
			case row.wantAct == "" && act != nil:
				t.Fatalf("unexpected refusal by the act rule: %v", act)
			case row.wantAct != "" && act == nil:
				t.Fatalf("the shape passed the act rule; want %q", row.wantAct)
			case row.wantAct != "" && !strings.Contains(act.Error(), row.wantAct):
				t.Fatalf("act refusal = %q, want it to contain %q", act, row.wantAct)
			}
			shape := validateRelinquishedOperandShapes(f, in)
			if row.wantShp == "" {
				if shape != nil {
					t.Fatalf("unexpected refusal by the shape rule: %v", shape)
				}
				return
			}
			if shape == nil || !strings.Contains(shape.Error(), row.wantShp) {
				t.Fatalf("shape refusal = %v, want it to contain %q", shape, row.wantShp)
			}
		})
	}
}

// The one sink the widened question is deliberately NOT asked of: the anchored
// body's send. Its prefix replays from the first instruction on every wake, so
// a walk emitted before the send would run again over storage the ring already
// owns; no instruction can satisfy the act rule at this sink, and a sink held
// to a rule nothing can meet fails the build with a validator's name instead of
// a diagnostic.
//
// What this row does NOT assert is that the payload was checked elsewhere. It
// was not. Sema holds an anchored send's payload to `own <captured binding>`
// only when the element may share a counted block, so an ARRAY payload is held
// to no shape: it may be built inside the block -- as it is here, an assignment
// rather than an unpack from `__state` -- or sliced out of a capture, and no
// walk on either thread sees it. That is the state this row pins, not a
// guarantee; closing it belongs to the sema gate the counted half uses.
func TestAnchoredSendOfAnArrayIsNotHeldToTheActRule(t *testing.T) {
	f, in, arrayTy, _ := arrayRelinquishFixture(t)
	f.Blocks[0].Instrs = []Instr{
		// The payload is built here, not unpacked from a state field.
		{Kind: InstrAssign, Assign: AssignInstr{
			Dst: relinquishLocal(0),
			Src: RValue{Kind: RValueUse, Use: Operand{Kind: OperandMove, Type: arrayTy, Place: relinquishLocal(2)}},
		}},
		{Kind: InstrCall, Call: CallInstr{
			Callee: Callee{Kind: CalleeValue, Name: "rt_anchored_channel_send"},
			Args:   []Operand{{Kind: OperandMove, Type: arrayTy, Place: relinquishLocal(0)}},
		}},
	}
	if err := validateRelinquishedOperandsArePrivate(f, in); err != nil {
		t.Fatalf("the anchored send of an array must not be held to the act rule: %v", err)
	}
	if err := validateRelinquishedOperandShapes(f, in); err != nil {
		t.Fatalf("the anchored send of an array must not be held to the shape rule: %v", err)
	}
}
