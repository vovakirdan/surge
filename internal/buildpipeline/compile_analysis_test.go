package buildpipeline

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
)

func compileWithCapturedStderr(t *testing.T, request *CompileRequest) (CompileResult, string, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("capture stderr pipe: %v", err)
	}
	original := os.Stderr
	os.Stderr = writer
	defer func() {
		os.Stderr = original
	}()

	result, compileErr := Compile(context.Background(), request)
	os.Stderr = original
	if err := writer.Close(); err != nil {
		t.Fatalf("close captured stderr writer: %v", err)
	}
	data, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("read captured stderr: %v", readErr)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close captured stderr reader: %v", err)
	}
	return result, string(data), compileErr
}

func writeAnalysisSource(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "analysis.sg")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write analysis source: %v", err)
	}
	return path
}

func TestCompileAnalysisSkipsOnlyEntrypointValidation(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	path := writeAnalysisSource(t, `fn helper() -> int { return 1; }`)

	analysis, err := Compile(context.Background(), &CompileRequest{
		TargetPath:     path,
		BaseDir:        testRepoRoot(t),
		MaxDiagnostics: 20,
		Analysis:       true,
	})
	if err != nil {
		t.Fatalf("analysis compile: %v", err)
	}
	if analysis.MIR == nil {
		t.Fatal("analysis compile returned no MIR")
	}

	executable, err := Compile(context.Background(), &CompileRequest{
		TargetPath:     path,
		BaseDir:        testRepoRoot(t),
		MaxDiagnostics: 20,
	})
	if err == nil || !strings.Contains(err.Error(), "no @entrypoint found") {
		t.Fatalf("default compile error = %v, want missing entrypoint", err)
	}
	if executable.MIR != nil {
		t.Fatal("default compile must not return MIR after entrypoint rejection")
	}
}

func TestCompileAnalysisKeepsDiagnosticsFatal(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	path := writeAnalysisSource(t, `fn broken() -> int { return missing; }`)

	request := &CompileRequest{
		TargetPath:     path,
		BaseDir:        testRepoRoot(t),
		MaxDiagnostics: 20,
		Analysis:       true,
	}
	result, analysisStderr, err := compileWithCapturedStderr(t, request)
	if err == nil || !strings.Contains(err.Error(), "diagnostics reported errors") {
		t.Fatalf("analysis diagnostic error = %v", err)
	}
	if result.MIR != nil {
		t.Fatal("analysis compile must not lower invalid source to MIR")
	}
	if result.Diagnose == nil || result.Diagnose.Bag == nil || !result.Diagnose.Bag.HasErrors() {
		t.Fatal("analysis compile must preserve fatal diagnostics in the result")
	}
	if analysisStderr != "" {
		t.Fatalf("analysis compile echoed diagnostics to stderr: %q", analysisStderr)
	}

	request.Analysis = false
	defaultResult, defaultStderr, defaultErr := compileWithCapturedStderr(t, request)
	if defaultErr == nil || !strings.Contains(defaultErr.Error(), "diagnostics reported errors") {
		t.Fatalf("default diagnostic error = %v", defaultErr)
	}
	if defaultResult.Diagnose == nil || defaultResult.Diagnose.Bag == nil || !defaultResult.Diagnose.Bag.HasErrors() {
		t.Fatal("default compile must preserve fatal diagnostics in the result")
	}
	if strings.TrimSpace(defaultStderr) == "" {
		t.Fatal("default compile did not echo its fatal diagnostic to stderr")
	}
	message := ""
	for _, diagnostic := range defaultResult.Diagnose.Bag.Items() {
		if diagnostic != nil && diagnostic.Severity == diag.SevError {
			message = diagnostic.Message
			break
		}
	}
	if message == "" {
		t.Fatal("default compile returned no fatal diagnostic message")
	}
	if !strings.Contains(defaultStderr, message) {
		t.Fatalf("default stderr %q does not contain diagnostic message %q", defaultStderr, message)
	}
}

func TestCompileAnalysisRejectsDiagnosticBypass(t *testing.T) {
	_, err := Compile(context.Background(), &CompileRequest{
		TargetPath:            "unused.sg",
		Analysis:              true,
		AllowDiagnosticsError: true,
	})
	if err == nil || !strings.Contains(err.Error(), "requires diagnostics to remain fatal") {
		t.Fatalf("analysis bypass error = %v", err)
	}
}
