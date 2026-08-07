package vm_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/mir"
)

// diagnoseSource runs the front end over one source and hands back its
// diagnostics, without insisting they be empty. Every other helper here
// compiles in order to RUN something and so fails the test on any error; a
// source that must be refused needs the bag itself.
func diagnoseSource(t *testing.T, sourceCode string) *diag.Bag {
	t.Helper()

	path := filepath.Join(t.TempDir(), "refused.sg")
	if err := os.WriteFile(path, []byte(sourceCode), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	// MaxDiagnostics sizes the bag, and a zero leaves it holding nothing at
	// all — a refusal test reading an unsized bag sees a clean compile and
	// passes whatever the compiler did.
	opts := driver.DiagnoseOptions{Stage: driver.DiagnoseStageSema, MaxDiagnostics: 100}
	result, err := driver.DiagnoseWithOptions(context.Background(), path, &opts)
	if err != nil {
		t.Fatalf("diagnose failed: %v", err)
	}
	return result.Bag
}

func TestVMRefsRead(t *testing.T) {
	sourceCode := `@entrypoint
fn main() -> int {
    let x: int = 7;
    let r: &int = &x;
    return *r;
}
`
	result := runProgramFromSource(t, sourceCode, runOptions{})
	if result.exitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", result.exitCode)
	}
}

func TestVMRefsRefMutWrite(t *testing.T) {
	sourceCode := `fn set(x: &mut int) -> nothing {
    *x = 9;
    return;
}

@entrypoint
fn main() -> int {
    let mut v: int = 1;
    set(&mut v);
    return v;
}
`
	result := runProgramFromSource(t, sourceCode, runOptions{})
	if result.exitCode != 9 {
		t.Fatalf("expected exit code 9, got %d", result.exitCode)
	}
}

func TestVMRefsStructFieldWrite(t *testing.T) {
	sourceCode := `type S = { a: int, b: int }

fn set(x: &mut int) -> nothing {
    *x = 10;
    return;
}

@entrypoint
fn main() -> int {
    let mut s: S = S { a = 1, b = 2 };
    set(&mut s.a);
    return s.a;
}
`
	result := runProgramFromSource(t, sourceCode, runOptions{})
	if result.exitCode != 10 {
		t.Fatalf("expected exit code 10, got %d", result.exitCode)
	}
}

func TestVMRefsStructFieldReadThroughRef(t *testing.T) {
	sourceCode := `type S = { a: int }

fn get(s: &S) -> int {
    return s.a;
}

@entrypoint
fn main() -> int {
    let s: S = S { a = 7 };
    return get(&s);
}
`
	result := runProgramFromSource(t, sourceCode, runOptions{})
	if result.exitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", result.exitCode)
	}
}

func TestVMRefsArrayElemWrite(t *testing.T) {
	sourceCode := `fn set(x: &mut int) -> nothing {
    *x = 9;
    return;
}

@entrypoint
fn main() -> int {
    let mut a: int[] = [1, 2, 3];
    set(&mut a[1]);
    return a[1];
}
`
	result := runProgramFromSource(t, sourceCode, runOptions{})
	if result.exitCode != 9 {
		t.Fatalf("expected exit code 9, got %d", result.exitCode)
	}
}

func TestVMRefsArrayFieldMutRefSharedReborrow(t *testing.T) {
	sourceCode := `type Entry = {
    borrowers: string[],
};

fn add_borrower(entry: &mut Entry, client_id: &string) -> nothing {
    if !entry.borrowers.contains(client_id) {
        entry.borrowers.push(client_id.__clone());
    }
    return nothing;
}

@entrypoint
fn main() -> int {
    let mut entry: Entry = { borrowers = [] };
    add_borrower(&mut entry, &"client-a");
    add_borrower(&mut entry, &"client-a");
    return entry.borrowers.__len() to int;
}
`
	result := runProgramFromSource(t, sourceCode, runOptions{})
	if result.exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d\nstderr:\n%s", result.exitCode, result.stderr)
	}
}

// This program used to reach the VM and trap. It no longer compiles, and that
// is the point: the trap was only ever half a rule.
//
// The native backend never had the other half. A reference there is a bare
// pointer with nowhere to keep a mutability bit, so this same store landed in
// the caller's `x` and the program carried on — two backends, two meanings, one
// source. Refusing it in sema is what makes them agree, and it is the only
// place that still knows `&int` from `&mut int`.
//
// VM2102 is now unreachable from well-typed source. It is deliberately not
// deleted: TestStoreThroughANonMutableLocationStillTraps exercises the guard
// directly, so a MIR that reaches the VM some other way still stops.
func TestVMRefsStoreThroughSharedRefIsRefusedBeforeItRuns(t *testing.T) {
	requireVMBackend(t)
	sourceCode := `fn set(x: &int) -> nothing {
    *x = 2;
    return;
}

@entrypoint
fn main() -> int {
    let x: int = 1;
    set(&x);
    return 0;
}
`

	bag := diagnoseSource(t, sourceCode)
	if !bag.HasErrors() {
		t.Fatal("expected the store through a shared reference to be refused, got a clean compile")
	}
	found := false
	for _, item := range bag.Items() {
		if item.Code == diag.SemaStoreThroughSharedRef {
			found = true
			break
		}
	}
	if !found {
		var sb strings.Builder
		for _, item := range bag.Items() {
			sb.WriteString(item.Code.ID())
			sb.WriteString(": ")
			sb.WriteString(item.Message)
			sb.WriteString("\n")
		}
		t.Fatalf("expected %v, got:\n%s", diag.SemaStoreThroughSharedRef, sb.String())
	}
}

func TestVMRefsMapGetMutReadonlyHelperKeepsMutRefLive(t *testing.T) {
	sourceCode := `type Entry = { value: string, owner: string? };

fn inspect(entry: &Entry) -> nothing {
    let _ = entry;
    return nothing;
}

@entrypoint
fn main() -> int {
    let mut entries = { "k" => Entry { value = "seed", owner = nothing } };
    compare entries.get_mut(&"k") {
        Some(entry) => {
            inspect(entry);
            entry.owner = Some("client-a");
            return 7;
        }
        nothing => return 1;
    }
    return 2;
}
`
	result := runProgramFromSource(t, sourceCode, runOptions{})
	if result.exitCode != 7 {
		t.Fatalf("expected exit code 7, got %d\nstderr:\n%s", result.exitCode, result.stderr)
	}
	if result.stderr != "" {
		t.Fatalf("expected empty stderr, got:\n%s", result.stderr)
	}
}

func TestVMRefsMapGetMutReadonlyHelperUsesSharedReborrowInMIR(t *testing.T) {
	sourceCode := `type Entry = { value: string, owner: string? };

fn inspect(entry: &Entry) -> nothing {
    let _ = entry;
    return nothing;
}

@entrypoint
fn main() -> nothing {
    let mut entries = { "k" => Entry { value = "seed", owner = nothing } };
    compare entries.get_mut(&"k") {
        Some(entry) => {
            inspect(entry);
            entry.owner = Some("client-a");
        }
        nothing => {}
    }
    return nothing;
}
`

	mirMod, _, _ := compileToMIRFromSource(t, sourceCode)

	var inspectCall *mir.CallInstr
	for _, fn := range mirMod.Funcs {
		if fn == nil || fn.Name != "main" {
			continue
		}
		for _, bb := range fn.Blocks {
			for _, instr := range bb.Instrs {
				if instr.Kind != mir.InstrCall {
					continue
				}
				if instr.Call.Callee.Name != "inspect" {
					continue
				}
				call := instr.Call
				inspectCall = &call
				break
			}
			if inspectCall != nil {
				break
			}
		}
	}

	if inspectCall == nil {
		t.Fatal("expected inspect call in MIR")
	}
	if len(inspectCall.Args) != 1 {
		t.Fatalf("expected 1 inspect arg, got %d", len(inspectCall.Args))
	}

	arg := inspectCall.Args[0]
	if arg.Kind != mir.OperandAddrOf {
		t.Fatalf("expected inspect arg to be addr_of reborrow, got %s", arg.Kind.String())
	}
	if len(arg.Place.Proj) == 0 {
		t.Fatalf("expected inspect arg place to deref the mutable ref local")
	}
	lastProj := arg.Place.Proj[len(arg.Place.Proj)-1]
	if lastProj.Kind != mir.PlaceProjDeref {
		t.Fatalf("expected final projection to be deref, got %v", lastProj.Kind)
	}
}

func TestVMRefsArrayIndexSharedReceiverUsesSharedReborrowInMIR(t *testing.T) {
	sourceCode := `fn read_twice(xs: &mut string[], i: int) -> nothing {
    let first = xs[i];
    let second = xs[i];
    return nothing;
}

@entrypoint
fn main() -> int {
    let mut xs: string[] = ["a"];
    read_twice(&mut xs, 0);
    return 0;
}
`

	mirMod, _, _ := compileToMIRFromSource(t, sourceCode)

	found := 0
	for _, fn := range mirMod.Funcs {
		if fn == nil || fn.Name != "read_twice" {
			continue
		}
		for _, bb := range fn.Blocks {
			for _, instr := range bb.Instrs {
				if instr.Kind != mir.InstrCall || !strings.HasPrefix(instr.Call.Callee.Name, "__index") {
					continue
				}
				found++
				if len(instr.Call.Args) == 0 {
					t.Fatal("expected __index receiver argument")
				}
				arg := instr.Call.Args[0]
				if arg.Kind != mir.OperandAddrOf {
					t.Fatalf("expected __index receiver to be addr_of reborrow, got %s", arg.Kind.String())
				}
				if len(arg.Place.Proj) == 0 || arg.Place.Proj[len(arg.Place.Proj)-1].Kind != mir.PlaceProjDeref {
					t.Fatalf("expected __index receiver place to dereference mutable reference, got %+v", arg.Place)
				}
			}
		}
	}
	if found != 2 {
		t.Fatalf("found %d __index calls, want 2", found)
	}
}

func TestVMRefsSharedReborrowOfProjectedFieldDoesNotDoubleDeref(t *testing.T) {
	sourceCode := `type Holder = { data: string[] };

fn inspect(data: &string[]) -> nothing { return nothing; }

fn forward(holder: &mut Holder) -> nothing {
    inspect(holder.data);
    return nothing;
}

@entrypoint
fn main() -> int {
    let mut holder = Holder { data = ["a"] };
    forward(&mut holder);
    return 0;
}
`

	mirMod, _, _ := compileToMIRFromSource(t, sourceCode)

	for _, fn := range mirMod.Funcs {
		if fn == nil || fn.Name != "forward" {
			continue
		}
		for _, bb := range fn.Blocks {
			for _, instr := range bb.Instrs {
				if instr.Kind != mir.InstrCall || instr.Call.Callee.Name != "inspect" {
					continue
				}
				if len(instr.Call.Args) != 1 {
					t.Fatalf("inspect args = %d, want 1", len(instr.Call.Args))
				}
				arg := instr.Call.Args[0]
				if arg.Kind != mir.OperandAddrOf {
					t.Fatalf("inspect arg = %s, want shared AddrOf", arg.Kind.String())
				}
				if len(arg.Place.Proj) == 0 || arg.Place.Proj[len(arg.Place.Proj)-1].Kind != mir.PlaceProjField {
					t.Fatalf("projected-field reborrow ends in %+v, want field projection without trailing deref", arg.Place.Proj)
				}
				return
			}
		}
	}
	t.Fatal("expected inspect call in forward")
}
