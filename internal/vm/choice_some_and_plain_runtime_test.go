package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A plain value, a `Some<T>` and `nothing` join into one `Option<T>` in any
// order and nesting: the plain value is wrapped in `Some` and the `Some<T>`
// upcast, so the plain join and the union join compose into one rule. The
// compare forms were refused ("compare arm type mismatch") before; each row
// runs with every branch taken on both backends.
const choiceSomeAndPlainSource = `fn show(o: int?) -> nothing { compare o { Some(v) => { print("S" + (v to string)); } nothing => { print("N"); } }; return nothing; }
fn run(c: bool, d: bool, k: int) -> nothing {
    let a = c ? Some(1) : d ? 2 : nothing;
    show(a);
    let b = c ? 3 : d ? Some(4) : nothing;
    show(b);
    let e = compare k { 1 => Some(5); 2 => 6; _ => nothing; };
    show(e);
    let f = compare k { 1 => 7; 2 => Some(8); _ => nothing; };
    show(f);
    let g = c ? Some(9) : 10;
    show(g);
    return nothing;
}
@entrypoint
fn main() -> int { run(true, true, 1); print("--"); run(false, true, 2); print("--"); run(false, false, 3); return 0; }
`

const choiceSomeAndPlainWant = "S1\nS3\nS5\nS7\nS9\n--\nS2\nS4\nS6\nS8\nS10\n--\nN\nN\nN\nN\nS10\n"

func TestChoiceOfSomeAPlainValueAndNothingRunsVM(t *testing.T) {
	requireVMBackend(t)
	res := runProgramFromSource(t, choiceSomeAndPlainSource, runOptions{captureStdout: true})
	if res.exitCode != 0 || res.stdout != choiceSomeAndPlainWant {
		t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", res.exitCode, res.stdout, choiceSomeAndPlainWant, res.stderr)
	}
}

func TestChoiceOfSomeAPlainValueAndNothingLLVMValgrind(t *testing.T) {
	outputPath := buildLLVMProgramFromSource(t, choiceSomeAndPlainSource)
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, envWithStdlib(repoRoot(t)), 120*time.Second)
	if hasValgrindMemcheckError(stderr) || strings.Contains(stderr, "are definitely lost") {
		t.Fatalf("memcheck\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 || stdout != choiceSomeAndPlainWant {
		t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", exitCode, stdout, choiceSomeAndPlainWant, stderr)
	}
}
