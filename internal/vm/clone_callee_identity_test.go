package vm_test

import (
	"os/exec"
	"testing"
)

// A call spelled `clone` is the builtin clone only when its callee is the core
// declaration. When the name resolves to anything else -- a local function
// value, a parameter, a binding in an inner block, a user overload -- the call
// runs that callee. Treating the spelling as the builtin copied the argument
// instead: every witness below printed the argument (1 or 5), never 42.
var cloneCalleeIdentityRows = []struct{ name, source, want string }{
	{"local_fn_value", `fn fortytwo(r: &int) -> int { return 42; }
fn f(a: &int) -> int {
    let clone: fn(&int) -> int = fortytwo;
    return clone(a);
}
@entrypoint
fn main() -> int { let x: int = 1; print(f(&x)); return 0; }
`, "42\n"},
	{"param_named_clone", `fn fortytwo(r: &int) -> int { return 42; }
fn f(clone: fn(&int) -> int, a: &int) -> int {
    return clone(a);
}
@entrypoint
fn main() -> int { let x: int = 1; print(f(fortytwo, &x)); return 0; }
`, "42\n"},
	{"block_binding", `fn fortytwo(r: &int) -> int { return 42; }
fn f(a: &int) -> int {
    {
        let clone: fn(&int) -> int = fortytwo;
        return clone(a);
    }
}
@entrypoint
fn main() -> int { let x: int = 1; print(f(&x)); return 0; }
`, "42\n"},
	{"user_overload_copy_type", `@overload
fn clone(x: &int64) -> int64 {
    return 42;
}
fn copy_number(n: &int64) -> int64 {
    return clone(n);
}
@entrypoint
fn main() -> int {
    let a: int64 = 5;
    print(copy_number(&a) to string);
    return 0;
}
`, "42\n"},
	// The overload set mixes a user clone with the core one: the user
	// declaration answers its own type, and the core clone still answers every
	// other type (the control half of this row).
	{"user_overload_beside_core", `type Tok = { v: int };
@overload
fn clone(x: &Tok) -> Tok {
    return { v: 42 };
}
@entrypoint
fn main() -> int {
    let t: Tok = { v: 5 };
    let s: string = "hi";
    let t2 = clone(&t);
    let s2 = clone(&s);
    print(t2.v to string);
    print(s2);
    return 0;
}
`, "42\nhi\n"},
	// Control: with nothing else named clone, the core clone is the callee.
	{"core_clone_control", `@entrypoint
fn main() -> int {
    let s: string = "str";
    let s2 = clone(&s);
    print(s2);
    return 0;
}
`, "str\n"},
}

func TestCloneCallRunsTheResolvedCalleeVM(t *testing.T) {
	requireVMBackend(t)
	for _, row := range cloneCalleeIdentityRows {
		t.Run(row.name, func(t *testing.T) {
			res := runProgramFromSource(t, row.source, runOptions{captureStdout: true})
			if res.exitCode != 0 || res.stdout != row.want {
				t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", res.exitCode, res.stdout, row.want, res.stderr)
			}
		})
	}
}

func TestCloneCallRunsTheResolvedCalleeLLVM(t *testing.T) {
	root := repoRoot(t)
	for _, row := range cloneCalleeIdentityRows {
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
