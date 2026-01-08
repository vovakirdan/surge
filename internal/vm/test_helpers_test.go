//go:build !golden
// +build !golden

package vm_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"surge/internal/vm"
)

var (
	surgeBinOnce sync.Once
	surgeBinPath string
	errSurgeBin  error
)

const (
	backendEnvVar = "SURGE_BACKEND"
	backendVM     = "vm"
	backendLLVM   = "llvm"
)

type runOptions struct {
	argv  []string
	stdin string
}

type runResult struct {
	stdout       string
	stderr       string
	exitCode     int
	artifactsDir string
}

type testArtifacts struct {
	Dir   string
	Repro string
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

func envWithStdlib(root string) []string {
	env := os.Environ()
	key := "SURGE_STDLIB="
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, key) {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, key+root)
	return out
}

func buildSurgeBinary(t *testing.T, root string) string {
	t.Helper()

	surgeBinOnce.Do(func() {
		tmp, err := os.MkdirTemp("", "surge-bin-*")
		if err != nil {
			errSurgeBin = err
			return
		}
		surgeBinPath = filepath.Join(tmp, "surge")

		// #nosec G204 -- test build command uses fixed arguments
		cmd := exec.Command("go", "build", "-o", surgeBinPath, "./cmd/surge")
		cmd.Dir = root
		cmd.Env = envWithStdlib(root)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			errSurgeBin = errors.New(strings.TrimSpace(stderr.String()))
			if errSurgeBin.Error() == "" {
				errSurgeBin = err
			}
		}
	})

	if errSurgeBin != nil {
		t.Fatalf("build surge binary: %v", errSurgeBin)
	}
	return surgeBinPath
}

func testBackend(t *testing.T) string {
	t.Helper()
	backend := strings.TrimSpace(os.Getenv(backendEnvVar))
	if backend == "" {
		return backendVM
	}
	switch backend {
	case backendVM, backendLLVM:
		return backend
	default:
		t.Fatalf("unsupported %s=%q (expected %q or %q)", backendEnvVar, backend, backendVM, backendLLVM)
	}
	return backendVM
}

func requireVMBackend(t *testing.T) {
	t.Helper()
	if backend := testBackend(t); backend != backendVM {
		t.Skipf("skipping VM-only test for %s=%s", backendEnvVar, backend)
	}
}

func ensureLLVMToolchain(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed; skipping LLVM backend tests")
	}
	if _, err := exec.LookPath("ar"); err != nil {
		t.Skip("ar not installed; skipping LLVM backend tests")
	}
}

func runSurge(t *testing.T, root, surgeBin string, args ...string) (stdout, stderr string, exitCode int) {
	return runSurgeWithInput(t, root, surgeBin, "", args...)
}

func runSurgeWithInput(t *testing.T, root, surgeBin, stdin string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(surgeBin, args...)
	cmd.Dir = root
	cmd.Env = envWithStdlib(root)
	stdout, stderr, exitCode = runCommand(t, cmd, stdin)
	stdout = stripTimingLines(stdout)
	return stdout, stderr, exitCode
}

func runCommand(t *testing.T, cmd *exec.Cmd, stdin string) (stdout, stderr string, exitCode int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()

	stdout = outBuf.String()
	stderr = errBuf.String()

	if err == nil {
		return stdout, stderr, 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run command: %v\nstderr:\n%s", err, stderr)
	}
	return stdout, stderr, exitErr.ExitCode()
}

func newTestArtifacts(t *testing.T, root string) *testArtifacts {
	t.Helper()
	name := sanitizeTestName(t.Name())
	dir := filepath.Join(root, "target", "debug", ".tests", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create artifacts dir: %v", err)
	}
	artifacts := &testArtifacts{Dir: dir}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("artifacts: %s", artifacts.Dir)
			if artifacts.Repro != "" {
				t.Logf("repro: %s", artifacts.Repro)
			}
			return
		}
		_ = os.RemoveAll(artifacts.Dir)
	})
	return artifacts
}

func sanitizeTestName(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, " ", "_")
	return name
}

func writeArtifact(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write artifact %s: %v", name, err)
	}
}

func runProgramFromSource(t *testing.T, source string, opts runOptions) runResult {
	t.Helper()
	root := repoRoot(t)
	artifacts := newTestArtifacts(t, root)
	srcPath := filepath.Join(artifacts.Dir, sanitizeTestName(t.Name())+".sg")
	if err := os.WriteFile(srcPath, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return runProgram(t, root, srcPath, opts, artifacts)
}

func runProgram(t *testing.T, root, srcPath string, opts runOptions, artifacts *testArtifacts) runResult {
	t.Helper()
	backend := testBackend(t)
	if backend == backendVM {
		mirMod, files, typesInterner := compileToMIR(t, srcPath)
		rt := vm.NewTestRuntime(opts.argv, opts.stdin)
		exitCode, vmErr := runVM(mirMod, rt, files, typesInterner, nil)
		res := runResult{exitCode: exitCode}
		if artifacts != nil {
			res.artifactsDir = artifacts.Dir
		}
		if vmErr != nil {
			res.stderr = vmErr.FormatWithFiles(files)
			if artifacts != nil {
				writeArtifact(t, artifacts.Dir, "vm.stderr", res.stderr)
			}
		}
		return res
	}

	ensureLLVMToolchain(t)
	surge := buildSurgeBinary(t, root)

	buildArgs := []string{"build", srcPath, "--emit-mir", "--emit-llvm", "--keep-tmp", "--print-commands"}
	buildOut, buildErr, buildCode := runSurgeWithInput(t, root, surge, "", buildArgs...)
	if artifacts != nil {
		writeArtifact(t, artifacts.Dir, "build.stdout", buildOut)
		writeArtifact(t, artifacts.Dir, "build.stderr", buildErr)
		writeArtifact(t, artifacts.Dir, "build.exit_code", fmt.Sprintf("%d\n", buildCode))
	}

	outputPath := llvmOutputPath(root, srcPath)
	repro := llvmReproCommand(root, srcPath, outputPath, opts.argv)
	if artifacts != nil {
		artifacts.Repro = repro
		writeArtifact(t, artifacts.Dir, "repro.txt", repro+"\n")
		writeArtifact(t, artifacts.Dir, "build.tmp_dir", filepath.Join(root, "target", "debug", ".tmp", filepath.Base(outputPath))+"\n")
	}
	if buildCode != 0 {
		t.Fatalf("LLVM build failed (exit=%d). See %s", buildCode, artifacts.Dir)
	}

	// #nosec G204 -- test executes build output with controlled args
	cmd := exec.Command(outputPath, opts.argv...)
	cmd.Dir = root
	stdout, stderr, exitCode := runCommand(t, cmd, opts.stdin)
	if artifacts != nil {
		writeArtifact(t, artifacts.Dir, "run.stdout", stdout)
		writeArtifact(t, artifacts.Dir, "run.stderr", stderr)
		writeArtifact(t, artifacts.Dir, "run.exit_code", fmt.Sprintf("%d\n", exitCode))
	}
	return runResult{
		stdout:       stdout,
		stderr:       stderr,
		exitCode:     exitCode,
		artifactsDir: artifacts.Dir,
	}
}

func llvmOutputPath(root, srcPath string) string {
	base := filepath.Base(srcPath)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	return filepath.Join(root, "target", "debug", name)
}

func llvmReproCommand(root, srcPath, outputPath string, argv []string) string {
	relPath, err := filepath.Rel(root, srcPath)
	if err != nil {
		relPath = srcPath
	}
	var args string
	if len(argv) > 0 {
		args = " " + strings.Join(argv, " ")
	}
	return fmt.Sprintf("cd %s && SURGE_STDLIB=%s go run ./cmd/surge build %s --emit-mir --emit-llvm --keep-tmp --print-commands && %s%s", root, root, relPath, outputPath, args)
}
