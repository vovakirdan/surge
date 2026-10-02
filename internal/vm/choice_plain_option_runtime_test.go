package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A choice that joins a plain value with `nothing` is an Option (owner ruling
// 2026-09-29): `c ? 1 : nothing` is `Option<int>`, the plain value wrapped in
// `Some`, in either order, nested, in a compare, and under an annotated
// Option target, a return or an assignment. The ternary used to be typed
// `int`, so its `nothing` path produced a value `int` cannot hold: natively
// a 0 (`Some(0)` under an `int?` target), on the VM a panic at the first use.
// The program walks every row with the condition true and false, so each
// `nothing` branch is taken on both backends.
const choicePlainOptionSource = `fn show(o: int?) -> nothing { compare o { Some(v) => { print("S" + (v to string)); } nothing => { print("N"); } }; return nothing; }
fn shows(o: string?) -> nothing { compare o { Some(v) => { print("T" + v); } nothing => { print("N"); } }; return nothing; }
fn rt(c: bool) -> int? { return c ? 5 : nothing; }
fn ret2(c: bool) -> Option<int> { return c ? nothing : 6; }
fn run(c: bool, d: bool) -> nothing {
    let a = c ? 1 : nothing;
    show(a);
    let b = c ? nothing : 2;
    show(b);
    show(c ? 3 : nothing);
    let e = c ? 4 : d ? 5 : nothing;
    show(e);
    let f = c ? (d ? 6 : nothing) : nothing;
    show(f);
    let g = c ? nothing : (d ? nothing : 7);
    show(g);
    let k = c ? 1 : 2;
    let h = compare k { 1 => 8; _ => nothing; };
    show(h);
    let i = compare k { 1 => nothing; _ => 9; };
    show(i);
    let j = compare k { 1 => 10; _ => d ? 11 : nothing; };
    show(j);
    let m: Option<int> = c ? 12 : nothing;
    show(m);
    let mut p: int? = nothing;
    p = c ? 14 : nothing;
    show(p);
    show(rt(c));
    show(ret2(c));
    let s = c ? "yes" : nothing;
    shows(s);
    let x = "own";
    shows(c ? x + "!" : nothing);
    return nothing;
}
@entrypoint
fn main() -> int { run(true, true); print("--"); run(false, true); print("--"); run(false, false); print("--"); run(true, false); return 0; }
`

const choicePlainOptionWant = "S1\nN\nS3\nS4\nS6\nN\nS8\nN\nS10\nS12\nS14\nS5\nN\nTyes\nTown!\n--\nN\nS2\nN\nS5\nN\nN\nN\nS9\nS11\nN\nN\nN\nS6\nN\nN\n--\nN\nS2\nN\nN\nN\nS7\nN\nS9\nN\nN\nN\nN\nS6\nN\nN\n--\nS1\nN\nS3\nS4\nN\nN\nS8\nN\nS10\nS12\nS14\nS5\nN\nTyes\nTown!\n"

func TestChoiceOfAPlainValueAndNothingRunsVM(t *testing.T) {
	requireVMBackend(t)
	res := runProgramFromSource(t, choicePlainOptionSource, runOptions{captureStdout: true})
	if res.exitCode != 0 || res.stdout != choicePlainOptionWant {
		t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", res.exitCode, res.stdout, choicePlainOptionWant, res.stderr)
	}
}

func TestChoiceOfAPlainValueAndNothingLLVMValgrind(t *testing.T) {
	outputPath := buildLLVMProgramFromSource(t, choicePlainOptionSource)
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, envWithStdlib(repoRoot(t)), 120*time.Second)
	if hasValgrindMemcheckError(stderr) || strings.Contains(stderr, "are definitely lost") {
		t.Fatalf("a Some-wrapped branch was not released as the Option it is\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 || stdout != choicePlainOptionWant {
		t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", exitCode, stdout, choicePlainOptionWant, stderr)
	}
}
