package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A variadic of references, `...args: &T`, receives the array the call packs,
// `args: (&T)[]`, and owns it like any variadic. The call used to borrow the
// packed array because the parameter's spelling (`&int`) starts with `&`, so
// the callee got `&(&int)[]`: the VM panicked VM1003 on `args.__len()`
// ("expected len-compatible, got ref") and the native program read the
// address as the array, printed garbage and freed a stack slot. Every row
// runs on both backends, natively under Valgrind, and prints the same.
var variadicReferencePackRows = []struct{ name, source, want string }{
	{"shared_refs", `fn count(...xs: &int) -> uint { return xs.__len(); }
@entrypoint
fn main() -> int {
    let a: int = 1;
    let b: int = 2;
    print(count(&a, &b) to string);
    print(count() to string);
    print((a + b) to string);
    return 0;
}
`, "2\n0\n3\n"},
	{"mut_refs_then_writes", `fn count_mut(...xs: &mut int) -> uint { return xs.__len(); }
@entrypoint
fn main() -> int {
    let mut x = 1;
    let mut y = 2;
    print(count_mut(&mut x, &mut y) to string);
    print(count_mut() to string);
    x = x + 10;
    y = y + 20;
    print((x + y) to string);
    return 0;
}
`, "2\n0\n33\n"},
	{"string_refs_after_a_leading_parameter", `fn count_str(prefix: string, ...xs: &string) -> uint { return prefix.__len() + xs.__len(); }
@entrypoint
fn main() -> int {
    let a = "ab";
    let b = "cde";
    print(count_str("p", &a, &b, &a) to string);
    print(count_str("pq") to string);
    print(a + b);
    return 0;
}
`, "4\n2\nabcde\n"},
	{"unused_pack", `fn seven(...xs: &int) -> int { return 7; }
@entrypoint
fn main() -> int {
    let x = 1;
    print(seven(&x, &x) to string);
    print(seven() to string);
    return 0;
}
`, "7\n7\n"},
}

func TestVariadicReferencePackRunsVM(t *testing.T) {
	requireVMBackend(t)
	for _, row := range variadicReferencePackRows {
		t.Run(row.name, func(t *testing.T) {
			res := runProgramFromSource(t, row.source, runOptions{captureStdout: true})
			if res.exitCode != 0 || res.stdout != row.want {
				t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", res.exitCode, res.stdout, row.want, res.stderr)
			}
		})
	}
}

func TestVariadicReferencePackLLVMValgrind(t *testing.T) {
	for _, row := range variadicReferencePackRows {
		t.Run(row.name, func(t *testing.T) {
			outputPath := buildLLVMProgramFromSource(t, row.source)
			stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, envWithStdlib(repoRoot(t)), 120*time.Second)
			if hasValgrindMemcheckError(stderr) || strings.Contains(stderr, "are definitely lost") {
				t.Fatalf("the packed array was not handed over as the value it is\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			if exitCode != 0 || stdout != row.want {
				t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", exitCode, stdout, row.want, stderr)
			}
		})
	}
}
