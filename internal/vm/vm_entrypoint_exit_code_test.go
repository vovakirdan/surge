package vm_test

import (
	"strings"
	"testing"
	"time"

	"surge/internal/mir"
)

// A Copy result still owns its heap-backed numeric fields after the exit-code
// method borrows it. Both execution lanes must return the right exit code,
// and native process exit must leave none of those fields allocated.
func TestEntrypointExitCodeCopyResultRunsLLVM(t *testing.T) {
	const src = `@copy type Status = { payload: uint };
extern<Status> {
    pub fn __exit_code(self: &Status) -> int { return 0; }
}
@entrypoint
fn main() -> Status { return Status { payload = 18446744073709551616:uint }; }
`
	res := runProgramFromSource(t, src, runOptions{})
	if res.exitCode != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", res.exitCode, res.stderr)
	}
	binary := buildLLVMProgramFromSource(t, src)
	stdout, stderr, code := runBinaryUnderValgrind(t, binary, envWithStdlib(repoRoot(t)), 120*time.Second)
	if code != 0 || stdout != "" || hasValgrindMemcheckError(stderr) {
		t.Fatalf("under valgrind: exit=%d stdout=%q\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "All heap blocks were freed") {
		t.Fatalf("main's Copy result outlived the program:\n%s", stderr)
	}
}

// An @entrypoint's result becomes the process exit code: `nothing` exits 0,
// `int` exits with itself, and every other result goes through the ExitCode
// contract's `__exit_code(self: &T) -> int` (core/entrypoint.sg). The same rows
// run on the LLVM backend with SURGE_BACKEND=llvm.
func TestEntrypointExitCodeContract(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"int", "@entrypoint\nfn main() -> int { return 9; }\n", 9},
		{"nothing", "@entrypoint\nfn main() { }\n", 0},
		{"option_some", "@entrypoint\nfn main() -> int? { return Some(5); }\n", 0},
		{"option_nothing", "@entrypoint\nfn main() -> int? { return nothing; }\n", 1},
		{"option_heap_payload", "@entrypoint\nfn main() -> Option<string> { return Some(\"payload\"); }\n", 0},
		{"erring_success", "@entrypoint\nfn main() -> int! { return Success(3); }\n", 0},
		{"erring_error_code", "@entrypoint\nfn main() -> int! {\n    let e: Error = { message = \"bad\", code = 7:uint };\n    return e;\n}\n", 7},
		{"erring_heap_payload_error", "@entrypoint\nfn main() -> string! {\n    let e: Error = { message = \"boom\", code = 3:uint };\n    return e;\n}\n", 3},
		{"uint", "@entrypoint\nfn main() -> uint { return 4:uint; }\n", 4},
		{"int8", "@entrypoint\nfn main() -> int8 { return 6:int8; }\n", 6},
		{"user_contract", `type Status = { label: string, code: int }
extern<Status> {
    pub fn __exit_code(self: &Status) -> int { return self.code; }
}
@entrypoint
fn main() -> Status { return Status { label = "done", code = 42 }; }
`, 42},
		{"direct_call", `@entrypoint
fn main() -> int {
    let o: Option<int> = nothing;
    let e: int! = Success(1);
    return o.__exit_code() * 10 + e.__exit_code();
}
`, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := runProgramFromSource(t, tc.src, runOptions{})
			if result.exitCode != tc.want {
				t.Fatalf("exit code = %d, want %d (stderr %q)", result.exitCode, tc.want, result.stderr)
			}
		})
	}
}

// `__exit_code` borrows main's result, so the generated entry still owns it and
// releases it after the call and before it exits.
func TestEntrypointExitCodeReleasesBorrowedResult(t *testing.T) {
	mirMod, _, _ := compileToMIRFromSource(t, `type Status = { label: string, code: int }
extern<Status> {
    pub fn __exit_code(self: &Status) -> int { return self.code; }
}
@entrypoint
fn main() -> Status { return Status { label = "done", code = 42 }; }
`)
	var start *mir.Func
	for _, fn := range mirMod.Funcs {
		if fn.Name == "__surge_start" {
			start = fn
		}
	}
	if start == nil {
		t.Fatal("no __surge_start")
	}
	receiver, called, dropped := mir.NoLocalID, false, false
	for _, block := range start.Blocks {
		for i := range block.Instrs {
			instr := &block.Instrs[i]
			switch {
			case instr.Kind == mir.InstrCall && instr.Call.Callee.Name == "__exit_code":
				if len(instr.Call.Args) != 1 || instr.Call.Args[0].Kind != mir.OperandAddrOf {
					t.Fatalf("__exit_code must borrow main's result: %+v", instr.Call.Args)
				}
				receiver, called = instr.Call.Args[0].Place.Local, true
			case instr.Kind == mir.InstrDrop && called && instr.Drop.Place.Local == receiver:
				dropped = true
			case instr.Kind == mir.InstrCall && instr.Call.Callee.Name == "rt_exit" && !dropped:
				t.Fatalf("rt_exit before main's borrowed result is released (called=%v)", called)
			}
		}
	}
	if !called || !dropped {
		t.Fatalf("called=%v dropped=%v", called, dropped)
	}
}
