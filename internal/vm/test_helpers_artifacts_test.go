package vm_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestVMTestArtifactsArePerRunUnique(t *testing.T) {
	root := repoRoot(t)

	first := newTestArtifacts(t, root)
	second := newTestArtifacts(t, root)

	if first.Dir == second.Dir {
		t.Fatalf("artifact dirs must be per-run unique, both got %s", first.Dir)
	}

	firstSource := filepath.Join(first.Dir, filepath.Base(first.Dir)+".sg")
	secondSource := filepath.Join(second.Dir, filepath.Base(second.Dir)+".sg")
	firstOutput := llvmOutputPath(root, firstSource)
	secondOutput := llvmOutputPath(root, secondSource)
	if firstOutput == secondOutput {
		t.Fatalf("LLVM output paths must be per-run unique, both got %s", firstOutput)
	}

	firstTmp := filepath.Join(root, "target", "debug", ".tmp", filepath.Base(firstOutput))
	secondTmp := filepath.Join(root, "target", "debug", ".tmp", filepath.Base(secondOutput))
	if firstTmp == secondTmp {
		t.Fatalf("LLVM tmp dirs must be per-run unique, both got %s", firstTmp)
	}
}

func TestVMTestArtifactsOverlapStress(t *testing.T) {
	requireLLVMBackend(t)
	ensureLLVMToolchain(t)

	root := repoRoot(t)
	const runs = 10
	const logicalName = "transport-overlap"
	source := `@entrypoint fn main() -> int { return 0; }
`

	type paths struct {
		dir    string
		output string
		tmp    string
	}
	var (
		mu      sync.Mutex
		results []paths
	)
	t.Cleanup(func() {
		if len(results) != runs {
			t.Fatalf("expected %d overlap runs, got %d", runs, len(results))
		}
		seenDirs := make(map[string]struct{}, runs)
		seenOutputs := make(map[string]struct{}, runs)
		seenTmps := make(map[string]struct{}, runs)
		for _, got := range results {
			if _, dup := seenDirs[got.dir]; dup {
				t.Fatalf("duplicate artifact dir under overlap: %s", got.dir)
			}
			if _, dup := seenOutputs[got.output]; dup {
				t.Fatalf("duplicate LLVM output under overlap: %s", got.output)
			}
			if _, dup := seenTmps[got.tmp]; dup {
				t.Fatalf("duplicate LLVM tmp dir under overlap: %s", got.tmp)
			}
			seenDirs[got.dir] = struct{}{}
			seenOutputs[got.output] = struct{}{}
			seenTmps[got.tmp] = struct{}{}
		}
	})

	for i := range runs {
		t.Run(fmt.Sprintf("run-%02d", i), func(t *testing.T) {
			t.Parallel()
			artifacts := newTestArtifactsWithName(t, root, logicalName)
			srcPath := artifactSourcePath(artifacts)
			if err := os.WriteFile(srcPath, []byte(source), 0o600); err != nil {
				t.Fatalf("write source: %v", err)
			}
			res := runProgram(t, root, srcPath, runOptions{}, artifacts)
			if res.exitCode != 0 {
				t.Fatalf("exit code: want 0, got %d\nstdout:\n%s\nstderr:\n%s\ndiagnostics:\n%s",
					res.exitCode, res.stdout, res.stderr, res.diagnostics)
			}
			mu.Lock()
			results = append(results, paths{
				dir:    artifacts.Dir,
				output: artifacts.OutputPath,
				tmp:    artifacts.TmpDir,
			})
			mu.Unlock()
		})
	}
}

func TestRunBinaryWithTimeoutReportsEmptyOutputDiagnostics(t *testing.T) {
	root := repoRoot(t)
	artifacts := newTestArtifacts(t, root)
	binPath := filepath.Join(t.TempDir(), "empty-exit")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 7\n"), 0o700); err != nil {
		t.Fatalf("write empty-output probe: %v", err)
	}
	trackLLVMBuildArtifacts(root, artifacts, binPath)

	_, res := runBinaryWithTimeout(t, binPath, envWithStdlib(root), time.Second)
	if res.exitCode != 7 {
		t.Fatalf("exit code: want 7, got %d", res.exitCode)
	}
	if res.stdout != "" {
		t.Fatalf("stdout changed: %q", res.stdout)
	}
	if res.stderr != "" {
		t.Fatalf("stderr changed: %q", res.stderr)
	}
	for _, want := range []string{
		"run diagnostics:",
		"command:",
		"binary: " + binPath,
		"exit_code: 7",
		"stdout_len: 0",
		"stderr_len: 0",
		"timeout: 1s",
	} {
		if !strings.Contains(res.diagnostics, want) {
			t.Fatalf("diagnostics missing %q:\n%s", want, res.diagnostics)
		}
	}
	for name, want := range map[string]string{
		"run.stdout":      "",
		"run.stderr":      "",
		"run.exit_code":   "7\n",
		"run.diagnostics": res.diagnostics,
	} {
		gotBytes, err := os.ReadFile(filepath.Join(artifacts.Dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if got := string(gotBytes); got != want {
			t.Fatalf("%s mismatch:\nwant %q\ngot  %q", name, want, got)
		}
	}
}
