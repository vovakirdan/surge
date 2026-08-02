//go:build !golden

package vm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These are distinct named types, rather than aliases or one shared marker
// type, so a caller cannot omit or accidentally exchange a type-matrix axis.
type moveOnlyHeapMarker string
type copyValueCompositeMarker string
type referenceCountedScalarMarker string
type nonOwningMarker string

type ownershipAxisMarker struct {
	axis  string
	value string
}

// ownershipGate runs one ownership probe through both executable backends.
// Its four marker parameters are intentionally positional and non-optional:
// adding a new call without all four ownership axes must be a compile error.
func ownershipGate(
	t *testing.T,
	source string,
	moveOnlyHeap moveOnlyHeapMarker,
	copyValueComposite copyValueCompositeMarker,
	referenceCountedScalar referenceCountedScalarMarker,
	nonOwning nonOwningMarker,
) {
	t.Helper()
	markers, err := validateOwnershipGateInput(
		source,
		moveOnlyHeap,
		copyValueComposite,
		referenceCountedScalar,
		nonOwning,
	)
	if err != nil {
		t.Fatalf("invalid ownership gate: %v", err)
	}

	t.Run("vm", func(t *testing.T) {
		t.Setenv(backendEnvVar, backendVM)
		result := runProgramFromSource(t, source, runOptions{})
		if result.exitCode != 0 {
			t.Fatalf("ownership probe failed on VM (exit=%d)\nstderr:\n%s", result.exitCode, result.stderr)
		}
		if strings.TrimSpace(result.stderr) != "" {
			t.Fatalf("ownership probe reported a VM runtime error:\n%s", result.stderr)
		}
	})

	t.Run("llvm_valgrind", func(t *testing.T) {
		// The build helper owns the timeout-policy skip. Once it returns, the
		// memory gate is mandatory and missing valgrind must fail closed.
		outputPath := buildRuntimeV2CrossingSource(t, source, nil)
		requireOwnershipValgrind(t, exec.LookPath)

		env := envWithStdlib(repoRoot(t))
		stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
		if exitCode != 0 {
			t.Fatalf(
				"ownership probe failed on LLVM under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s",
				exitCode,
				stdout,
				stderr,
			)
		}
		if missing := missingOwnershipMarkerLines(stdout, markers); len(missing) != 0 {
			t.Fatalf("ownership probe missed exact marker line(s) %v; stdout=%q", missing, stdout)
		}

		lostBytes, lostBlocks, parseErr := parseValgrindDefinitelyLost(stderr)
		memcheckError := hasValgrindMemcheckError(stderr)
		if parseErr != nil {
			t.Fatalf(
				"ownership probe could not read valgrind leak summary (memcheck_error=%t): %v\nstderr:\n%s",
				memcheckError,
				parseErr,
				stderr,
			)
		}
		if memcheckError || lostBytes != 0 || lostBlocks != 0 {
			t.Fatalf(
				"ownership probe failed memory gate: memcheck_error=%t definitely_lost=%d bytes/%d blocks; want false and strict 0/0\nstdout:\n%s\nstderr:\n%s",
				memcheckError,
				lostBytes,
				lostBlocks,
				stdout,
				stderr,
			)
		}
	})
}

func requireOwnershipValgrind(
	t *testing.T,
	lookPath func(string) (string, error),
) {
	t.Helper()
	if _, err := lookPath("valgrind"); err != nil {
		t.Fatalf("ownership gate requires valgrind on PATH after LLVM build: %v", err)
	}
}

func validateOwnershipGateInput(
	source string,
	moveOnlyHeap moveOnlyHeapMarker,
	copyValueComposite copyValueCompositeMarker,
	referenceCountedScalar referenceCountedScalarMarker,
	nonOwning nonOwningMarker,
) ([]ownershipAxisMarker, error) {
	if strings.TrimSpace(source) == "" {
		return nil, fmt.Errorf("source must be nonempty")
	}
	markers := []ownershipAxisMarker{
		{axis: "move-only heap", value: string(moveOnlyHeap)},
		{axis: "@copy value-composite", value: string(copyValueComposite)},
		{axis: "reference-counted scalar", value: string(referenceCountedScalar)},
		{axis: "non-owning", value: string(nonOwning)},
	}
	seen := make(map[string]string, len(markers))
	for i := range markers {
		marker := &markers[i]
		if strings.TrimSpace(marker.value) == "" {
			return nil, fmt.Errorf("%s marker must be nonempty", marker.axis)
		}
		if marker.value != strings.TrimSpace(marker.value) || strings.ContainsAny(marker.value, "\r\n") {
			return nil, fmt.Errorf("%s marker must be one exact trimmed output line", marker.axis)
		}
		if otherAxis, ok := seen[marker.value]; ok {
			return nil, fmt.Errorf("%s marker duplicates %s marker %q", marker.axis, otherAxis, marker.value)
		}
		seen[marker.value] = marker.axis
		if !strings.Contains(source, marker.value) {
			return nil, fmt.Errorf("source does not contain %s marker %q", marker.axis, marker.value)
		}
	}
	return markers, nil
}

func missingOwnershipMarkerLines(stdout string, markers []ownershipAxisMarker) []string {
	lines := make(map[string]struct{})
	for _, line := range strings.Split(stdout, "\n") {
		lines[strings.TrimSuffix(line, "\r")] = struct{}{}
	}
	var missing []string
	for _, marker := range markers {
		if _, ok := lines[marker.value]; !ok {
			missing = append(missing, marker.axis+"="+marker.value)
		}
	}
	return missing
}

const runtimeV2OwnershipGateSource = `
@copy type CopyCell = { left: int, right: int };

fn move_only_heap_probe() -> int {
    let mut text = "m";
    text = text + "ove";
    return len(text) to int;
}

fn copy_value_composite_probe() -> int {
    let original = CopyCell { left = 1, right = 2 };
    let mut copied = original;
    copied.left = 7;
    return (original.left * 10) + copied.left;
}

fn reference_counted_scalar_probe() -> float {
    let original: float = 1.5;
    let copied = original;
    return original + copied;
}

fn non_owning_probe() -> int {
    let original: int = 7;
    let copied = original;
    return original + copied;
}

@entrypoint
fn main() -> int {
    if move_only_heap_probe() != 4 {
        return 1;
    }
    print("ownership-axis-move-only");
    if copy_value_composite_probe() != 17 {
        return 2;
    }
    print("ownership-axis-copy-composite");
    if reference_counted_scalar_probe() != 3.0 {
        return 3;
    }
    print("ownership-axis-refcounted-scalar");
    if non_owning_probe() != 14 {
        return 4;
    }
    print("ownership-axis-non-owning");
    return 0;
}
`

func TestRuntimeV2OwnershipGateFourAxisSelfProof(t *testing.T) {
	ownershipGate(
		t,
		runtimeV2OwnershipGateSource,
		moveOnlyHeapMarker("ownership-axis-move-only"),
		copyValueCompositeMarker("ownership-axis-copy-composite"),
		referenceCountedScalarMarker("ownership-axis-refcounted-scalar"),
		nonOwningMarker("ownership-axis-non-owning"),
	)
}

func TestOwnershipGateValidatesMarkerContract(t *testing.T) {
	validSource := "move-only\ncopy-composite\nrefcounted\nnon-owning\n"
	tests := []struct {
		name    string
		source  string
		move    moveOnlyHeapMarker
		copy    copyValueCompositeMarker
		scalar  referenceCountedScalarMarker
		plain   nonOwningMarker
		wantErr string
	}{
		{
			name: "empty_source", source: " \n", move: "move-only", copy: "copy-composite",
			scalar: "refcounted", plain: "non-owning", wantErr: "source must be nonempty",
		},
		{
			name: "empty_axis", source: validSource, move: "", copy: "copy-composite",
			scalar: "refcounted", plain: "non-owning", wantErr: "move-only heap marker must be nonempty",
		},
		{
			name: "duplicate_axes", source: validSource, move: "move-only", copy: "copy-composite",
			scalar: "refcounted", plain: "refcounted", wantErr: "non-owning marker duplicates reference-counted scalar",
		},
		{
			name: "marker_not_in_source", source: validSource, move: "move-only", copy: "copy-composite",
			scalar: "refcounted", plain: "missing", wantErr: "source does not contain non-owning marker",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateOwnershipGateInput(test.source, test.move, test.copy, test.scalar, test.plain)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("validation error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestOwnershipGateMissingValgrindFailsClosed(t *testing.T) {
	const childEnv = "SURGE_OWNERSHIP_GATE_MISSING_VALGRIND_CHILD"
	if os.Getenv(childEnv) == "1" {
		requireOwnershipValgrind(t, func(string) (string, error) {
			return "", os.ErrNotExist
		})
		t.Fatal("missing-valgrind requirement unexpectedly returned")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(
		ctx,
		os.Args[0], "-test.run=^TestOwnershipGateMissingValgrindFailsClosed$", "-test.count=1",
	)
	cmd.Env = overrideEnvVar(os.Environ(), childEnv, "1")
	output, runErr := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("missing-valgrind child timed out\noutput:\n%s", output)
	}
	if runErr == nil {
		t.Fatalf("missing-valgrind child unexpectedly passed\noutput:\n%s", output)
	}

	text := string(output)
	const want = "ownership gate requires valgrind on PATH after LLVM build: file does not exist"
	if strings.Count(text, want) != 1 {
		t.Fatalf("missing-valgrind child did not report the exact fail-closed error\noutput:\n%s", text)
	}
	if strings.Contains(text, "--- SKIP") {
		t.Fatalf("missing-valgrind child skipped instead of failing\noutput:\n%s", text)
	}
}

func TestOwnershipGateMissingFourthAxisDoesNotCompile(t *testing.T) {
	root := repoRoot(t)
	target := filepath.Join(root, "internal", "vm", "runtime_v2_ownership_gate_test.go")
	original, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("read ownership gate source: %v", readErr)
	}
	const fourthAxisArgument = "\t\tnonOwningMarker(\"ownership-axis-non-owning\"),\n"
	if count := strings.Count(string(original), fourthAxisArgument); count != 1 {
		t.Fatalf("missing-axis overlay anchor count = %d, want exactly 1", count)
	}
	missingFourthAxis := strings.Replace(string(original), fourthAxisArgument, "", 1)

	tempDir := t.TempDir()
	replacement := filepath.Join(tempDir, "runtime_v2_ownership_gate_missing_axis_test.go")
	if writeErr := os.WriteFile(replacement, []byte(missingFourthAxis), 0o600); writeErr != nil {
		t.Fatalf("write missing-axis replacement: %v", writeErr)
	}
	overlayBytes, marshalErr := json.Marshal(struct {
		Replace map[string]string `json:"Replace"`
	}{Replace: map[string]string{target: replacement}})
	if marshalErr != nil {
		t.Fatalf("encode go overlay: %v", marshalErr)
	}
	overlayPath := filepath.Join(tempDir, "overlay.json")
	if writeErr := os.WriteFile(overlayPath, overlayBytes, 0o600); writeErr != nil {
		t.Fatalf("write go overlay: %v", writeErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(
		ctx,
		"go", "test", "-overlay="+overlayPath, "-run", "^$", "-count=1", "-p=1", "./internal/vm",
	)
	cmd.Dir = root
	cmd.Env = overrideEnvVar(os.Environ(), "GOFLAGS", "")
	output, runErr := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("missing-axis overlay compile timed out\noutput:\n%s", output)
	}
	if runErr == nil {
		t.Fatalf("missing-axis overlay unexpectedly compiled\noutput:\n%s", output)
	}

	text := string(output)
	const wantError = "not enough arguments in call to ownershipGate"
	const wantHave = "have (*testing.T, string, moveOnlyHeapMarker, copyValueCompositeMarker, referenceCountedScalarMarker)"
	const wantSignature = "want (*testing.T, string, moveOnlyHeapMarker, copyValueCompositeMarker, referenceCountedScalarMarker, nonOwningMarker)"
	if strings.Count(text, wantError) != 1 || !strings.Contains(text, wantHave) || !strings.Contains(text, wantSignature) {
		t.Fatalf("overlay failed for a reason other than the exact missing fourth axis\noutput:\n%s", text)
	}
	var diagnostics []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, ".go:") && strings.Contains(line, ": ") {
			diagnostics = append(diagnostics, line)
		}
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0], wantError) {
		t.Fatalf("overlay produced diagnostics beyond the exact missing-axis error: %v\noutput:\n%s", diagnostics, text)
	}
}
