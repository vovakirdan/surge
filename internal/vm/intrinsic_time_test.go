package vm_test

import "testing"

func TestVMDurationIntrinsicMethods(t *testing.T) {
	requireVMBackend(t)
	t.Setenv("SURGE_STDLIB", repoRoot(t))

	sourceCode := `import stdlib/time as time;

@entrypoint
fn main() -> int {
    let d = time.Duration.new(1_500_250_999:int64);
    if d.as_nanos() != 1_500_250_999:int64 {
        return 1;
    }
    if d.as_micros() != 1_500_250:int64 {
        return 2;
    }
    if d.as_millis() != 1_500:int64 {
        return 3;
    }
    if d.as_seconds() != 1:int64 {
        return 4;
    }
    let later = time.Duration.new(2_000_000_000:int64);
    let diff = later.sub(d);
    if diff.as_micros() != 499_749:int64 {
        return 5;
    }
    let started = time.Duration.now();
    let finished = time.Duration.now();
    if finished.sub(started).as_nanos() < 0:int64 {
        return 6;
    }
    return 0;
}
`

	result := runProgramFromSource(t, sourceCode, runOptions{})
	if result.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr:\n%s", result.exitCode, result.stderr)
	}
	if result.stderr != "" {
		t.Fatalf("expected empty stderr, got:\n%s", result.stderr)
	}
}
