package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A known call parameter supplies the target union. Both paths must preserve
// its discriminant, and an owned string wrapped in Some must be released.
const choiceCallTargetSource = `type MaybeInt = int | nothing;
fn show_union(x: MaybeInt) -> nothing {
    compare x { nothing => { print("N"); } finally => { print("V"); } };
    return nothing;
}
fn show_option(x: int?) -> nothing {
    compare x { Some(v) => { print("S" + (v to string)); } nothing => { print("N"); } };
    return nothing;
}
fn show_string(x: string?) -> nothing {
    compare x { Some(v) => { print("T" + v); } nothing => { print("N"); } };
    return nothing;
}
fn exercise(c: bool) -> nothing {
    show_union(((compare c { true => 17; false => nothing; })));
    show_union(c ? 19 : nothing);
    show_union(compare c { true => nothing; false => 21; });
    show_option(compare c { true => 23; false => nothing; });
    show_option(c ? nothing : 29);
    let value = "value";
    show_string(compare c { true => value + "!"; false => nothing; });
    return nothing;
}
@entrypoint
fn main() -> int { exercise(true); exercise(false); return 0; }
`

const choiceCallTargetWant = "V\nV\nN\nS23\nN\nTvalue!\nN\nN\nV\nN\nS29\nN\n"

func TestChoiceCallTargetRunsVM(t *testing.T) {
	requireVMBackend(t)
	res := runProgramFromSource(t, choiceCallTargetSource, runOptions{captureStdout: true})
	if res.exitCode != 0 || res.stdout != choiceCallTargetWant || res.stderr != "" {
		t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", res.exitCode, res.stdout, choiceCallTargetWant, res.stderr)
	}
}

func TestChoiceCallTargetLLVMValgrind(t *testing.T) {
	outputPath := buildLLVMProgramFromSource(t, choiceCallTargetSource)
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, envWithStdlib(repoRoot(t)), 120*time.Second)
	if hasValgrindMemcheckError(stderr) || strings.Contains(stderr, "are definitely lost") {
		t.Fatalf("memcheck\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 || stdout != choiceCallTargetWant {
		t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", exitCode, stdout, choiceCallTargetWant, stderr)
	}
}
