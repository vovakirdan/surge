//go:build !golden

package vm_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

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
	t.Helper()
	ensureLLVMToolchain(t)

	root := repoRoot(t)
	t.Setenv("SURGE_STDLIB", root)
	artifacts := newTestArtifacts(t, root)
	sourcePath := artifactSourcePath(artifacts)
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write crossing source: %v", err)
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
