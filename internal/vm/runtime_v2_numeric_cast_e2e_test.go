//go:build runtime_v2_pending

package vm_test

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Parameters prevent constant folding from answering for the conversions. The
// caller reads each result after the callee has ended, then reads its sources
// again. Powers above the signed/unsigned tag ceilings force heap intermediates
// while remaining representable in the fixed-width destination.
const numericCastOwnershipSource = `
fn cast_f64_int(value: float64) -> int { return value to int; }
fn cast_f64_uint(value: float64) -> uint { return value to uint; }
fn cast_float_i64(value: float) -> int64 { return value to int64; }
fn cast_float_u64(value: float) -> uint64 { return value to uint64; }
fn cast_int_u64(value: int) -> uint64 { return value to uint64; }
fn cast_int_f64(value: int) -> float64 { return value to float64; }
fn cast_uint_f64(value: uint) -> float64 { return value to float64; }
fn borrow_int_i64(value: int) -> int64 { return value to int64; }
fn borrow_uint_u64(value: uint) -> uint64 { return value to uint64; }
fn borrow_float_f64(value: float) -> float64 { return value to float64; }

@entrypoint
fn main() -> int {
    let fixed: float64 = 42.75:float64;
    if cast_f64_int(fixed) != 42 { return 1; }
    if cast_f64_uint(fixed) != 42:uint { return 2; }

    let signed_float: float = 4611686018427387904.75;
    let unsigned_float: float = 9223372036854775808.75;
    if cast_float_i64(signed_float) != 4611686018427387904:int64 { return 3; }
    if cast_float_u64(unsigned_float) != 9223372036854775808:uint64 { return 4; }

    let large: uint64 = 9223372036854775808:uint64;
    let signed: int = large to int;
    let unsigned: uint = large to uint;
    if cast_int_u64(signed) != large { return 5; }
    if cast_int_f64(signed) != 9223372036854775808.0:float64 { return 6; }
    if cast_uint_f64(unsigned) != 9223372036854775808.0:float64 { return 7; }

    // These outputs require an owner transfer, not release of the result.
    if cast_f64_int(4611686018427387904.0:float64) != 4611686018427387904 { return 8; }
    if cast_f64_uint(9223372036854775808.0:float64) != 9223372036854775808:uint { return 9; }

    // The same owning paths also encounter NULL and inline tagged integers.
    if cast_f64_int(0.0:float64) != 0 || cast_f64_uint(0.0:float64) != 0:uint { return 10; }
    if cast_float_i64(0.0) != 0:int64 || cast_float_u64(0.0) != 0:uint64 { return 11; }
    if cast_float_i64(7.75) != 7:int64 || cast_float_u64(7.75) != 7:uint64 { return 12; }
    if cast_int_u64(0) != 0:uint64 || cast_int_u64(7) != 7:uint64 { return 13; }
    if cast_int_f64(0) != 0.0:float64 || cast_uint_f64(0:uint) != 0.0:float64 { return 14; }
    if cast_int_f64(7) != 7.0:float64 || cast_uint_f64(7:uint) != 7.0:float64 { return 15; }

    let borrowed_int: int = 4611686018427387904;
    let borrowed_float: float = 42.5;
    if borrow_int_i64(borrowed_int) != 4611686018427387904:int64 { return 16; }
    if borrow_uint_u64(unsigned) != large { return 17; }
    if borrow_float_f64(borrowed_float) != 42.5:float64 { return 18; }

    if fixed != 42.75:float64 || signed_float != 4611686018427387904.75 ||
       unsigned_float != 9223372036854775808.75 || signed != 9223372036854775808 ||
       unsigned != 9223372036854775808:uint || borrowed_int != 4611686018427387904 ||
       borrowed_float != 42.5 { return 19; }
    print("numeric-cast-temporaries-ok");
    return 0;
}
`

func requireNumericCastTools(t *testing.T, native bool) {
	t.Helper()
	requireNumericProofRun(t)
	if native {
		for _, tool := range []string{"clang", "ar"} {
			if _, err := exec.LookPath(tool); err != nil {
				t.Fatalf("required numeric cast tool %s unavailable: %v", tool, err)
			}
		}
	}
}

func TestRuntimeV2NumericCastTemporariesValgrindZero(t *testing.T) {
	requireNumericCastTools(t, true)
	requireOwnershipValgrind(t, exec.LookPath)
	const want = "numeric-cast-temporaries-ok\n"
	t.Setenv(backendEnvVar, backendVM)
	vm := runProgramFromSource(t, numericCastOwnershipSource, runOptions{captureStdout: true})
	if vm.exitCode != 0 || vm.stdout != want || vm.stderr != "" {
		t.Fatalf("numeric cast VM: exit=%d stdout=%q stderr=%q", vm.exitCode, vm.stdout, vm.stderr)
	}
	t.Setenv(backendEnvVar, backendLLVM)
	bin := buildRuntimeV2CrossingSource(t, numericCastOwnershipSource, nil)
	stdout, stderr, code := runBinaryUnderValgrind(t, bin, envWithStdlib(repoRoot(t)), 120*time.Second)
	if code != 0 || stdout != want {
		t.Fatalf("numeric cast LLVM: exit=%d stdout=%q\nstderr:\n%s", code, stdout, stderr)
	}
	bytes, blocks := parseValgrindInUseAtExit(t, stderr)
	if bytes != 0 || blocks != 0 || hasValgrindMemcheckError(stderr) ||
		!strings.Contains(stderr, "ERROR SUMMARY: 0 errors from 0 contexts") {
		t.Fatalf("numeric cast physical Memcheck: in_use=%d bytes/%d blocks; want strict zero\n%s", bytes, blocks, stderr)
	}
	t.Log("VM and LLVM answers agree; physical Memcheck: in_use=0 bytes/0 blocks; zero errors")
}

func TestVMNumericCastFailureContract(t *testing.T) {
	backend := testBackend(t)
	requireNumericCastTools(t, backend == backendLLVM)
	for _, row := range []struct{ name, inputType, outputType, literal, message string }{
		{"float_i64_overflow", "float", "int64", "18446744073709551616.0", "integer overflow"},
		{"float_u64_overflow", "float", "uint64", "18446744073709551616.0", "unsigned overflow"},
		{"int_u64_overflow", "int", "uint64", "18446744073709551616", "unsigned overflow"},
		{"int_f64_overflow", "int", "float64", "1" + strings.Repeat("0", 310), "float overflow"},
		{"uint_f64_overflow", "uint", "float64", "1" + strings.Repeat("0", 310), "float overflow"},
		{"float_i8_overflow", "float", "int8", "256.75", "integer overflow"},
		{"int_u8_overflow", "int", "uint8", "256", "unsigned overflow"},
		{"f64_uint_negative", "float64", "uint", "-1.0", "negative float to uint"},
		// A negative inline int must leave the fixnum fast path for the runtime
		// conversion that owns the refusal, not decode into a huge uint64.
		{"int_u64_negative_fixnum", "int", "uint64", "-1", "cannot convert negative int to uint"},
	} {
		t.Run(row.name, func(t *testing.T) {
			// The conversion sees a runtime parameter. A rejected constant cast
			// cannot stand in for reaching the numeric failure continuation.
			source := fmt.Sprintf(`
fn rejected_cast(value: %s) -> %s { return value to %s; }
@entrypoint
fn main() {
    let input: %s = %s;
    print("numeric-cast-entered");
    let result = rejected_cast(input);
    print(result to string);
    print("numeric-cast-unexpected-success");
}
`, row.inputType, row.outputType, row.outputType, row.inputType, row.literal)
			result := runProgramFromSource(t, source, runOptions{captureStdout: true})
			message, code := row.message, 0
			// The in-process VM reports its VMError separately; only native
			// panic exits a process with status one. Both must reach VM3202.
			if backend == backendLLVM {
				code = 1
				if row.name == "f64_uint_negative" {
					message = "cannot convert negative float to uint"
				}
			}
			want := "panic VM3202: " + message
			if result.exitCode != code || result.stdout != "numeric-cast-entered\n" ||
				strings.Count(result.stderr, want) != 1 {
				t.Fatalf("%s/%s must reach %q: exit=%d stdout=%q stderr=%q artifacts=%s",
					backend, row.name, want, result.exitCode, result.stdout, result.stderr, result.artifactsDir)
			}
		})
	}
}
