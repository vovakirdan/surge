package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A choice of a plain value and `nothing` under a target union that has both
// cases converts each branch into the union directly, and under a narrower
// Option target its literal takes the payload type. The base miscompiled the
// union rows (the `nothing` branch read as the int) and refused the `int8?`
// ones; each `nothing` branch is taken on both backends.
const choiceTargetUnionSource = `type MaybeInt = int | nothing;
fn f(c: bool) -> MaybeInt { return c ? 5 : nothing; }
fn show(u: MaybeInt) -> nothing { compare u { nothing => { print("N"); } finally => { print("V"); } }; return nothing; }
fn narrow(c: bool, d: bool) -> nothing {
    let y: int8? = c ? 6 : nothing;
    compare y { Some(v) => { print("B" + (v to string)); } nothing => { print("N"); } };
    let z: int8? = c ? 7 : d ? 8 : nothing;
    compare z { Some(v) => { print("B" + (v to string)); } nothing => { print("N"); } };
    return nothing;
}
@entrypoint
fn main() -> int {
    let c = false;
    let x: MaybeInt = c ? 5 : nothing;
    show(x);
    show(f(true));
    show(f(false));
    narrow(true, true);
    narrow(false, true);
    narrow(false, false);
    return 0;
}
`

const choiceTargetUnionWant = "N\nV\nN\nB6\nB7\nN\nB8\nN\nN\n"

func TestChoiceIntoATargetUnionRunsVM(t *testing.T) {
	requireVMBackend(t)
	res := runProgramFromSource(t, choiceTargetUnionSource, runOptions{captureStdout: true})
	if res.exitCode != 0 || res.stdout != choiceTargetUnionWant {
		t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", res.exitCode, res.stdout, choiceTargetUnionWant, res.stderr)
	}
}

func TestChoiceIntoATargetUnionLLVMValgrind(t *testing.T) {
	outputPath := buildLLVMProgramFromSource(t, choiceTargetUnionSource)
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, envWithStdlib(repoRoot(t)), 120*time.Second)
	if hasValgrindMemcheckError(stderr) || strings.Contains(stderr, "are definitely lost") {
		t.Fatalf("memcheck\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 || stdout != choiceTargetUnionWant {
		t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", exitCode, stdout, choiceTargetUnionWant, stderr)
	}
}
