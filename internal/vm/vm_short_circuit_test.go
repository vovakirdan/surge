//go:build !golden
// +build !golden

package vm_test

import "testing"

func TestLogicalOperatorsShortCircuit(t *testing.T) {
	source := `
fn rhs_panic() -> bool {
    panic("rhs evaluated");
    return false;
}

@entrypoint
fn main() -> int {
    let s: string = "abc";
    let i: int = 3;

    if !(true && true) {
        return 1;
    }
    if !(false || true) {
        return 2;
    }
    if i < (s.__len() to int) && s[i] == 34:uint32 {
        return 3;
    }
    if true || rhs_panic() {
        return 0;
    }

    return 4;
}
`

	for _, backend := range []string{backendVM, backendLLVM} {
		t.Run(backend, func(t *testing.T) {
			t.Setenv(backendEnvVar, backend)
			res := runProgramFromSource(t, source, runOptions{})
			if res.exitCode != 0 {
				t.Fatalf("exit code: want 0, got %d\nstderr:\n%s", res.exitCode, res.stderr)
			}
			if res.stderr != "" {
				t.Fatalf("unexpected stderr:\n%s", res.stderr)
			}
			if res.stdout != "" {
				t.Fatalf("unexpected stdout:\n%s", res.stdout)
			}
		})
	}
}
