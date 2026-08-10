package vm_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"surge/internal/buildpipeline"
	"surge/internal/sema"
)

// buildRuntimeV2CrossingSource builds a source program through the same
// source -> MIR -> LLVM -> native pipeline as `surge build`. The forms map is
// deliberately wired only to CompileRequest.CrossingFormsForTest so the
// executable proof cannot accidentally change the public backend gate.
func buildRuntimeV2CrossingSource(
	t *testing.T,
	source string,
	forms map[sema.CrossingLoweringKind]bool,
) string {
	return buildRuntimeV2CrossingProject(t, source, nil, forms)
}

// buildRuntimeV2CrossingProject is the multi-file variant: modules maps a
// module name to its source; each is written as <dir>/<name>/<name>.sg next
// to the root source so `import <name>::...` resolves through the normal
// dependency scan.
func buildRuntimeV2CrossingProject(
	t *testing.T,
	source string,
	modules map[string]string,
	forms map[sema.CrossingLoweringKind]bool,
) string {
	t.Helper()
	ensureLLVMToolchain(t)

	root := repoRoot(t)
	t.Setenv("SURGE_STDLIB", root)
	artifacts := newTestArtifacts(t, root)
	sourcePath := artifactSourcePath(artifacts)
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write crossing source: %v", err)
	}
	for name, body := range modules {
		moduleDir := filepath.Join(artifacts.Dir, name)
		if err := os.MkdirAll(moduleDir, 0o700); err != nil {
			t.Fatalf("create module dir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(moduleDir, name+".sg"), []byte(body), 0o600); err != nil {
			t.Fatalf("write module %s: %v", name, err)
		}
	}

	result, err := buildpipeline.Build(t.Context(), &buildpipeline.BuildRequest{
		CompileRequest: buildpipeline.CompileRequest{
			TargetPath:           sourcePath,
			BaseDir:              root,
			MaxDiagnostics:       200,
			CrossingFormsForTest: forms,
		},
		OutputName: artifacts.SourceBase,
		OutputRoot: root,
		Profile:    "debug",
		Backend:    buildpipeline.BackendLLVM,
		EmitMIR:    true,
		EmitLLVM:   true,
		KeepTmp:    true,
	})

	if result.OutputPath != "" {
		trackLLVMBuildArtifacts(root, artifacts, result.OutputPath)
	}
	if result.TmpDir != "" {
		artifacts.TmpDir = result.TmpDir
	}
	artifacts.Repro = fmt.Sprintf(
		"cd %s && go test ./internal/vm -run '^%s$' -count=1",
		root,
		t.Name(),
	)
	writeArtifact(t, artifacts.Dir, "repro.txt", artifacts.Repro+"\n")
	writeArtifact(t, artifacts.Dir, "build.output_path", result.OutputPath+"\n")
	writeArtifact(t, artifacts.Dir, "build.tmp_dir", result.TmpDir+"\n")
	if err != nil {
		writeArtifact(t, artifacts.Dir, "build.error", err.Error()+"\n")
		t.Fatalf("LLVM crossing build failed: %v (artifacts: %s)", err, artifacts.Dir)
	}
	if result.OutputPath == "" {
		t.Fatalf("LLVM crossing build returned no executable (artifacts: %s)", artifacts.Dir)
	}
	if _, statErr := os.Stat(result.OutputPath); statErr != nil {
		t.Fatalf("stat LLVM crossing executable %s: %v", result.OutputPath, statErr)
	}
	return filepath.Clean(result.OutputPath)
}

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
	const targetRel = "internal/vm/runtime_v2_crossing_source_build_test.go"
	targetPath := filepath.Join(root, filepath.FromSlash(targetRel))
	original, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		t.Fatalf("read ownership gate source: %v", readErr)
	}
	const fourthAxisArgument = "\t\tnonOwningMarker(\"ownership-axis-non-owning\"),\n"
	if count := strings.Count(string(original), fourthAxisArgument); count != 1 {
		t.Fatalf("missing-axis overlay anchor count = %d, want exactly 1", count)
	}
	missingFourthAxis := strings.Replace(string(original), fourthAxisArgument, "", 1)

	artifacts, artifactDirRel := newOwnershipOverlayArtifacts(t, root)
	const replacementName = "runtime_v2_crossing_source_build_missing_axis_test.go"
	replacementPath := filepath.Join(artifacts.Dir, replacementName)
	if writeErr := os.WriteFile(replacementPath, []byte(missingFourthAxis), 0o600); writeErr != nil {
		t.Fatalf("write missing-axis replacement: %v", writeErr)
	}
	replacementRel := artifactDirRel + "/" + replacementName
	overlayBytes := []byte(`{"Replace":{"` + targetRel + `":"` + replacementRel + `"}}`)
	const overlayName = "overlay.json"
	overlayPath := filepath.Join(artifacts.Dir, overlayName)
	if writeErr := os.WriteFile(overlayPath, overlayBytes, 0o600); writeErr != nil {
		t.Fatalf("write go overlay: %v", writeErr)
	}
	overlayRel := artifactDirRel + "/" + overlayName

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(
		ctx,
		"go", "test", "-overlay="+overlayRel, "-run", "^$", "-count=1", "-p=1", "./internal/vm",
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

	assertOwnershipGateMissingFourthAxisDiagnostic(t, string(output))
}

func newOwnershipOverlayArtifacts(t *testing.T, root string) (*testArtifacts, string) {
	t.Helper()
	artifacts := newTestArtifacts(t, root)
	artifactDirRel, relErr := filepath.Rel(root, artifacts.Dir)
	if relErr != nil {
		t.Fatalf("make ownership overlay artifact path relative: %v", relErr)
	}
	artifactDirRel = filepath.ToSlash(artifactDirRel)
	const artifactDirPrefix = "target/debug/.tests/"
	const safeRelativePathCharacters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._/"
	if !strings.HasPrefix(artifactDirRel, artifactDirPrefix) ||
		strings.Trim(artifactDirRel, safeRelativePathCharacters) != "" {
		t.Fatalf("ownership overlay artifact path is not safe ASCII under %s: %q", artifactDirPrefix, artifactDirRel)
	}
	return artifacts, artifactDirRel
}

func assertOwnershipGateMissingFourthAxisDiagnostic(t *testing.T, output string) {
	t.Helper()
	const wantError = "not enough arguments in call to ownershipGate"
	const wantHave = "have (*testing.T, string, moveOnlyHeapMarker, copyValueCompositeMarker, referenceCountedScalarMarker)"
	const wantSignature = "want (*testing.T, string, moveOnlyHeapMarker, copyValueCompositeMarker, referenceCountedScalarMarker, nonOwningMarker)"
	if strings.Count(output, wantError) != 1 || !strings.Contains(output, wantHave) || !strings.Contains(output, wantSignature) {
		t.Fatalf("overlay failed for a reason other than the exact missing fourth axis\noutput:\n%s", output)
	}
	var diagnostics []string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, ".go:") && strings.Contains(line, ": ") {
			diagnostics = append(diagnostics, line)
		}
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0], wantError) {
		t.Fatalf("overlay produced diagnostics beyond the exact missing-axis error: %v\noutput:\n%s", diagnostics, output)
	}
}
