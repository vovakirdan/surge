//go:build runtime_v2_pending

package vm_test

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

var borrowedParameterOwnershipCases = []struct {
	name string
	body string
	args string
	want string
}{
	{
		name: "readonly-array",
		body: `fn subject(value: $K) -> nothing {
    let values: $K[] = Array::<$K>::with_len_value(2:uint, value);
    if (len(values) != 2 || (values[0]:uint64) != 9223372036854775811:uint64 ||
        (values[1]:uint64) != 9223372036854775811:uint64) {
        panic("borrowed array values changed");
    }
}
`,
	},
	{
		name: "readonly-fixed-array",
		body: `fn subject(value: $K) -> nothing {
    let values: ArrayFixed<$K, 3> = ArrayFixed::<$K, 3>::with_len_value(3:uint, value);
    if (len(values) != 3 || (values[0]:uint64) != 9223372036854775811:uint64 ||
        (values[1]:uint64) != 9223372036854775811:uint64 ||
        (values[2]:uint64) != 9223372036854775811:uint64) {
        panic("borrowed fixed array values changed");
    }
}
`,
	},
	{
		name: "direct-rebind",
		body: `fn subject(value: $K) -> $K {
    value = value + 1:$K;
    return value;
}
`,
		want: "9223372036854775812",
	},
	{
		name: "conditional-false",
		body: borrowedParameterConditionalBody,
		args: ", false",
		want: "9223372036854775811",
	},
	{
		name: "conditional-true",
		body: borrowedParameterConditionalBody,
		args: ", true",
		want: "9223372036854775812",
	},
	{
		name: "loop-zero",
		body: borrowedParameterLoopBody,
		args: ", 0:uint64",
		want: "9223372036854775811",
	},
	{
		name: "loop-multiple",
		body: borrowedParameterLoopBody,
		args: ", 3:uint64",
		want: "9223372036854775814",
	},
	{
		name: "explicit-drop",
		body: `fn subject(value: $K) -> nothing {
    @drop value;
}
`,
	},
	{
		name: "drop-then-rebind",
		body: `fn subject(value: $K) -> $K {
    @drop value;
    let replacement: uint64 = 9223372036854775813:uint64;
    value = replacement:$K;
    return value;
}
`,
		want: "9223372036854775813",
	},
}

const borrowedParameterConditionalBody = `fn subject(value: $K, change: bool) -> $K {
    if change { value = value + 1:$K; }
    return value;
}
`

const borrowedParameterLoopBody = `fn subject(value: $K, rounds: uint64) -> $K {
    let mut i: uint64 = 0:uint64;
    while i < rounds {
        value = value + 1:$K;
        i = i + 1:uint64;
    }
    return value;
}
`

// Fixed-width construction makes both int and uint heap owners. Returned
// scalars are checked after subject's cleanup, then released before the caller
// rereads its original. Arrays also die before that independent caller check.
func borrowedParameterOwnershipSource(kind, body, args, want, marker string) string {
	call := "subject(original" + args + ");"
	if want != "" {
		call = "{\n        let survivor: $K = subject(original" + args + ");\n" +
			"        if ((survivor:uint64) != " + want + ":uint64) {\n" +
			"            panic(\"borrowed parameter result did not survive callee cleanup\");\n        }\n    }"
	}
	source := body + "\n" + strings.ReplaceAll(`@entrypoint
fn main() {
    let large: uint64 = 9223372036854775811:uint64;
    let original: $K = large:$K;
    $CALL
    if ((original:uint64) != large) {
        panic("borrowed parameter changed or consumed its caller");
    }
    print("$MARKER");
}
`, "$CALL", call)
	return strings.NewReplacer("$K", kind, "$MARKER", marker).Replace(source)
}

func TestRuntimeV2BorrowedParameterOwnershipValgrindZero(t *testing.T) {
	requireNumericProofRun(t)
	for _, tool := range []string{"clang", "ar"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("borrowed parameter ownership proof requires %s: %v", tool, err)
		}
	}
	requireOwnershipValgrind(t, exec.LookPath)
	t.Setenv(backendEnvVar, backendLLVM)
	for _, kind := range []string{"int", "uint"} {
		for _, row := range borrowedParameterOwnershipCases {
			t.Run(kind+"-"+row.name, func(t *testing.T) {
				marker := "borrowed-parameter-" + kind + "-" + row.name + "-ok"
				source := borrowedParameterOwnershipSource(kind, row.body, row.args, row.want, marker)
				t.Logf("LLVM source_SHA256=%x", sha256.Sum256([]byte(source)))
				bin := buildRuntimeV2CrossingSource(t, source, nil)
				binary, err := os.ReadFile(bin)
				if err != nil {
					t.Fatalf("read built ownership proof binary %s: %v", bin, err)
				}
				t.Logf("LLVM binary_path=%q binary_SHA256=%x", bin, sha256.Sum256(binary))
				stdout, stderr, code := runBinaryUnderValgrind(t, bin, envWithStdlib(repoRoot(t)), 120*time.Second)
				t.Logf("LLVM Valgrind raw: exit_code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
				if code != 0 || stdout != marker+"\n" {
					t.Fatalf("borrowed parameter native value proof: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
				}
				bytes, blocks := parseValgrindInUseAtExit(t, stderr)
				if bytes != 0 || blocks != 0 || hasValgrindMemcheckError(stderr) ||
					!strings.Contains(stderr, "ERROR SUMMARY: 0 errors from 0 contexts") {
					t.Fatalf("borrowed parameter physical Memcheck: in_use=%d bytes/%d blocks; want strict zero\n%s", bytes, blocks, stderr)
				}
				t.Logf("LLVM stdout=%q; physical Memcheck: in_use=0 bytes/0 blocks; zero errors", stdout)
			})
		}
	}
}

func TestRuntimeV2BorrowedParameterOwnershipVM(t *testing.T) {
	requireNumericProofRun(t)
	t.Setenv(backendEnvVar, backendVM)
	for _, kind := range []string{"int", "uint"} {
		for _, row := range borrowedParameterOwnershipCases {
			t.Run(kind+"-"+row.name, func(t *testing.T) {
				marker := "borrowed-parameter-" + kind + "-" + row.name + "-ok"
				source := borrowedParameterOwnershipSource(kind, row.body, row.args, row.want, marker)
				t.Logf("VM source_SHA256=%x", sha256.Sum256([]byte(source)))
				result := runProgramFromSource(t, source, runOptions{captureStdout: true})
				// Normal rt_exit checks every RC heap object after shutdown cleanup.
				// Native Valgrind separately witnesses omitted explicit final drops.
				if result.exitCode != 0 || result.stdout != marker+"\n" || strings.TrimSpace(result.stderr) != "" {
					t.Fatalf("borrowed parameter VM value/shutdown proof: code=%d\nstdout:\n%s\nstderr:\n%s", result.exitCode, result.stdout, result.stderr)
				}
				t.Logf("VM stdout=%q; normal shutdown RC heap check passed", result.stdout)
			})
		}
	}
}
