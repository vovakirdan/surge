//go:build runtime_v2_pending

package vm_test

import (
	"crypto/sha256"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRuntimeV2UintLiteralValueValgrindZero(t *testing.T) {
	requireNumericCastTools(t, true)
	requireOwnershipValgrind(t, exec.LookPath)
	huge := "1" + strings.Repeat("0", 310)
	for _, row := range []struct{ name, typ, literal, decimal string }{
		{"above_u64", "uint", "18446744073709551616", "18446744073709551616"},
		{"decimal310", "uint", huge, huge},
		{"hex_above_u64", "uint", "0x10000000000000000", "18446744073709551616"},
		{"alias_uint", "Count", "18446744073709551616", "18446744073709551616"},
	} {
		t.Run(row.name, func(t *testing.T) {
			// The oracle is the full printed magnitude. Comparing two equally
			// mistyped uint literals could make a pair of zeroes look correct.
			source := fmt.Sprintf(`
type Count = uint;
@entrypoint
fn main() {
    let value: %s = %s;
    print("uint-literal-entered");
    print(value to string);
}
`, row.typ, row.literal)
			want := "uint-literal-entered\n" + row.decimal + "\n"
			t.Setenv(backendEnvVar, backendVM)
			vm := runProgramFromSource(t, source, runOptions{captureStdout: true})
			if vm.exitCode != 0 || vm.stdout != want || vm.stderr != "" {
				t.Fatalf("uint literal VM magnitude: exit=%d stdout=%q want=%q stderr=%q", vm.exitCode, vm.stdout, want, vm.stderr)
			}
			t.Setenv(backendEnvVar, backendLLVM)
			bin := buildRuntimeV2CrossingSource(t, source, nil)
			stdout, stderr, code := runBinaryUnderValgrind(t, bin, envWithStdlib(repoRoot(t)), 120*time.Second)
			if code != 0 || stdout != want {
				t.Fatalf("uint literal native magnitude: exit=%d stdout=%q want=%q\nstderr:\n%s", code, stdout, want, stderr)
			}
			bytes, blocks := parseValgrindInUseAtExit(t, stderr)
			if bytes != 0 || blocks != 0 || hasValgrindMemcheckError(stderr) ||
				!strings.Contains(stderr, "ERROR SUMMARY: 0 errors from 0 contexts") {
				t.Fatalf("uint literal physical Memcheck: in_use=%d bytes/%d blocks; want strict zero\n%s", bytes, blocks, stderr)
			}
			t.Logf("source_SHA256=%x VM/native magnitude=%s; physical Memcheck 0 bytes/0 blocks/0 errors", sha256.Sum256([]byte(source)), row.decimal)
		})
	}
}
