package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A freshly prepared Copy composite uses a MIR move, but its fixed-width
// fields own no heap. Its implicit join must not add an invalid cleanup Drop.
const runtimeV2AsyncPlainCompositeReturnSource = `
@copy type Pair = { a: int64, b: int64 };

async fn make_pair(k: int64) -> Pair {
    return Pair { a = k, b = k + 1:int64 };
}

@entrypoint
fn main() -> int {
    let p: Pair = compare make_pair(7:int64).await() {
        Success(value) => value;
        Cancelled() => { return 1; };
    };
    if p.a != 7:int64 || p.b != 8:int64 { return 2; }
    print("async-plain-composite-return-ok");
    return 0;
}
`

func TestRuntimeV2AsyncPlainCompositeReturn(t *testing.T) {
	t.Setenv("SURGE_THREADS", "1")
	t.Setenv("SURGE_SHARDS", "1")
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2AsyncPlainCompositeReturnSource, nil)
	duration, result := runBinaryWithTimeout(t, outputPath, envWithStdlib(repoRoot(t)), 30*time.Second)
	if result.exitCode != 0 || result.stderr != "" {
		t.Fatalf("plain composite return failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
			result.exitCode, duration, result.stdout, result.stderr)
	}
	if strings.Count(result.stdout, "async-plain-composite-return-ok") != 1 {
		t.Fatalf("plain composite return missing completion marker; stdout=%q", result.stdout)
	}
	t.Logf("plain composite return output:\n%s", result.stdout)
}
