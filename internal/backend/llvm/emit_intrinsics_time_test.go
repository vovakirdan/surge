package llvm

import (
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

func TestEmitDurationIntrinsicsLowered(t *testing.T) {
	t.Setenv("SURGE_STDLIB", repoRootFromLLVMTest(t))

	sourceCode := `import stdlib/time as time;

fn duration(ns: int64) -> time.Duration {
    return time.Duration { __opaque = ns };
}

@entrypoint
fn main() -> int {
    let d = duration(1_500_250_999:int64);
    let later = duration(2_000_000_000:int64);
    let diff = later.sub(d);
    if diff.as_millis() != 499:int64 {
        return 1;
    }
    if d.as_seconds() != 1:int64 {
        return 2;
    }
    if d.as_micros() != 1_500_250:int64 {
        return 3;
    }
    if d.as_nanos() != 1_500_250_999:int64 {
        return 4;
    }
    return 0;
}
`

	ir := emitLLVMFromSource(t, sourceCode)

	for _, name := range []string{"sub", "as_seconds", "as_millis", "as_micros", "as_nanos"} {
		pattern := regexp.MustCompile(`call [^(]+ @` + name + `\(`)
		if pattern.MatchString(ir) {
			t.Fatalf("duration intrinsic %s leaked as external call:\n%s", name, ir)
		}
	}
	if !regexp.MustCompile(`sub i64`).MatchString(ir) {
		t.Fatalf("expected Duration.sub to emit integer subtraction:\n%s", ir)
	}
	if !regexp.MustCompile(`sdiv i64 [^,]+, 1000000`).MatchString(ir) {
		t.Fatalf("expected as_millis to divide nanoseconds by 1_000_000:\n%s", ir)
	}
	if !regexp.MustCompile(`sdiv i64 [^,]+, 1000000000`).MatchString(ir) {
		t.Fatalf("expected as_seconds to divide nanoseconds by 1_000_000_000:\n%s", ir)
	}
}

func repoRootFromLLVMTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}
