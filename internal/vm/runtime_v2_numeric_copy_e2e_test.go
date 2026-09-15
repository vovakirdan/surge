//go:build runtime_v2_pending

package vm_test

import (
	"crypto/sha256"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Construct the heap value from a fixed-width uint64, so this lifecycle proof
// does not depend on decimal bignum parsing or arithmetic allocation sizes.
// The returning function checks the original and its copy while both live;
// main reads the surviving copy after the original's scope has ended.
func numericCopyOwnershipSource(kind string, array bool) string {
	body := `
fn surviving_copy() -> $K {
    let large: uint64 = 9223372036854775811:uint64;
    let original: $K = large:$K;
    let copied: $K = original;
    let cloned: $K = clone(&copied);
    if ((original:uint64) != large || (copied:uint64) != large || (cloned:uint64) != large) {
        panic("numeric shared owners changed");
    }
    return cloned;
}
@entrypoint
fn main() {
    let survivor: $K = surviving_copy();
    if ((survivor:uint64) != 9223372036854775811:uint64) {
        panic("numeric copy did not outlive its original");
    }
    print("$MARKER");
}
`
	marker := "numeric-" + kind + "-ok"
	if array {
		marker = "numeric-" + kind + "-array-ok"
		body = `
fn surviving_copy() -> $K[] {
    let large: uint64 = 9223372036854775811:uint64;
    let original: $K[] = [large:$K, 7:$K];
    let empty: $K[] = [];
    let copied: $K[] = original + empty;
    if ((original[0]:uint64) != large || (copied[0]:uint64) != large) {
        panic("numeric array copy changed its caller");
    }
    let tail: $K[] = [large:$K];
    return copied + tail;
}
@entrypoint
fn main() {
    let survivor: $K[] = surviving_copy();
    let empty: $K[] = [];
    let another: $K[] = survivor + empty;
    if (len(survivor) != 3 || len(another) != 3) {
        panic("numeric array copy changed its length");
    }
    if ((survivor[0]:uint64) != 9223372036854775811:uint64 ||
       (another[2]:uint64) != 9223372036854775811:uint64 || another[1] != 7:$K) {
        panic("numeric array leaves did not outlive their originals");
    }
    print("$MARKER");
}
`
	}
	return strings.NewReplacer("$K", kind, "$MARKER", marker).Replace(body)
}

func TestRuntimeV2NumericCopyOwnershipValgrindZero(t *testing.T) {
	requireNumericProofRun(t)
	for _, tool := range []string{"clang", "ar"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required numeric source proof tool %s unavailable: %v", tool, err)
		}
	}
	requireOwnershipValgrind(t, exec.LookPath)
	for _, row := range []struct {
		kind  string
		array bool
	}{{"int", false}, {"uint", false}, {"int", true}, {"uint", true}} {
		name := row.kind
		if row.array {
			name += "-array"
		}
		t.Run(name, func(t *testing.T) {
			source := numericCopyOwnershipSource(row.kind, row.array)
			want := "numeric-" + name + "-ok\n"
			t.Setenv(backendEnvVar, backendVM)
			vm := runProgramFromSource(t, source, runOptions{captureStdout: true})
			if vm.exitCode != 0 || vm.stdout != want || strings.TrimSpace(vm.stderr) != "" {
				t.Fatalf("numeric %s VM owner proof: code=%d\nstdout:\n%s\nstderr:\n%s", name, vm.exitCode, vm.stdout, vm.stderr)
			}
			t.Logf("numeric %s VM source_SHA256=%x stdout=%q", name, sha256.Sum256([]byte(source)), vm.stdout)
			t.Setenv(backendEnvVar, backendLLVM)
			bin := buildRuntimeV2CrossingSource(t, source, nil)
			stdout, stderr, code := runBinaryUnderValgrind(t, bin, envWithStdlib(repoRoot(t)), 120*time.Second)
			if code != 0 || stdout != want {
				t.Fatalf("numeric %s native owner proof: code=%d\nstdout:\n%s\nstderr:\n%s", name, code, stdout, stderr)
			}
			bytes, blocks := parseValgrindInUseAtExit(t, stderr)
			if bytes != 0 || blocks != 0 || hasValgrindMemcheckError(stderr) ||
				!strings.Contains(stderr, "ERROR SUMMARY: 0 errors from 0 contexts") {
				t.Fatalf("numeric %s physical Memcheck: in_use=%d bytes/%d blocks; want strict zero\n%s", name, bytes, blocks, stderr)
			}
			t.Logf("numeric %s LLVM stdout=%q; physical Memcheck: in_use=0 bytes/0 blocks; zero errors", name, stdout)
		})
	}
}
