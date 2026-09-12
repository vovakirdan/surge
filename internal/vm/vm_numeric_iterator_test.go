package vm_test

import "testing"

// The normal backend matrix runs both rows once with SURGE_BACKEND=vm and
// once with SURGE_BACKEND=llvm. Float bounds have their own LLVM-only row.
func TestVMNumericIteratorFloatArrays(t *testing.T) {
	result := runProgramFromSource(t, numericIteratorArraySource, runOptions{captureStdout: true})
	if result.exitCode != 0 || result.stdout != "numeric-iterator-arrays-witness\n" || result.stderr != "" {
		t.Fatalf("float array iterator: exit=%d stdout=%q stderr=%q artifacts=%s", result.exitCode, result.stdout, result.stderr, result.artifactsDir)
	}
}

func TestVMNumericIteratorFastLatch(t *testing.T) {
	result := runProgramFromSource(t, numericIteratorFastSource, runOptions{captureStdout: true})
	if result.exitCode != 0 || result.stdout != "numeric-iterator-fast-witness\n" || result.stderr != "" {
		t.Fatalf("numeric fast latch: exit=%d stdout=%q stderr=%q artifacts=%s", result.exitCode, result.stdout, result.stderr, result.artifactsDir)
	}
}
