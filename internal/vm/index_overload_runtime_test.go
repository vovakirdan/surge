package vm_test

import (
	"os/exec"
	"testing"
)

// An index overload with the exact index type wins over one the index only
// widens into. With both `__index(self: &S, i: int)` and
// `@overload __index(self: &S, i: int64)`, `s[5:int64]` used to select the
// `int` body whichever came first, which then read the int64 as a dynamic
// integer. Both declaration orders must run the int64 body.
var indexOverloadRuntimeRows = []struct{ name, source, want string }{
	{"int_declared_first", `type S = { n: int };
extern<S> {
    fn __index(self: &S, i: int) -> int { return i + 1000; }
    @overload fn __index(self: &S, i: int64) -> int { return (i to int) + 2000; }
}
@entrypoint
fn main() -> int {
    let s: S = { n: 1 };
    let b: int = s[5:int64];
    let big: int64 = 4294967300:int64;
    let c: int = s[big];
    print(b to string);
    print(c to string);
    return 0;
}
`, "2005\n4294969300\n"},
	{"int64_declared_first", `type S = { n: int };
extern<S> {
    fn __index(self: &S, i: int64) -> int { return (i to int) + 2000; }
    @overload fn __index(self: &S, i: int) -> int { return i + 1000; }
}
@entrypoint
fn main() -> int {
    let s: S = { n: 1 };
    let b: int = s[5:int64];
    let big: int64 = 4294967300:int64;
    let c: int = s[big];
    print(b to string);
    print(c to string);
    return 0;
}
`, "2005\n4294969300\n"},
}

func TestIndexOverloadRunsTheExactIndexTypeVM(t *testing.T) {
	requireVMBackend(t)
	for _, row := range indexOverloadRuntimeRows {
		t.Run(row.name, func(t *testing.T) {
			res := runProgramFromSource(t, row.source, runOptions{captureStdout: true})
			if res.exitCode != 0 || res.stdout != row.want {
				t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", res.exitCode, res.stdout, row.want, res.stderr)
			}
		})
	}
}

func TestIndexOverloadRunsTheExactIndexTypeLLVM(t *testing.T) {
	root := repoRoot(t)
	for _, row := range indexOverloadRuntimeRows {
		t.Run(row.name, func(t *testing.T) {
			outputPath := buildLLVMProgramFromSource(t, row.source)
			cmd := exec.Command(outputPath)
			cmd.Dir = root
			stdout, stderr, exitCode := runCommand(t, cmd, "")
			if exitCode != 0 || stdout != row.want {
				t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", exitCode, stdout, row.want, stderr)
			}
		})
	}
}
