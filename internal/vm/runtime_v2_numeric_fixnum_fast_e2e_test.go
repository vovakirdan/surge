//go:build runtime_v2_pending

package vm_test

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Runtime parameters keep constant folding away from every operation, so each
// one reaches the emitted inline fixnum path or its runtime fallback. The values
// straddle what that path must refuse: +-2^62 is where a fast add or sub has to
// hand its result to the runtime, a value above 2^80 is a heap operand it must
// never decode, 2^63+3 is a heap int whose conversion to uint64 still succeeds,
// 2^63-1 is the widest inline uint, and 3 - 3 has to come back as canonical zero.
// The strict-zero Memcheck is what shows the fast arms created and released
// nothing the heap arms own.
const numericFixnumFastSource = `
fn add(a: int, b: int) -> int { return a + b; }
fn sub(a: int, b: int) -> int { return a - b; }
fn less(a: int, b: int) -> bool { return a < b; }
fn same(a: int, b: int) -> bool { return a == b; }
fn to_u64(v: int) -> uint64 { return v to uint64; }
fn to_i64(v: int) -> int64 { return v to int64; }
fn uint_to_u64(v: uint) -> uint64 { return v to uint64; }

@entrypoint
fn main() -> int {
    let max_fix: int = 4611686018427387903;
    let min_fix: int = -4611686018427387904;
    let big: int = 2417851639229258349412352;
    if add(max_fix, 1) != 4611686018427387904 { return 1; }
    if sub(min_fix, 1) != -4611686018427387905 { return 2; }
    if add(max_fix, 0) != max_fix || sub(min_fix, 0) != min_fix { return 3; }
    let zero: int = sub(3, 3);
    if zero != 0 || to_u64(zero) != 0:uint64 { return 4; }
    if add(big, 1) != 2417851639229258349412353 || sub(big, big) != 0 { return 5; }
    if add(1, big) != 2417851639229258349412353 || add(sub(0, big), big) != 0 { return 6; }
    if less(big, 1) || less(max_fix, min_fix) { return 7; }
    if less(1, big) == false || less(sub(0, big), min_fix) == false { return 8; }
    if same(big, 2417851639229258349412352) == false || same(big, 1) || same(-7, -7) == false { return 9; }
    let wide: int = 9223372036854775811;
    if to_u64(wide) != 9223372036854775811:uint64 || to_u64(7) != 7:uint64 { return 10; }
    if to_i64(-5) != -5:int64 || to_i64(max_fix) != 4611686018427387903:int64 { return 11; }
    if to_i64(4611686018427387904) != 4611686018427387904:int64 { return 12; }
    if uint_to_u64(9223372036854775807:uint) != 9223372036854775807:uint64 { return 13; }
    if uint_to_u64(9223372036854775808:uint) != 9223372036854775808:uint64 { return 14; }
    if big != 2417851639229258349412352 || max_fix != 4611686018427387903 || wide != 9223372036854775811 { return 15; }
    print("numeric-fixnum-fast-ok");
    return 0;
}
`

func TestRuntimeV2FixnumFastPathHeapWitnessValgrindZero(t *testing.T) {
	requireNumericCastTools(t, true)
	requireOwnershipValgrind(t, exec.LookPath)
	const want = "numeric-fixnum-fast-ok\n"
	t.Setenv(backendEnvVar, backendVM)
	vm := runProgramFromSource(t, numericFixnumFastSource, runOptions{captureStdout: true})
	if vm.exitCode != 0 || vm.stdout != want || vm.stderr != "" {
		t.Fatalf("fixnum fast path VM: exit=%d stdout=%q stderr=%q", vm.exitCode, vm.stdout, vm.stderr)
	}
	t.Setenv(backendEnvVar, backendLLVM)
	bin := buildRuntimeV2CrossingSource(t, numericFixnumFastSource, nil)
	stdout, stderr, code := runBinaryUnderValgrind(t, bin, envWithStdlib(repoRoot(t)), 120*time.Second)
	if code != 0 || stdout != want {
		t.Fatalf("fixnum fast path LLVM: exit=%d stdout=%q\nstderr:\n%s", code, stdout, stderr)
	}
	bytes, blocks := parseValgrindInUseAtExit(t, stderr)
	if bytes != 0 || blocks != 0 || hasValgrindMemcheckError(stderr) ||
		!strings.Contains(stderr, "ERROR SUMMARY: 0 errors from 0 contexts") {
		t.Fatalf("fixnum fast path physical Memcheck: in_use=%d bytes/%d blocks; want strict zero\n%s", bytes, blocks, stderr)
	}
	t.Log("VM and LLVM answers agree; physical Memcheck: in_use=0 bytes/0 blocks; zero errors")
}
