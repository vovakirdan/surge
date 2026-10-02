package vm_test

import (
	"strings"
	"testing"
	"time"
)

const choiceInjectionSpanSource = `fn id<T>(x: T) -> T { return x; }
fn show_option(x: string?) -> nothing {
    compare x { Some(v) => { print("S" + v); } nothing => { print("N"); } };
    return nothing;
}
fn show_success(x: Erring<string, nothing>) -> nothing {
    compare x { Success(v) => { print("R" + v); } finally => { print("N"); } };
    return nothing;
}
fn exercise(c: bool) -> nothing {
    let option: string? = ((id("some" + "!")));
    let result: Erring<string, nothing> = ((id("success" + "!")));
    show_option(option);
    show_success(result);
    show_option(compare c { true => (("arm" + "!")); false => nothing; });
    let ternary: string? = c ? nothing : (("tern" + "!"));
    show_option(ternary);
    let number: int? = clone(7);
    compare number { Some(v) => { print(v to string); } nothing => { print("N"); } };
    return nothing;
}
@entrypoint
fn main() -> int { exercise(true); exercise(false); return 0; }
`

const choiceInjectionSpanWant = "Ssome!\nRsuccess!\nSarm!\nN\n7\nSsome!\nRsuccess!\nN\nStern!\n7\n"

func TestChoiceInjectionSpanRunsVM(t *testing.T) {
	requireVMBackend(t)
	res := runProgramFromSource(t, choiceInjectionSpanSource, runOptions{captureStdout: true})
	if res.exitCode != 0 || res.stdout != choiceInjectionSpanWant || res.stderr != "" {
		t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", res.exitCode, res.stdout, choiceInjectionSpanWant, res.stderr)
	}
}

func TestChoiceInjectionSpanLLVMValgrind(t *testing.T) {
	outputPath := buildLLVMProgramFromSource(t, choiceInjectionSpanSource)
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, envWithStdlib(repoRoot(t)), 120*time.Second)
	if hasValgrindMemcheckError(stderr) || strings.Contains(stderr, "are definitely lost") {
		t.Fatalf("memcheck\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 || stdout != choiceInjectionSpanWant {
		t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", exitCode, stdout, choiceInjectionSpanWant, stderr)
	}
}
