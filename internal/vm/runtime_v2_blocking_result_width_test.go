package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A blocking body's result is stored at its own type, so a result WIDER than a
// machine word survives the trip.
//
// The representation this replaces could carry exactly one word out of
// `__surge_blocking_call`, which meant a composite was boxed by the worker
// thread and adopted again by the awaiting poll. Against that code this
// program prints garbage for the composite's first field and then SEGFAULTS:
// the two sides disagreed about whether the word was the value or a pointer to
// it.
//
// Both widths are driven, and both directions of each: the composite (two
// fields, sixteen bytes, one of them owning heap) and the bare string that
// already fit a word. A cell that can be written and not read passes every test
// that does not round-trip, so each value is read back and compared.
const runtimeV2BlockingResultWidthSource = `
type Note = { text: string, count: int };

fn build(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 3 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

async fn run() -> int {
    let wide: Note = compare (blocking {
        ret Note { text = build("wide-"), count = 7 };
    }).await() {
        Success(v) => v;
        Cancelled() => Note { text = "", count = 0 };
    };
    if wide.count != 7 {
        print("FAIL blocking composite count");
        return 1;
    }
    let text: string = own wide.text;
    if text != "wide-xxx" {
        print("FAIL blocking composite field ");
        print(text);
        return 2;
    }

    let narrow: string = compare (blocking { ret build("narrow-"); }).await() {
        Success(v) => v;
        Cancelled() => "";
    };
    if narrow != "narrow-xxx" {
        print("FAIL blocking string ");
        print(narrow);
        return 3;
    }

    print("blocking-result-width-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

func TestRuntimeV2BlockingResultKeepsItsWidth(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2BlockingResultWidthSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	duration, result := runBinaryWithTimeout(t, outputPath, baseEnv, 30*time.Second)
	if result.exitCode != 0 {
		t.Fatalf("blocking result width failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
			result.exitCode, duration, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "blocking-result-width-ok") {
		t.Fatalf("blocking result width missing completion marker; stdout=%q", result.stdout)
	}
}
