package vm_test

import (
	"os"
	"testing"
)

// A function with a result whose body ends in a compare every arm of which
// returns never reaches its end. Each program below answers 6 through an
// arm's own return; it must build and answer 6 on both backends. The native
// build is the one that could fail: a `return` of `nothing` at the unreachable
// end is written `ret i8 0` in a function that returns something else, and llc
// refuses the module.

const tailCompareAwaitSource = `async fn add(a: int, b: int) -> int {
    checkpoint().await();
    return a + b;
}

@entrypoint
fn main() -> int {
    compare add(2, 4).await() {
        Success(v) => return v;
        Cancelled() => return 9;
    };
}
`

const tailCompareOptionSource = `fn pick(x: Option<int>) -> int {
    compare x {
        Some(v) => return v;
        nothing => return 0;
    };
}

@entrypoint
fn main() -> int {
    let six: Option<int> = Some(6);
    let none: Option<int> = nothing;
    return pick(six) + pick(none);
}
`

const tailCompareBoolSource = `fn classify(flag: bool) -> int {
    compare flag {
        true => { return 6; }
        false => { return 0; }
    };
}

@entrypoint
fn main() -> int {
    return classify(true);
}
`

// The control: a local still owned at the end of the body is dropped there,
// so the compare is not the body's last statement. This shape built before
// the fix, and must still build after it.
const tailCompareLiveLocalSource = `fn pick_kept(x: Option<int>) -> int {
    let kept: int[] = [1, 2, 3];
    compare x {
        Some(v) => return v + kept[0] - 1;
        nothing => return kept[1] - 2;
    };
}

@entrypoint
fn main() -> int {
    let six: Option<int> = Some(6);
    let none: Option<int> = nothing;
    return pick_kept(six) + pick_kept(none);
}
`

func TestAFunctionEndingInACompareEveryArmOfWhichReturnsRunsOnBothBackends(t *testing.T) {
	for _, tc := range []struct {
		name    string
		program string
	}{
		{name: "await_compare", program: tailCompareAwaitSource},
		{name: "option_compare", program: tailCompareOptionSource},
		{name: "bool_compare", program: tailCompareBoolSource},
		{name: "live_local_control", program: tailCompareLiveLocalSource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("vm", func(t *testing.T) {
				root := repoRoot(t)
				srcPath := writeTailCompareSource(t, root, tc.program)
				surge := buildSurgeBinary(t, root)
				stdout, stderr, code := runSurgeWithEnv(t, root, surge, envForParity(root), "run", "--backend=vm", srcPath)
				assertTailCompareAnswer(t, "vm", code, stdout, stderr)
			})
			t.Run("llvm", func(t *testing.T) {
				ensureLLVMToolchain(t)
				root := repoRoot(t)
				artifacts := newTestArtifacts(t, root)
				srcPath := artifactSourcePath(artifacts)
				if err := os.WriteFile(srcPath, []byte(tc.program), 0o600); err != nil {
					t.Fatalf("write source: %v", err)
				}
				surge := buildSurgeBinary(t, root)
				buildOut, buildErr, buildCode := runSurgeWithEnv(t, root, surge, envForParity(root), "build", srcPath)
				outputPath := llvmOutputPath(root, srcPath)
				trackLLVMBuildArtifacts(root, artifacts, outputPath)
				if buildCode != 0 {
					t.Fatalf("BUILD-FAILED: native build exited %d\nstdout:\n%s\nstderr:\n%s", buildCode, buildOut, buildErr)
				}
				stdout, stderr, code := runBinary(t, outputPath)
				assertTailCompareAnswer(t, "llvm", code, stdout, stderr)
			})
		})
	}
}

func writeTailCompareSource(t *testing.T, root, program string) string {
	t.Helper()
	artifacts := newTestArtifacts(t, root)
	srcPath := artifactSourcePath(artifacts)
	if err := os.WriteFile(srcPath, []byte(program), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return srcPath
}

func assertTailCompareAnswer(t *testing.T, lane string, code int, stdout, stderr string) {
	t.Helper()
	if code != 6 || stderr != "" {
		t.Fatalf("WRONG-ANSWER: %s exited %d, want 6 through an arm's return\nstdout:\n%s\nstderr:\n%s",
			lane, code, stdout, stderr)
	}
}
