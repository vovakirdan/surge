//go:build !golden

package vm_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"surge/internal/vm"
)

const runtimeV2CompareBorrowedTupleSource = `
fn build(prefix: string) -> string {
    let mut out = prefix;
    let mut i = 0;
    while i < 4 {
        out = out + "x";
        i = i + 1;
    }
    return out;
}

fn peek(x: &string) -> int { return len(x) to int; }

@entrypoint
fn main() -> int {
    let pair: (string, string) = (build("left"), build("right"));
    let seen: int = compare *(&pair) {
        (left, right) => peek(&left) + peek(&right);
    };
    if seen != 17 {
        return 1;
    }
    if peek(&pair.0) + peek(&pair.1) != 17 {
        return 2;
    }
    print("compare-borrowed-tuple-elements-stay-borrowed");
    return 0;
}
`

func TestRuntimeV2CompareBorrowedTupleElementsStayBorrowedVM(t *testing.T) {
	mirMod, files, typesInterner := compileToMIRFromSource(t, runtimeV2CompareBorrowedTupleSource)
	var forbiddenDrops []string
	for _, fnID := range mirMod.SortedFuncIDs() {
		fn := mirMod.Funcs[fnID]
		if fn == nil || fn.Name != "main" {
			continue
		}
		for localID, local := range fn.Locals {
			switch local.Name {
			case "left", "right":
				forbiddenDrops = append(forbiddenDrops, fmt.Sprintf(" drop L%d @", localID))
			}
		}
	}
	if len(forbiddenDrops) != 2 {
		t.Fatalf("expected two tuple binding locals, got trace patterns %v", forbiddenDrops)
	}

	var trace bytes.Buffer
	tracer := vm.NewTracer(&trace, files)
	rt := vm.NewTestRuntime(nil, "")
	exitCode, vmErr := runVM(mirMod, rt, files, typesInterner, tracer)
	if vmErr != nil {
		t.Fatalf("borrowed tuple compare failed on VM: %v", vmErr)
	}
	if exitCode != 0 {
		t.Fatalf("borrowed tuple compare exited %d, want 0", exitCode)
	}
	for _, drop := range forbiddenDrops {
		if strings.Contains(trace.String(), drop) {
			t.Fatalf("VM executed an owned drop for a borrowed tuple element: %q", drop)
		}
	}
}

func TestRuntimeV2CompareBorrowedTupleElementsStayBorrowedLLVMValgrind(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2CompareBorrowedTupleSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("borrowed tuple compare hit an invalid drop under valgrind\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("borrowed tuple compare failed natively (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "compare-borrowed-tuple-elements-stay-borrowed") {
		t.Fatalf("borrowed tuple compare missed completion marker; stdout=%q", stdout)
	}
	lostBytes, lostBlocks, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse borrowed tuple valgrind summary: %v\nstderr:\n%s", err, stderr)
	}
	if lostBytes != 0 || lostBlocks != 0 {
		t.Fatalf("borrowed tuple compare leaked %d bytes in %d blocks; want strict zero\nstderr:\n%s",
			lostBytes, lostBlocks, stderr)
	}
}
