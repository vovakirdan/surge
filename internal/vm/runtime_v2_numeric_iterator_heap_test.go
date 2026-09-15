package vm_test

import (
	"crypto/sha256"
	"testing"
)

func TestVMNumericIteratorHeapArrays(t *testing.T) {
	for _, kind := range []string{"int", "uint"} {
		t.Run(kind, func(t *testing.T) {
			source := numericIteratorHeapArraySource(kind)
			t.Logf("heap array source_SHA256=%x", sha256.Sum256([]byte(source)))
			result := runProgramFromSource(t, source, runOptions{captureStdout: true})
			if result.exitCode != 0 || result.stdout != numericIteratorHeapMarker(kind, "arrays")+"\n" || result.stderr != "" {
				t.Fatalf("heap %s array iterator: exit=%d stdout=%q stderr=%q artifacts=%s", kind, result.exitCode, result.stdout, result.stderr, result.artifactsDir)
			}
		})
	}
}

func TestVMNumericIteratorHeapRanges(t *testing.T) {
	for _, kind := range []string{"int", "uint"} {
		t.Run(kind, func(t *testing.T) {
			source := numericIteratorHeapRangeSource(kind)
			t.Logf("heap range source_SHA256=%x", sha256.Sum256([]byte(source)))
			result := runProgramFromSource(t, source, runOptions{captureStdout: true})
			if result.exitCode != 0 || result.stdout != numericIteratorHeapMarker(kind, "ranges")+"\n" || result.stderr != "" {
				t.Fatalf("heap %s range iterator: exit=%d stdout=%q stderr=%q artifacts=%s", kind, result.exitCode, result.stdout, result.stderr, result.artifactsDir)
			}
		})
	}
}

func TestRuntimeV2NumericIteratorHeapArraysValgrindZero(t *testing.T) {
	for _, kind := range []string{"int", "uint"} {
		t.Run(kind, func(t *testing.T) {
			source := numericIteratorHeapArraySource(kind)
			t.Logf("heap array source_SHA256=%x", sha256.Sum256([]byte(source)))
			runNumericIteratorValgrind(t, source, numericIteratorHeapMarker(kind, "arrays"))
		})
	}
}

func TestRuntimeV2NumericIteratorHeapRangesValgrindZero(t *testing.T) {
	for _, kind := range []string{"int", "uint"} {
		t.Run(kind, func(t *testing.T) {
			source := numericIteratorHeapRangeSource(kind)
			t.Logf("heap range source_SHA256=%x", sha256.Sum256([]byte(source)))
			runNumericIteratorValgrind(t, source, numericIteratorHeapMarker(kind, "ranges"))
		})
	}
}

func numericIteratorHeapAsyncRows() []struct {
	name, kind, form, marker string
	cancel                   bool
} {
	return []struct {
		name, kind, form, marker string
		cancel                   bool
	}{
		{"int_array_resume", "int", "array", "numeric-iterator-heap-int-array-resumed-witness", false},
		{"int_array_cancel", "int", "array", "numeric-iterator-heap-int-array-cancelled-witness", true},
		{"uint_array_resume", "uint", "array", "numeric-iterator-heap-uint-array-resumed-witness", false},
		{"uint_array_cancel", "uint", "array", "numeric-iterator-heap-uint-array-cancelled-witness", true},
		{"uint_fast_resume", "uint", "fast", "numeric-iterator-heap-uint-fast-resumed-witness", false},
		{"uint_fast_cancel", "uint", "fast", "numeric-iterator-heap-uint-fast-cancelled-witness", true},
	}
}

func TestVMNumericIteratorHeapSuspendLifecycle(t *testing.T) {
	for _, row := range numericIteratorHeapAsyncRows() {
		t.Run(row.name, func(t *testing.T) {
			source := numericIteratorAsyncTypedProgram(row.kind, row.form, row.cancel, false)
			t.Logf("heap suspend source_SHA256=%x", sha256.Sum256([]byte(source)))
			result := runProgramFromSource(t, source, runOptions{captureStdout: true})
			if result.exitCode != 0 || result.stdout != row.marker+"\n" || result.stderr != "" {
				t.Fatalf("heap iterator %s: exit=%d stdout=%q stderr=%q artifacts=%s", row.name, result.exitCode, result.stdout, result.stderr, result.artifactsDir)
			}
		})
	}
}

// Existing float workers=8 rows cover scheduler width. These rows isolate the
// counted leaf and array-cursor/fast-bound owners on one worker, comparing
// complete allocation origins at rounds 1 and 16 with the shared control.
func TestRuntimeV2NumericIteratorHeapSuspendValgrindBaseline(t *testing.T) {
	for _, row := range numericIteratorHeapAsyncRows() {
		t.Run(row.name, func(t *testing.T) {
			source := numericIteratorAsyncTypedProgram(row.kind, row.form, row.cancel, true)
			t.Logf("heap suspend baseline source_SHA256=%x", sha256.Sum256([]byte(source)))
			output := buildAsyncAllocationProgram(t, source)
			runAsyncAllocationBaseline(t, output, row.marker, asyncAllocationEnvironment(t, "1"))
		})
	}
}
