package vm_test

import (
	"strings"
	"testing"
	"time"
)

// The owner's hello-world contract for printing (2026-08-26): `print("Hello
// world!")`, `print(1)` through the implicit `int.__to(string)`, `print(name)`
// inside a `for` that only reads, `print(s); print(s);` reading one string
// twice -- and no `&`, `own` or `clone` spelled at any of them. `print` takes
// a borrow and converts on the way in; what the conversion produced is the
// statement's to release, which the valgrind row is the instrument for.
const runtimeV2PrintConvertsSource = `
@entrypoint
fn main() -> int {
    print("Hello world!");
    print(1);
    let n: int = 7;
    print(n);
    print(2.5);
    let names: string[] = ["a", "bb"];
    for name in names {
        print(name);
    }
    let s: string = "keep";
    print(s);
    print(s);
    print("x" + "y");
    print("print-converts-ok");
    return 0;
}
`

const runtimeV2PrintConvertsWant = "Hello world!\n1\n7\n2.5E+0\na\nbb\nkeep\nkeep\nxy\nprint-converts-ok\n"

func TestRuntimeV2PrintConvertsItsArgumentOnBothLanes(t *testing.T) {
	for _, backend := range []string{backendVM, backendLLVM} {
		t.Run(backend, func(t *testing.T) {
			t.Setenv(backendEnvVar, backend)
			res := runProgramFromSource(t, runtimeV2PrintConvertsSource, runOptions{})
			if res.exitCode != 0 || res.stderr != "" {
				t.Fatalf("print program failed (exit=%d)\nstdout:\n%s\nstderr:\n%s", res.exitCode, res.stdout, res.stderr)
			}
			// The VM runner does not capture the program's stdout; the exit
			// code is the assertion there and the text is checked natively.
			if backend == backendLLVM && res.stdout != runtimeV2PrintConvertsWant {
				t.Fatalf("print output differs\nwant:\n%s\ngot:\n%s", runtimeV2PrintConvertsWant, res.stdout)
			}
		})
	}
}

func TestRuntimeV2PrintReleasesWhatItConverted(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2PrintConvertsSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a real memcheck error\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("print program failed under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "print-converts-ok") {
		t.Fatalf("print program missing its completion marker; stdout=%q", stdout)
	}
	definiteBytes, definiteBlocks := parseValgrindLeakMatch(valgrindDefiniteLeakRE, stderr)
	indirectBytes, indirectBlocks := parseValgrindLeakMatch(valgrindIndirectLeakRE, stderr)
	if definiteBytes != 0 || definiteBlocks != 0 || indirectBytes != 0 || indirectBlocks != 0 {
		t.Fatalf(
			"a converted print argument leaked: definitely_lost=%dB/%dblk indirectly_lost=%dB/%dblk, want strict zero\nstderr:\n%s",
			definiteBytes, definiteBlocks, indirectBytes, indirectBlocks, stderr,
		)
	}
}
