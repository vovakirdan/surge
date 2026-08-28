package vm_test

import (
	"os"
	"testing"
)

// The allocation-refusal negative control, end to end.
//
// A real allocator cannot be made to refuse on demand, so the control is a
// BUILD: SURGE_INTERNAL_TEST_ALLOC_REFUSAL names one emitted allocation site and
// the compiler asks the allocator for 2^64-1 bytes there. Nothing else differs
// from an ordinary build -- no allocator is stubbed, no branch is planted, and
// the refusal travels the same rt_alloc -> NULL -> guard path a real one would.
//
// What this row pins is the difference the owner ruling of 2026-08-28 exists to
// make. On the tree before the guard, both arms of the armed build were killed
// by SIGSEGV with an empty stderr -- `Segmentation fault (core dumped)` under a
// shell, exit code -1 as Go reports a signalled process -- because the generated
// body stored through the NULL that rt_alloc.c returns. With the guard they
// answer `panic: out of memory: could not allocate <type>` and exit 1.

const allocRefusalEnv = "SURGE_INTERNAL_TEST_ALLOC_REFUSAL"

// allocRefusalArrayProgram builds one dynamic array and reads it back. Its exit
// code is the sum, so an unarmed run that produced no array at all could not
// answer 6.
const allocRefusalArrayProgram = `@entrypoint
fn main() -> int {
    let a: int[] = [1, 2, 3];
    return a[0] + a[1] + a[2];
}
`

// allocRefusalAsyncProgram reaches the site the ruling is about: the suspension
// frame, which is storage the RUNTIME owns past the await and the emitter takes
// from rt_alloc rather than from the stack.
const allocRefusalAsyncProgram = `async fn add(a: int, b: int) -> int {
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

// TestRuntimeV2AllocationRefusalReportsTheTypeItCouldNotAllocate is the row.
//
// The unarmed arm is not decoration: a stand whose program dies whichever way it
// is built proves nothing about the guard, and the exit code it asserts is a sum
// only a program that actually built its array can produce.
func TestRuntimeV2AllocationRefusalReportsTheTypeItCouldNotAllocate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		program  string
		site     string
		typeName string
		served   int
	}{
		{
			name:     "array_literal_elements",
			program:  allocRefusalArrayProgram,
			site:     "array-literal-elements",
			typeName: "Array<int>",
			served:   6,
		},
		{
			name:     "runtime_owned_suspension_frame",
			program:  allocRefusalAsyncProgram,
			site:     "runtime-owned-storage",
			typeName: "__AsyncState$add",
			served:   6,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := buildAndRunWithAllocRefusal(t, tc.program, "")
			if code != tc.served || stderr != "" {
				t.Fatalf("an ordinary build did not run to completion: exit=%d stdout=%q stderr=%q",
					code, stdout, stderr)
			}

			_, stderr, code = buildAndRunWithAllocRefusal(t, tc.program, tc.site)
			want := "panic: out of memory: could not allocate " + tc.typeName + "\n"
			if code != 1 {
				t.Fatalf("a refused allocation exited %d, want 1 (-1 is the SIGSEGV this guard replaced); stderr=%q",
					code, stderr)
			}
			if stderr != want {
				t.Fatalf("a refused allocation reported %q, want %q", stderr, want)
			}
		})
	}
}

// buildAndRunWithAllocRefusal builds the program with the named site armed (or
// with nothing armed, for the empty string) and runs the binary.
func buildAndRunWithAllocRefusal(t *testing.T, program, site string) (stdout, stderr string, exitCode int) {
	t.Helper()
	ensureLLVMToolchain(t)
	root := repoRoot(t)
	artifacts := newTestArtifacts(t, root)
	srcPath := artifactSourcePath(artifacts)
	if err := os.WriteFile(srcPath, []byte(program), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	surge := buildSurgeBinary(t, root)

	env := envForParity(root)
	if site != "" {
		env = overrideEnvVar(env, allocRefusalEnv, site)
	}
	buildOut, buildErr, buildCode := runSurgeWithEnv(t, root, surge, env, "build", srcPath)
	if buildCode != 0 {
		t.Fatalf("build failed (exit=%d)\nstdout:\n%s\nstderr:\n%s", buildCode, buildOut, buildErr)
	}
	outputPath := llvmOutputPath(root, srcPath)
	trackLLVMBuildArtifacts(root, artifacts, outputPath)

	return runBinary(t, outputPath)
}
