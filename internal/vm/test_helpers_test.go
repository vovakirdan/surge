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
	"syscall"
	"testing"
	"time"

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
	diagnostics  string
}

type testArtifacts struct {
	Dir        string
	SourceBase string
	OutputPath string
	TmpDir     string
	Repro      string
}

type runArtifactInfo struct {
	artifactsDir string
	tmpDir       string
}

type commandResult struct {
	stdout   string
	stderr   string
	exitCode int
	exitErr  *exec.ExitError
	duration time.Duration
}

var runArtifactRegistry sync.Map

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

func envForParity(root string) []string {
	const key = "SURGE_THREADS"
	prefix := key + "="
	env := envWithStdlib(root)
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, prefix+"1")
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
	skipTimeoutTests(t)
	if backend := testBackend(t); backend != backendVM {
		t.Skipf("skipping VM-only test for %s=%s", backendEnvVar, backend)
	}
}

func ensureLLVMToolchain(t *testing.T) {
	t.Helper()
	skipTimeoutTests(t)
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed; skipping LLVM backend tests")
	}
	if _, err := exec.LookPath("ar"); err != nil {
		t.Skip("ar not installed; skipping LLVM backend tests")
	}
}

func skipTimeoutTests(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("timeout-sensitive test skipped in short mode")
	}
	raw := strings.TrimSpace(os.Getenv("SURGE_SKIP_TIMEOUT_TESTS"))
	if raw == "" {
		return
	}
	if raw == "0" || strings.EqualFold(raw, "false") {
		return
	}
	t.Skip("timeout-sensitive test skipped; set SURGE_SKIP_TIMEOUT_TESTS=0 to run")
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

func runSurgeWithEnv(t *testing.T, root, surgeBin string, env []string, args ...string) (stdout, stderr string, exitCode int) {
	return runSurgeWithInputEnv(t, root, surgeBin, "", env, args...)
}

func runSurgeWithInputEnv(t *testing.T, root, surgeBin, stdin string, env []string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(surgeBin, args...)
	cmd.Dir = root
	cmd.Env = env
	stdout, stderr, exitCode = runCommand(t, cmd, stdin)
	stdout = stripTimingLines(stdout)
	return stdout, stderr, exitCode
}

func runCommand(t *testing.T, cmd *exec.Cmd, stdin string) (stdout, stderr string, exitCode int) {
	t.Helper()
	res := runCommandResult(t, cmd, stdin)
	return res.stdout, res.stderr, res.exitCode
}

func runCommandResult(t *testing.T, cmd *exec.Cmd, stdin string) commandResult {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start)

	res := commandResult{
		stdout:   outBuf.String(),
		stderr:   errBuf.String(),
		duration: dur,
	}

	if err == nil {
		return res
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run command: %v\nstderr:\n%s", err, res.stderr)
	}
	res.exitCode = exitErr.ExitCode()
	res.exitErr = exitErr
	return res
}

func newTestArtifacts(t *testing.T, root string) *testArtifacts {
	t.Helper()
	return newTestArtifactsWithName(t, root, t.Name())
}

func newTestArtifactsWithName(t *testing.T, root, logicalName string) *testArtifacts {
	t.Helper()
	name := sanitizeTestName(logicalName)
	parent := filepath.Join(root, "target", "debug", ".tests")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatalf("create artifacts parent dir: %v", err)
	}
	dir, err := os.MkdirTemp(parent, name+"-")
	if err != nil {
		t.Fatalf("create artifacts dir: %v", err)
	}
	artifacts := &testArtifacts{
		Dir:        dir,
		SourceBase: filepath.Base(dir),
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("artifacts: %s", artifacts.Dir)
			if artifacts.OutputPath != "" {
				t.Logf("binary: %s", formatBinaryStat(artifacts.OutputPath))
			}
			if artifacts.TmpDir != "" {
				t.Logf("tmp dir: %s", artifacts.TmpDir)
			}
			if artifacts.Repro != "" {
				t.Logf("repro: %s", artifacts.Repro)
			}
			if diagnostics := readRunDiagnostics(artifacts.Dir); diagnostics != "" {
				t.Logf("run diagnostics:\n%s", diagnostics)
			}
			return
		}
		if artifacts.OutputPath != "" {
			runArtifactRegistry.Delete(artifacts.OutputPath)
			_ = os.Remove(artifacts.OutputPath)
		}
		if artifacts.TmpDir != "" {
			_ = os.RemoveAll(artifacts.TmpDir)
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

func artifactSourcePath(artifacts *testArtifacts) string {
	return filepath.Join(artifacts.Dir, artifacts.SourceBase+".sg")
}

func runProgramFromSource(t *testing.T, source string, opts runOptions) runResult {
	t.Helper()
	root := repoRoot(t)
	artifacts := newTestArtifacts(t, root)
	srcPath := artifactSourcePath(artifacts)
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
	if artifacts != nil {
		trackLLVMBuildArtifacts(root, artifacts, outputPath)
	}
	repro := llvmReproCommand(root, srcPath, outputPath, opts.argv)
	if artifacts != nil {
		artifacts.Repro = repro
		writeArtifact(t, artifacts.Dir, "repro.txt", repro+"\n")
		writeArtifact(t, artifacts.Dir, "build.tmp_dir", artifacts.TmpDir+"\n")
	}
	if buildCode != 0 {
		artifactsDir := ""
		if artifacts != nil {
			artifactsDir = artifacts.Dir
		}
		t.Fatalf("LLVM build failed (exit=%d). See %s", buildCode, artifactsDir)
	}

	// #nosec G204 -- test executes build output with controlled args
	cmd := exec.Command(outputPath, opts.argv...)
	cmd.Dir = root
	runRes := runCommandResult(t, cmd, opts.stdin)
	stdout, stderr, exitCode := runRes.stdout, runRes.stderr, runRes.exitCode
	diagnostics := ""
	if artifacts != nil && exitCode != 0 && stdout == "" && stderr == "" {
		diagnostics = formatRunDiagnostics(runDiagnostics{
			cmd:          cmd,
			artifactsDir: artifacts.Dir,
			outputPath:   outputPath,
			tmpDir:       artifacts.TmpDir,
			stdout:       stdout,
			stderr:       stderr,
			exitCode:     exitCode,
			exitErr:      runRes.exitErr,
			duration:     runRes.duration,
		})
		writeRunDiagnostics(t, artifacts.Dir, diagnostics)
	}
	if artifacts != nil {
		writeRunOutputArtifacts(t, artifacts.Dir, stdout, stderr, exitCode)
	}
	res := runResult{
		stdout:      stdout,
		stderr:      stderr,
		exitCode:    exitCode,
		diagnostics: diagnostics,
	}
	if artifacts != nil {
		res.artifactsDir = artifacts.Dir
	}
	return res
}

func llvmOutputPath(root, srcPath string) string {
	base := filepath.Base(srcPath)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	return filepath.Join(root, "target", "debug", name)
}

func llvmTmpDir(root, outputPath string) string {
	return filepath.Join(root, "target", "debug", ".tmp", filepath.Base(outputPath))
}

func trackLLVMBuildArtifacts(root string, artifacts *testArtifacts, outputPath string) {
	artifacts.OutputPath = outputPath
	artifacts.TmpDir = llvmTmpDir(root, outputPath)
	runArtifactRegistry.Store(outputPath, runArtifactInfo{
		artifactsDir: artifacts.Dir,
		tmpDir:       artifacts.TmpDir,
	})
}

func lookupRunArtifactInfo(outputPath string) runArtifactInfo {
	if value, ok := runArtifactRegistry.Load(outputPath); ok {
		return value.(runArtifactInfo)
	}
	return runArtifactInfo{}
}

func writeRunDiagnostics(t *testing.T, artifactsDir, diagnostics string) {
	t.Helper()
	if artifactsDir == "" || diagnostics == "" {
		return
	}
	writeArtifact(t, artifactsDir, "run.diagnostics", diagnostics)
}

func writeRunOutputArtifacts(t *testing.T, artifactsDir, stdout, stderr string, exitCode int) {
	t.Helper()
	if artifactsDir == "" {
		return
	}
	writeArtifact(t, artifactsDir, "run.stdout", stdout)
	writeArtifact(t, artifactsDir, "run.stderr", stderr)
	writeArtifact(t, artifactsDir, "run.exit_code", fmt.Sprintf("%d\n", exitCode))
}

func readRunDiagnostics(artifactsDir string) string {
	if artifactsDir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(artifactsDir, "run.diagnostics"))
	if err != nil || len(data) == 0 {
		return ""
	}
	return string(data)
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

type runDiagnostics struct {
	cmd          *exec.Cmd
	artifactsDir string
	outputPath   string
	tmpDir       string
	stdout       string
	stderr       string
	exitCode     int
	exitErr      *exec.ExitError
	duration     time.Duration
	timeout      time.Duration
	ctxErr       error
}

func formatRunDiagnostics(diag runDiagnostics) string {
	var b strings.Builder
	b.WriteString("run diagnostics:\n")
	if diag.cmd != nil {
		fmt.Fprintf(&b, "command: %s\n", diag.cmd.String())
		fmt.Fprintf(&b, "dir: %s\n", diag.cmd.Dir)
	}
	fmt.Fprintf(&b, "artifact_dir: %s\n", diag.artifactsDir)
	fmt.Fprintf(&b, "binary: %s\n", formatBinaryStat(diag.outputPath))
	fmt.Fprintf(&b, "tmp_dir: %s\n", diag.tmpDir)
	fmt.Fprintf(&b, "exit_code: %d\n", diag.exitCode)
	if signal := exitSignal(diag.exitErr); signal != "" {
		fmt.Fprintf(&b, "signal: %s\n", signal)
	}
	fmt.Fprintf(&b, "duration: %s\n", diag.duration)
	if diag.timeout > 0 {
		fmt.Fprintf(&b, "timeout: %s\n", diag.timeout)
	}
	if diag.ctxErr != nil {
		fmt.Fprintf(&b, "context_error: %v\n", diag.ctxErr)
	}
	fmt.Fprintf(&b, "stdout_len: %d\n", len(diag.stdout))
	b.WriteString("stdout:\n")
	b.WriteString(diag.stdout)
	if !strings.HasSuffix(diag.stdout, "\n") {
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "stderr_len: %d\n", len(diag.stderr))
	b.WriteString("stderr:\n")
	b.WriteString(diag.stderr)
	if !strings.HasSuffix(diag.stderr, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

func formatBinaryStat(path string) string {
	if path == "" {
		return "<unknown>"
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("%s (stat error: %v)", path, err)
	}
	return fmt.Sprintf("%s (mode=%s size=%d modtime=%s executable=%t)",
		path,
		info.Mode(),
		info.Size(),
		info.ModTime().Format(time.RFC3339Nano),
		info.Mode().Perm()&0o111 != 0,
	)
}

func exitSignal(exitErr *exec.ExitError) string {
	if exitErr == nil {
		return ""
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}
	return status.Signal().String()
}
