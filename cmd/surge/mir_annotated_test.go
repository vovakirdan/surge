package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const diagnoseMIRAnnotationSource = `
fn inspect() -> int {
    let message: string = "hello";
    return len(&message) to int;
}
`

const buildMIRAnnotationSource = `
@entrypoint
fn main() -> int {
    let message: string = "hello";
    return len(&message) to int;
}
`

func TestMIRAnnotatedFlagsAreRegistered(t *testing.T) {
	commands := []struct {
		name    string
		command *cobra.Command
	}{
		{name: "build", command: newBuildCommand()},
		{name: "diag", command: newDiagnoseCommand()},
	}
	for _, item := range commands {
		if item.command.Flags().Lookup("emit-mir-annotated") == nil {
			t.Errorf("%s command does not register --emit-mir-annotated", item.name)
		}
	}
}

func TestDiagnoseMIRAnnotatedImpliesMIREmission(t *testing.T) {
	t.Setenv("SURGE_STDLIB", surgeRepoRootForBuildTest(t))
	path := filepath.Join(t.TempDir(), "mir_annotation.sg")
	if err := os.WriteFile(path, []byte(diagnoseMIRAnnotationSource), 0o600); err != nil {
		t.Fatalf("write diagnose MIR source: %v", err)
	}

	root := newDiagnoseRootForTest()
	root.SetArgs([]string{"diag", "--format=short", "--stages=sema", "--emit-mir-annotated", path})
	output, err := captureStdoutForTest(t, root.Execute)
	if err != nil {
		t.Fatalf("diag --emit-mir-annotated: %v", err)
	}
	if !strings.Contains(output, "== MIR ==") || !strings.Contains(output, "owes_release") ||
		!strings.Contains(output, "[effect=mint]") {
		t.Fatalf("annotated-only diagnose did not emit annotated MIR:\n%s", output)
	}
}

func TestBuildMIRAnnotatedFlagPropagatesToDump(t *testing.T) {
	repoRoot := surgeRepoRootForBuildTest(t)
	t.Setenv("SURGE_STDLIB", repoRoot)
	workspace := t.TempDir()
	chdirForTest(t, workspace)
	path := filepath.Join(workspace, "mir_annotation.sg")
	if err := os.WriteFile(path, []byte(buildMIRAnnotationSource), 0o600); err != nil {
		t.Fatalf("write build MIR source: %v", err)
	}

	root := &cobra.Command{
		Use:           "surge",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().Int("max-diagnostics", 100, "")
	root.AddCommand(newBuildCommand())
	root.SetArgs([]string{
		"build", "--backend=vm", "--ui=off", "--emit-mir-annotated", path,
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("build --emit-mir-annotated: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(workspace, "target", "debug", ".tmp", "*", "out.mir"))
	if err != nil {
		t.Fatalf("glob annotated build MIR: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("annotated build MIR dumps = %v, want exactly one", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read annotated build MIR: %v", err)
	}
	output := string(data)
	if !strings.Contains(output, "owes_release") || !strings.Contains(output, "[effect=mint]") {
		t.Fatalf("build flag did not propagate ownership annotations:\n%s", output)
	}
}

func TestDiagnoseMIRAnnotatedUsesExistingMIRGuards(t *testing.T) {
	t.Run("stage", func(t *testing.T) {
		root := newDiagnoseRootForTest()
		root.SetArgs([]string{"diag", "--stages=syntax", "--emit-mir-annotated", "unused.sg"})
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), "--emit-mir requires --stages sema|all") {
			t.Fatalf("annotated MIR stage error = %v", err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		root := newDiagnoseRootForTest()
		root.SetArgs([]string{"diag", "--emit-mir-annotated", t.TempDir()})
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), "--emit-mir is only supported for single files") {
			t.Fatalf("annotated MIR directory error = %v", err)
		}
	})
}

func newDiagnoseRootForTest() *cobra.Command {
	root := &cobra.Command{
		Use:           "surge",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().Int("max-diagnostics", 100, "")
	root.PersistentFlags().Bool("timings", false, "")
	root.PersistentFlags().String("color", "off", "")
	root.PersistentFlags().String("cpu-profile", "", "")
	root.PersistentFlags().String("mem-profile", "", "")
	root.PersistentFlags().String("runtime-trace", "", "")
	root.AddCommand(newDiagnoseCommand())
	return root
}

func captureStdoutForTest(t *testing.T, run func() error) (string, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout capture pipe: %v", err)
	}
	original := os.Stdout
	os.Stdout = writer
	defer func() {
		os.Stdout = original
		_ = writer.Close()
		_ = reader.Close()
	}()

	type readResult struct {
		data []byte
		err  error
	}
	readDone := make(chan readResult, 1)
	go func() {
		data, readErr := io.ReadAll(reader)
		readDone <- readResult{data: data, err: readErr}
	}()

	runErr := run()
	os.Stdout = original
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("close stdout capture writer: %v", closeErr)
	}
	read := <-readDone
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatalf("close stdout capture reader: %v", closeErr)
	}
	if read.err != nil {
		t.Fatalf("read captured stdout: %v", read.err)
	}
	return string(read.data), runErr
}
