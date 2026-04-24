package vm_test

import (
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/vm"
)

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

func TestVMRefsStoreThroughSharedRefPanics(t *testing.T) {
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

	mirMod, files, typesInterner := compileToMIRFromSource(t, sourceCode)
	rt := vm.NewTestRuntime(nil, "")
	_, vmErr := runVM(mirMod, rt, files, typesInterner, nil)

	if vmErr == nil {
		t.Fatal("expected panic, got nil")
	}
	if vmErr.Code != vm.PanicStoreThroughNonMutRef {
		t.Fatalf("expected %v, got %v", vm.PanicStoreThroughNonMutRef, vmErr.Code)
	}

	out := vmErr.FormatWithFiles(files)
	if !strings.Contains(out, "panic VM2102") {
		t.Fatalf("expected panic code in output, got:\n%s", out)
	}
	// Проверяем, что путь к файлу присутствует (будет временный файл)
	if !strings.Contains(out, ".sg:") {
		t.Fatalf("expected span with file path in output, got:\n%s", out)
	}
	if !strings.Contains(out, "backtrace:") || !strings.Contains(out, "main") {
		t.Fatalf("expected backtrace with main frame, got:\n%s", out)
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
