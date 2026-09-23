package llvm

import (
	"regexp"
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

// A function with a result whose body ends in a statement that never
// completes -- here a compare every arm of which returns -- has an end
// nothing reaches. Sema types that last statement `nothing`, and
// the end of such a body must be emitted as `unreachable`: a `return` of
// `nothing` there is written `ret i8 0` in a function that returns something
// else, and llc refuses the whole module.

const tailCompareAwaitProgram = `async fn add(a: int, b: int) -> int {
    checkpoint().await();
    return a + b;
}

@entrypoint
fn main() -> int {
    compare add(2, 4).await() {
        Success(v) => return v;
        Cancelled() => return 9;
    };
}
`

const tailCompareOptionProgram = `fn pick(x: Option<int>) -> int {
    compare x {
        Some(v) => return v;
        nothing => return 0;
    };
}

@entrypoint
fn main() -> int {
    let six: Option<int> = Some(6);
    let none: Option<int> = nothing;
    return pick(six) + pick(none);
}
`

const tailCompareBoolProgram = `fn classify(flag: bool) -> int {
    compare flag {
        true => { return 6; }
        false => { return 0; }
    };
}

@entrypoint
fn main() -> int {
    return classify(true);
}
`

var llvmDefineRe = regexp.MustCompile(`^define (\S+) @`)
var llvmSwitchDefaultRe = regexp.MustCompile(`switch i32 \S+, label %(bb\d+) \[`)

func TestAFunctionEndingInAStatementThatNeverCompletesEndsInUnreachable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		program  string
		fn       string
		wantsTag bool // the compare is over a union, so its tests become a switch
	}{
		{name: "await_compare", program: tailCompareAwaitProgram, fn: "main", wantsTag: true},
		{name: "option_compare", program: tailCompareOptionProgram, fn: "pick", wantsTag: true},
		{name: "bool_compare", program: tailCompareBoolProgram, fn: "classify"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mirMod, result := lowerMIRFromSource(t, tc.program)
			fn := findFuncByName(t, mirMod, tc.fn)
			assertNoReturnOfNothing(t, fn, result.Sema.TypeInterner)

			ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
			if err != nil {
				t.Fatalf("emit LLVM IR: %v", err)
			}
			body := functionBody(t, ir, fn.ID)
			assertEveryRetHasTheResultType(t, tc.fn, body)
			if !hasIRLine(body, "unreachable") {
				t.Errorf("UNREACHABLE-MISSING: %s has no `unreachable` for the end of its body:\n%s", tc.fn, body)
			}
			if tc.wantsTag {
				assertSwitchDefaultIsUnreachable(t, tc.fn, body)
			}
		})
	}
}

// assertNoReturnOfNothing pins the layer: MIR must not return a `nothing`
// constant from a function whose result is not `nothing`.
func assertNoReturnOfNothing(t *testing.T, fn *mir.Func, typesIn *types.Interner) {
	t.Helper()
	if isNothingTypeID(typesIn, fn.Result) {
		t.Fatalf("%s returns nothing; the row needs a function with a result", fn.Name)
	}
	for i := range fn.Blocks {
		term := fn.Blocks[i].Term
		if term.Kind != mir.TermReturn || !term.Return.HasValue {
			continue
		}
		op := term.Return.Value
		if op.Kind == mir.OperandConst && op.Const.Kind == mir.ConstNothing && isNothingTypeID(typesIn, op.Type) {
			t.Errorf("RETURN-OF-NOTHING: %s block %d returns `nothing` from a function with a result", fn.Name, i)
		}
	}
}

func isNothingTypeID(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindNothing
}

// assertEveryRetHasTheResultType reads the function's own `define` line and
// requires every `ret` in its body to return that type.
func assertEveryRetHasTheResultType(t *testing.T, name, body string) {
	t.Helper()
	lines := strings.Split(body, "\n")
	m := llvmDefineRe.FindStringSubmatch(lines[0])
	if m == nil {
		t.Fatalf("no define line for %s:\n%s", name, lines[0])
	}
	want := "ret " + m[1]
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "ret ") {
			continue
		}
		if trimmed != want && !strings.HasPrefix(trimmed, want+" ") {
			t.Errorf("RET-TYPE: %s is defined to return %s but has %q", name, m[1], trimmed)
		}
	}
}

// assertSwitchDefaultIsUnreachable requires the default of the compare's tag
// switch -- the edge on which no arm matched -- to end in `unreachable`,
// following unconditional branches, and never in a `ret`.
func assertSwitchDefaultIsUnreachable(t *testing.T, name, body string) {
	t.Helper()
	m := llvmSwitchDefaultRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no tag switch in %s:\n%s", name, body)
	}
	label := m[1]
	for range 16 {
		term, ok := blockTerminator(body, label)
		if !ok {
			t.Fatalf("no terminated block %s in %s:\n%s", label, name, body)
		}
		switch {
		case term == "unreachable":
			return
		case strings.HasPrefix(term, "br label %"):
			label = strings.TrimPrefix(term, "br label %")
		default:
			t.Errorf("SWITCH-DEFAULT: the default %s of %s's tag switch reaches %q, want unreachable", m[1], name, term)
			return
		}
	}
	t.Errorf("SWITCH-DEFAULT: the default %s of %s's tag switch does not end within 16 blocks", m[1], name)
}

// blockTerminator returns the terminator line of the block with the label.
func blockTerminator(body, label string) (string, bool) {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != label+":" {
			continue
		}
		for _, next := range lines[i+1:] {
			trimmed := strings.TrimSpace(next)
			if strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, " ") {
				return "", false
			}
			for _, prefix := range []string{"unreachable", "ret ", "br ", "switch "} {
				if trimmed == prefix || strings.HasPrefix(trimmed, prefix) {
					return trimmed, true
				}
			}
		}
	}
	return "", false
}

func hasIRLine(body, want string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}
