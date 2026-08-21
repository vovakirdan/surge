package mir_test

import (
	"fmt"
	"strings"
	"testing"

	"surge/internal/mir"
)

// findCall returns the first call to a callee whose base name matches.
func findCall(t *testing.T, mod *mir.Module, name string) *mir.CallInstr {
	t.Helper()
	for _, id := range mod.SortedFuncIDs() {
		f := mod.Funcs[id]
		if f == nil {
			continue
		}
		for bi := range f.Blocks {
			for ii := range f.Blocks[bi].Instrs {
				ins := &f.Blocks[bi].Instrs[ii]
				if ins.Kind != mir.InstrCall {
					continue
				}
				callee := ins.Call.Callee.Name
				if idx := strings.Index(callee, "::<"); idx >= 0 {
					callee = callee[:idx]
				}
				if callee == name {
					return &ins.Call
				}
			}
		}
	}
	t.Fatalf("no call to %q found in module", name)
	return nil
}

func contractNames(cs []mir.ArgContract) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, c.String())
	}
	return fmt.Sprintf("[%s]", strings.Join(parts, " "))
}

// TestCallArgContractsCoverEveryPosition is the coverage half of the
// per-argument ownership contract: whatever callee shape a program reaches —
// direct function, generic instance, tag constructor, intrinsic, method,
// default argument, indirect function value — the position count and the
// contract count must agree, or the contract is not usable as a sink table.
func TestCallArgContractsCoverEveryPosition(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "direct_and_generic_and_default",
			src: `
fn takes(s: string) -> nothing { }
fn ident<T>(v: T) -> T { return v; }
fn defaulted(a: int, b: int = 7) -> int { return a + b; }
fn main() -> int {
    takes("hi");
    let n = ident::<int>(1);
    return defaulted(n);
}`,
		},
		{
			name: "tag_constructor_and_method",
			src: `
tag Some<T>(T);
tag None();
type Option<T> = Some(T) | None;
type Box = { v: int };
extern<Box> {
    fn get(self: &Box) -> int { return self.v; }
}
fn main() -> int {
    let o = Some::<string>("held");
    let b = Box { v: 3 };
    return b.get();
}`,
		},
		{
			name: "indirect_function_value",
			src: `
fn twice(v: int) -> int { return v * 2; }
fn main() -> int {
    let f = twice;
    return f(21);
}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileCrossingMIR(t, tc.src, nil)
			for _, id := range compiled.mod.SortedFuncIDs() {
				f := compiled.mod.Funcs[id]
				if f == nil {
					continue
				}
				for bi := range f.Blocks {
					for ii := range f.Blocks[bi].Instrs {
						ins := &f.Blocks[bi].Instrs[ii]
						if ins.Kind != mir.InstrCall {
							continue
						}
						if len(ins.Call.ArgContracts) != len(ins.Call.Args) {
							t.Errorf("%s bb%d instr %d: call %q has %d args but %d contracts",
								f.Name, bi, ii, ins.Call.Callee.Name,
								len(ins.Call.Args), len(ins.Call.ArgContracts))
						}
					}
				}
			}
			if err := mir.ValidateStructure(compiled.mod, compiled.types); err != nil {
				t.Fatalf("validation failed: %v", err)
			}
		})
	}
}

// TestCallArgContractsAreMixedPerPosition is the granularity half: a container
// intrinsic borrows its receiver and STORES the value in the same call, so a
// per-CALL answer could never be right. This is the rt_array_push shape the
// verifier's sink rule is built around.
func TestCallArgContractsAreMixedPerPosition(t *testing.T) {
	compiled := compileCrossingMIR(t, `
@intrinsic fn rt_array_push<T>(a: &mut Array<T>, value: T) -> nothing;
fn main() -> int {
    let mut names: string[] = [];
    rt_array_push(&mut names, "held");
    return 0;
}`, nil)

	call := findCall(t, compiled.mod, "rt_array_push")
	want := []mir.ArgContract{mir.ArgContractBorrow, mir.ArgContractStore}
	if len(call.ArgContracts) != len(want) {
		t.Fatalf("rt_array_push contracts = %s, want %s", contractNames(call.ArgContracts), contractNames(want))
	}
	for i := range want {
		if call.ArgContracts[i] != want[i] {
			t.Fatalf("rt_array_push contracts = %s, want %s", contractNames(call.ArgContracts), contractNames(want))
		}
	}
}

// TestCallArgContractsSkipUserFunctionsSharingAnIntrinsicName pins the guard on
// the audited name table: `__index_set` is a magic method any type may
// implement, and a hand-written one is owed whatever its own parameters say. A
// name-only upgrade would report it as a consuming sink it is not.
func TestCallArgContractsSkipUserFunctionsSharingAnIntrinsicName(t *testing.T) {
	compiled := compileCrossingMIR(t, `
type Counter = { seen: int };
extern<Counter> {
    pub fn __index_set(self: &mut Counter, index: int, value: &string) -> nothing {
        self.seen = index;
        return nothing;
    }
}
fn main() -> int {
    let mut c = Counter { seen: 0 };
    let held = "borrowed";
    c.__index_set(1, &held);
    return 0;
}`, nil)

	call := findCall(t, compiled.mod, "__index_set")
	for i, got := range call.ArgContracts {
		if got == mir.ArgContractStore {
			t.Errorf("user-defined __index_set position %d = store, want no upgrade (all: %s)",
				i, contractNames(call.ArgContracts))
		}
	}
}

// TestCallArgContractsSplitBorrowFromOwned pins the ordinary-parameter fork the
// contract records: a reference position lends, an owned by-value position
// hands over, and a tag constructor's payload is kept past the call.
func TestCallArgContractsSplitBorrowFromOwned(t *testing.T) {
	compiled := compileCrossingMIR(t, `
tag Some<T>(T);
tag None();
type Option<T> = Some(T) | None;
fn borrows(s: &string) -> nothing { }
fn owns(s: string) -> nothing { }
fn main() -> int {
    let held = "value";
    borrows(&held);
    owns("moved");
    let o = Some::<string>("stored");
    return 0;
}`, nil)

	cases := []struct {
		callee string
		want   mir.ArgContract
	}{
		{"borrows", mir.ArgContractBorrow},
		{"owns", mir.ArgContractTransferOwned},
		{"Some", mir.ArgContractStore},
	}
	for _, tc := range cases {
		call := findCall(t, compiled.mod, tc.callee)
		if len(call.ArgContracts) != 1 {
			t.Fatalf("%s: contracts = %s, want one entry", tc.callee, contractNames(call.ArgContracts))
		}
		if call.ArgContracts[0] != tc.want {
			t.Errorf("%s: contract = %s, want %s", tc.callee, call.ArgContracts[0], tc.want)
		}
	}
}

// TestAStoredRefCountedScalarIsRetainedNotCopied pins the connection between a
// position's CONTRACT and its OPERAND, which existed only in prose.
//
// A reference-counted scalar is Copy at the surface, so a call reads it with
// the ordinary rule, and that rule produces a bare Copy at a call site because
// most callees borrow: a `float` handed to an arithmetic entry point must NOT
// be retained. But a STORE position is a sink -- the container outlives the
// call and releases the value later -- so the caller's own binding and the
// container would release one block twice.
//
// Measured on Channel<float> before the fix, both halves of the same defect:
// with the sender's binding still live the program printed the right answer and
// then SEGFAULTED on the second release; with the sender a temporary, the
// channel kept a freed block and the program printed a WRONG ANSWER (`small`
// for 1.5 > 1.0) and exited 0.
func TestAStoredRefCountedScalarIsRetainedNotCopied(t *testing.T) {
	compiled := compileCrossingMIR(t, crossingMIRPrelude+`
extern<Channel<T>> {
    @intrinsic fn send(self: &Channel<T>, value: T) -> nothing;
}

fn main(ch: &Channel<float>) -> int {
    let kept = 1.5;
    ch.send(kept);
    return 0;
}`, nil)

	call := findCall(t, compiled.mod, "send")
	if len(call.Args) != 2 || len(call.ArgContracts) != 2 {
		t.Fatalf("send has %d args / %d contracts, want 2 / 2", len(call.Args), len(call.ArgContracts))
	}
	if call.ArgContracts[1] != mir.ArgContractStore {
		t.Fatalf("send contracts = %s, want the value position to be a store", contractNames(call.ArgContracts))
	}
	if call.Args[1].Kind != mir.OperandRetain {
		t.Fatalf(
			"the stored float is passed as %v, so nothing bumps its count: the channel and the "+
				"caller's binding would each release the same block",
			call.Args[1].Kind,
		)
	}
}

// TestABorrowedRefCountedScalarIsNotRetained is the other half, and it is what
// keeps the fix from being "retain everything". An ordinary function's by-value
// parameter does not outlive the call, so bumping the count there would leak on
// every arithmetic call in the language.
func TestABorrowedRefCountedScalarIsNotRetained(t *testing.T) {
	compiled := compileCrossingMIR(t, crossingMIRPrelude+`
fn takes(v: float) -> nothing { return nothing; }

fn main() -> int {
    let kept = 1.5;
    takes(kept);
    return 0;
}`, nil)

	call := findCall(t, compiled.mod, "takes")
	if len(call.Args) != 1 {
		t.Fatalf("takes has %d args, want 1", len(call.Args))
	}
	if call.Args[0].Kind == mir.OperandRetain {
		t.Fatal("a borrowed float was retained; the count would never come back down")
	}
}
