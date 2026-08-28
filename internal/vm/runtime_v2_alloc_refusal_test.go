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

// allocRefusalPushProgram grows an array through the path a program actually
// takes. `a.push(...)` reaches rt_realloc, not rt_alloc: the first writing of
// this guard tested the spelling instead of the answer, so this call stored the
// refusal into the array header, recorded the grown capacity over it, and then
// wrote the element through the null.
const allocRefusalPushProgram = `@entrypoint
fn main() -> int {
    let mut a: int[] = [1];
    a.push(2);
    a.push(3);
    return a[0] + a[1] + a[2];
}
`

// allocRefusalReserveProgram reaches the same reallocation through `reserve`,
// which is the other caller of it and grows without an element to store.
const allocRefusalReserveProgram = `@entrypoint
fn main() -> int {
    let mut a: int[] = [1, 2, 3];
    a.reserve(64:uint);
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
			name:     "array_grow_push",
			program:  allocRefusalPushProgram,
			site:     "array-grow-push",
			typeName: "Array<int>",
			served:   6,
		},
		{
			name:     "array_grow_reserve",
			program:  allocRefusalReserveProgram,
			site:     "array-grow-reserve",
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

// refusedStringProgram asks for a string no allocator serves. It needs no
// control build: 2^63-1 bytes is refused on every machine, and the refusal
// travels rt_string_repeat's own rt_alloc.
const refusedStringProgram = `@entrypoint
fn main() -> int {
    let s: string = "x" * 9223372036854775807;
    if s == "" {
        return 7;
    }
    return 0;
}
`

// TestARefusedStringReportsInsteadOfAnsweringTheEmptyString is the string half
// of the same defect, and it is a different failure from the array half.
//
// A refused string was not a fault: the readers answer NULL and 0 for a handle
// that is not there, so the program carried on with a string that does not
// exist. On the tree before the fix this exits 0 with an empty stderr — it does
// not report, and it does not even take the `s == ""` branch, so the program can
// tell neither that it got the string nor that it did not. The report is in the
// runtime rather than in the generated code because `string` is not a result
// type: a dozen entry points reach the same two allocations, several of them
// indirectly, and no caller can be handed a refusal it has no way to represent.
func TestARefusedStringReportsInsteadOfAnsweringTheEmptyString(t *testing.T) {
	_, stderr, code := buildAndRunWithAllocRefusal(t, refusedStringProgram, "")
	want := "panic: out of memory: could not allocate String\n"
	if code != 1 {
		t.Fatalf("a refused string exited %d, want 1 (0 is the silent wrong answer this fix replaced); stderr=%q",
			code, stderr)
	}
	if stderr != want {
		t.Fatalf("a refused string reported %q, want %q", stderr, want)
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
