// Package buildpipeline orchestrates the compilation process.
package buildpipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"surge/internal/abimanifest"
	"surge/internal/backend/llvm"
	"surge/internal/driver"
	"surge/internal/mir"
	"surge/internal/trace"
	runtimeembed "surge/runtime"
)

// BuildRequest configures output generation for a compilation.
type BuildRequest struct {
	CompileRequest
	OutputName       string
	OutputRoot       string
	Profile          string
	Backend          Backend
	EmitMIR          bool
	EmitMIRAnnotated bool
	EmitLLVM         bool
	KeepTmp          bool
	PrintCommands    bool
	ManifestRoot     string
	ManifestFound    bool
}

// BuildResult captures build artefacts and timings.
type BuildResult struct {
	OutputPath string
	TmpDir     string
	Timings    Timings
	Diagnose   *driver.DiagnoseResult
	MIR        *mir.Module
}

// Build compiles and emits an executable or wrapper script.
func Build(ctx context.Context, req *BuildRequest) (BuildResult, error) {
	var result BuildResult
	if req == nil {
		return result, fmt.Errorf("missing build request")
	}
	reqCopy := *req
	req = &reqCopy

	if req.OutputName == "" {
		req.OutputName = "a.out"
	}
	if req.Profile == "" {
		req.Profile = "debug"
	}

	req.CompileRequest.Backend = req.Backend
	compileRes, err := Compile(ctx, &req.CompileRequest)
	result.Timings = compileRes.Timings
	result.Diagnose = compileRes.Diagnose
	result.MIR = compileRes.MIR
	if err != nil {
		return result, err
	}

	if req.Backend != BackendVM && req.Backend != BackendLLVM {
		err = fmt.Errorf("unsupported backend: %s (supported: vm, llvm)", req.Backend)
		emitStage(req.Progress, req.Files, StageBuild, StatusError, err, 0)
		return result, err
	}

	outputRoot := req.OutputRoot
	if outputRoot == "" {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			cwd = "."
		}
		outputRoot = cwd
	}
	outputDir := filepath.Join(outputRoot, "target", req.Profile)
	outputPath := filepath.Join(outputDir, req.OutputName)
	tmpDir := filepath.Join(outputDir, ".tmp", req.OutputName)
	result.OutputPath = outputPath
	result.TmpDir = tmpDir

	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return result, fmt.Errorf("failed to create output dir: %w", err)
	}

	emitMIR := req.EmitMIR || req.EmitMIRAnnotated
	keepTmp := req.KeepTmp || emitMIR || req.EmitLLVM
	if req.Backend == BackendLLVM || keepTmp {
		if err := os.MkdirAll(tmpDir, 0o750); err != nil {
			return result, fmt.Errorf("failed to create tmp dir: %w", err)
		}
	}

	if emitMIR {
		mirPath := filepath.Join(tmpDir, "out.mir")
		dumpOpts := mir.DumpOptions{AnnotateOwnership: req.EmitMIRAnnotated}
		if compileRes.Diagnose != nil {
			dumpOpts.Sema = compileRes.Diagnose.Sema
		}
		if err := writeMIRDump(mirPath, compileRes.MIR, compileRes.Diagnose, dumpOpts); err != nil {
			emitStage(req.Progress, req.Files, StageBuild, StatusError, err, 0)
			return result, err
		}
	}

	buildStart := time.Now()
	emitStage(req.Progress, req.Files, StageBuild, StatusWorking, nil, 0)

	switch req.Backend {
	case BackendVM:
		script := buildVMWrapperScript(req.ManifestFound, req.ManifestRoot, req.TargetPath, req.BaseDir)
		if err := os.WriteFile(outputPath, []byte(script), 0o600); err != nil {
			err = fmt.Errorf("failed to write build output %q: %w", outputPath, err)
			emitStage(req.Progress, req.Files, StageBuild, StatusError, err, 0)
			return result, err
		}
		// #nosec G302 -- wrapper script must be executable by the current user
		if err := os.Chmod(outputPath, 0o700); err != nil {
			err = fmt.Errorf("failed to mark build output executable: %w", err)
			emitStage(req.Progress, req.Files, StageBuild, StatusError, err, 0)
			return result, err
		}
		result.Timings.Set(StageBuild, time.Since(buildStart))

	case BackendLLVM:
		if err := ensureClangAvailable(); err != nil {
			emitStage(req.Progress, req.Files, StageBuild, StatusError, err, 0)
			return result, err
		}
		llPath := filepath.Join(tmpDir, "out.ll")
		llvmIR, err := llvm.EmitModule(
			compileRes.MIR,
			compileRes.Diagnose.Sema.TypeInterner,
			compileRes.Diagnose.Symbols.Table,
			compileRes.Diagnose.FileSet,
		)
		if err != nil {
			err = fmt.Errorf("LLVM emit failed: %w", err)
			emitStage(req.Progress, req.Files, StageBuild, StatusError, err, 0)
			return result, err
		}
		if err := os.WriteFile(llPath, []byte(llvmIR), 0o600); err != nil {
			err = fmt.Errorf("failed to write LLVM IR: %w", err)
			emitStage(req.Progress, req.Files, StageBuild, StatusError, err, 0)
			return result, err
		}
		result.Timings.Set(StageBuild, time.Since(buildStart))

		linkStart := time.Now()
		emitStage(req.Progress, req.Files, StageLink, StatusWorking, nil, 0)
		abiDebug := trace.FromContext(ctx).Level() >= trace.LevelDebug
		if err := buildLLVMOutput(tmpDir, outputPath, req.PrintCommands, abiDebug); err != nil {
			emitStage(req.Progress, req.Files, StageLink, StatusError, err, 0)
			return result, err
		}
		result.Timings.Set(StageLink, time.Since(linkStart))
	}

	if !keepTmp {
		if err := os.RemoveAll(tmpDir); err != nil {
			return result, fmt.Errorf("failed to clean tmp dir: %w", err)
		}
	}

	emitStage(req.Progress, req.Files, StageBuild, StatusDone, nil, result.Timings.Duration(StageBuild))
	return result, nil
}

func writeMIRDump(targetPath string, mod *mir.Module, result *driver.DiagnoseResult, opts mir.DumpOptions) error {
	if mod == nil || result == nil || result.Sema == nil {
		return fmt.Errorf("missing MIR or type information")
	}
	// #nosec G304 -- path is derived from build output configuration
	file, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to write MIR dump: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			// Игнорируем ошибку закрытия файла, так как основная операция уже завершена
			_ = closeErr
		}
	}()
	if err := mir.DumpModule(file, mod, result.Sema.TypeInterner, opts); err != nil {
		return fmt.Errorf("failed to dump MIR: %w", err)
	}
	return nil
}

func buildVMWrapperScript(manifestFound bool, manifestRoot, targetPath, baseDir string) string {
	if manifestFound {
		return fmt.Sprintf("#!/bin/sh\nset -e\ncd %q\nexec surge run --backend=vm -- \"$@\"\n", manifestRoot)
	}
	absPath := targetPath
	if !filepath.IsAbs(absPath) {
		abs, err := filepath.Abs(targetPath)
		if err == nil {
			absPath = abs
		}
	}
	if baseDir == "" {
		baseDir = "."
	}
	return fmt.Sprintf("#!/bin/sh\nset -e\ncd %q\nexec surge run --backend=vm %q -- \"$@\"\n", baseDir, absPath)
}

func ensureClangAvailable() error {
	if _, err := exec.LookPath("clang"); err != nil {
		return fmt.Errorf("clang not found; install with: sudo apt-get update && sudo apt-get install -y clang llvm lld")
	}
	return nil
}

func buildLLVMOutput(tmpDir, outputPath string, printCommands, abiDebug bool) error {
	runtimeDir, runtimeSources, err := extractNativeRuntime(tmpDir)
	if err != nil {
		return err
	}
	runtimeObjs, err := compileRuntime(runtimeDir, runtimeSources, printCommands)
	if err != nil {
		return err
	}
	libPath, err := archiveRuntime(runtimeDir, runtimeObjs, printCommands)
	if err != nil {
		return err
	}
	objPath := filepath.Join(tmpDir, "out.o")
	llPath := filepath.Join(tmpDir, "out.ll")
	if err := compileLLVMIR(printCommands, llPath, objPath); err != nil {
		return err
	}
	args := []string{objPath, libPath, "-o", outputPath}
	if runtime.GOOS != "windows" {
		args = append(args, "-pthread")
	}
	if err := runCommand(printCommands, "clang", args...); err != nil {
		if isMissingTypedCarrierSentinel(err) {
			return &typedCarrierABIMismatchError{
				expectedHash: abimanifest.GeneratedManifestHash,
				actualHash:   discoverRuntimeABIHash(runtimeDir),
				debug:        abiDebug,
			}
		}
		return err
	}
	return nil
}

func compileLLVMIR(printCommands bool, llPath, objPath string) error {
	if err := runCommand(printCommands, "clang", "-c", "-x", "ir", llPath, "-o", objPath); err == nil {
		return nil
	}
	// Fallback to llc
	// clangErr := err // not used, but could be useful for debugging
	llcPath, llcErr := exec.LookPath("llc")
	if llcErr != nil {
		return fmt.Errorf("clang failed and llc not found: %w", llcErr)
	}
	triple := hostTripleFromClang()
	args := []string{"-filetype=obj", llPath, "-o", objPath}
	if triple != "" {
		args = append([]string{"-mtriple=" + triple}, args...)
	}
	if err := runCommand(printCommands, llcPath, args...); err != nil {
		return fmt.Errorf("clang and llc failed: %w", err)
	}
	if printCommands {
		_, printErr := fmt.Fprintln(os.Stdout, "note: clang IR compile failed; fell back to llc")
		if printErr != nil {
			return fmt.Errorf("failed to print command: %w", printErr)
		}
	}
	return nil
}

func hostTripleFromClang() string {
	out, err := exec.Command("clang", "-dumpmachine").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func extractNativeRuntime(tmpDir string) (runtimeDir string, sources []string, errNativeRuntime error) {
	runtimeDir = filepath.Join(tmpDir, "native_runtime")
	if err := os.MkdirAll(runtimeDir, 0o750); err != nil {
		return "", nil, fmt.Errorf("failed to create native runtime dir: %w", err)
	}

	fsys := runtimeembed.NativeRuntimeFS()
	walkErr := fs.WalkDir(fsys, "native", func(entryPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !runtimeFileAllowed(entryPath) {
			return nil
		}
		rel := strings.TrimPrefix(entryPath, "native/")
		if rel == entryPath {
			return fmt.Errorf("unexpected embedded runtime path: %s", entryPath)
		}
		dst := filepath.Join(runtimeDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return err
		}
		data, errReadFile := fs.ReadFile(fsys, entryPath)
		if errReadFile != nil {
			return errReadFile
		}
		if errWriteFile := os.WriteFile(dst, data, 0o600); errWriteFile != nil {
			return errWriteFile
		}
		if strings.HasSuffix(entryPath, ".c") {
			sources = append(sources, dst)
		}
		return nil
	})
	if walkErr != nil {
		return "", nil, fmt.Errorf("failed to extract embedded runtime sources: %w", walkErr)
	}
	if len(sources) == 0 {
		return "", nil, fmt.Errorf("embedded runtime sources missing (build bug)")
	}
	sort.Strings(sources)
	return runtimeDir, sources, nil
}

func runtimeFileAllowed(entryPath string) bool {
	base := path.Base(entryPath)
	if strings.HasSuffix(base, "_linux.c") || strings.HasSuffix(base, "_linux.h") {
		return runtime.GOOS == "linux"
	}
	if strings.HasSuffix(base, "_darwin.c") || strings.HasSuffix(base, "_darwin.h") {
		return runtime.GOOS == "darwin"
	}
	if strings.HasSuffix(base, "_windows.c") || strings.HasSuffix(base, "_windows.h") {
		return runtime.GOOS == "windows"
	}
	return true
}

func compileRuntime(runtimeDir string, sources []string, printCommands bool) ([]string, error) {
	testSyncPointFlags, err := runtimeTestSyncPointFlags()
	if err != nil {
		return nil, err
	}
	carrierBenchFlags, carrierBenchEnabled, err := runtimeCarrierBenchFlags()
	if err != nil {
		return nil, err
	}
	negativeControlFlags, err := runtimeNegativeControlFlags()
	if err != nil {
		return nil, err
	}
	objs := make([]string, 0, len(sources))
	for _, src := range sources {
		base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
		if !carrierBenchEnabled && (base == "rt_carrier_bench" || base == "rt_carrier_bench_report") {
			continue
		}
		obj := filepath.Join(runtimeDir, base+".o")
		args := []string{"-c", "-std=c11", "-g"}
		if runtime.GOOS != "windows" {
			args = append(args, "-pthread")
		}
		args = append(args, testSyncPointFlags...)
		args = append(args, carrierBenchFlags...)
		args = append(args, negativeControlFlags...)
		args = append(args, src, "-o", obj)
		if err := runCommand(printCommands, "clang", args...); err != nil {
			return nil, err
		}
		objs = append(objs, obj)
	}
	return objs, nil
}

func runtimeCarrierBenchFlags() (flags []string, enabled bool, err error) {
	value := os.Getenv("SURGE_INTERNAL_CARRIER_BENCH_COUNTERS")
	switch value {
	case "":
		return nil, false, nil
	case "1":
		return []string{"-DRT_CARRIER_BENCH_ENABLED"}, true, nil
	default:
		return nil, false, fmt.Errorf(
			"SURGE_INTERNAL_CARRIER_BENCH_COUNTERS must be unset or exactly 1",
		)
	}
}

// runtimeNegativeControlFlags carries a Rule-13 mutant into a program's own
// runtime build. A defect that lives in the native runtime but is only
// observable through a compiled program -- an anchored body's state released
// on cancel, say -- has no other way to be shown red: the C stands cannot
// reach it, and rebuilding the runtime by hand beside the test would measure
// a different tree. So a test names the control here, exactly as
// SURGE_INTERNAL_TEST_SYNC_POINTS names the sync-point build, and the shape
// is refused rather than trusted: only RV2_*_NEGATIVE_CONTROL.
func runtimeNegativeControlFlags() ([]string, error) {
	value := os.Getenv("SURGE_INTERNAL_RUNTIME_NEGATIVE_CONTROL")
	if value == "" {
		return nil, nil
	}
	flags := make([]string, 0, 2)
	for _, name := range strings.Split(value, ",") {
		name = strings.TrimSpace(name)
		if !strings.HasPrefix(name, "RV2_") || !strings.HasSuffix(name, "_NEGATIVE_CONTROL") {
			return nil, fmt.Errorf(
				"SURGE_INTERNAL_RUNTIME_NEGATIVE_CONTROL names %q; every entry must be "+
					"RV2_*_NEGATIVE_CONTROL", name,
			)
		}
		flags = append(flags, "-D"+name)
	}
	return flags, nil
}

func runtimeTestSyncPointFlags() ([]string, error) {
	value := os.Getenv("SURGE_INTERNAL_TEST_SYNC_POINTS")
	switch value {
	case "":
		return nil, nil
	case "1":
		return []string{"-DRT_TEST_SYNC_POINTS"}, nil
	default:
		return nil, fmt.Errorf(
			"SURGE_INTERNAL_TEST_SYNC_POINTS must be unset or exactly 1",
		)
	}
}

func archiveRuntime(runtimeDir string, objs []string, printCommands bool) (string, error) {
	if _, err := exec.LookPath("ar"); err != nil {
		return "", fmt.Errorf("ar not found; install with: sudo apt-get update && sudo apt-get install -y clang llvm lld")
	}
	libPath := filepath.Join(runtimeDir, "libruntime_native.a")
	args := append([]string{"rcs", libPath}, objs...)
	if err := runCommand(printCommands, "ar", args...); err != nil {
		return "", err
	}
	return libPath, nil
}

func runCommand(printCommands bool, name string, args ...string) error {
	if printCommands {
		_, printErr := fmt.Fprintf(os.Stdout, "%s %s\n", name, strings.Join(args, " "))
		if printErr != nil {
			return fmt.Errorf("failed to print command: %w", printErr)
		}
	}
	// #nosec G204 -- name/args are constructed by the build pipeline and executed without a shell.
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		return &commandError{name: name, stderr: msg, cause: err}
	}
	return nil
}

const typedCarrierABIRebuildMessage = "internal compiler/runtime typed-carrier ABI mismatch; rebuild or reinstall Surge so the compiler and runtime come from one revision"

type commandError struct {
	name   string
	stderr string
	cause  error
}

func (err *commandError) Error() string {
	if err.stderr == "" {
		return err.cause.Error()
	}
	return fmt.Sprintf("%s: %s", err.name, err.stderr)
}

func (err *commandError) Unwrap() error {
	return err.cause
}

type typedCarrierABIMismatchError struct {
	expectedHash string
	actualHash   string
	debug        bool
}

func (err *typedCarrierABIMismatchError) Error() string {
	if !err.debug {
		return typedCarrierABIRebuildMessage
	}
	actual := err.actualHash
	if actual == "" {
		actual = "absent"
	}
	return fmt.Sprintf("%s\ntrace: typed-carrier ABI expected=%s actual=%s missing_symbol=%s", typedCarrierABIRebuildMessage, err.expectedHash, actual, abimanifest.SentinelPrefix+err.expectedHash)
}

func isMissingTypedCarrierSentinel(err error) bool {
	var commandErr *commandError
	if !errors.As(err, &commandErr) || commandErr.stderr == "" {
		return false
	}
	lower := strings.ToLower(commandErr.stderr)
	undefined := strings.Contains(lower, "undefined reference") ||
		strings.Contains(lower, "undefined symbol") ||
		strings.Contains(lower, "undefined symbols") ||
		strings.Contains(lower, "unresolved external symbol")
	if !undefined {
		return false
	}
	return containsExactLinkSymbol(commandErr.stderr, abimanifest.GeneratedSentinelSymbol)
}

func containsExactLinkSymbol(output, symbol string) bool {
	for _, token := range strings.FieldsFunc(output, func(r rune) bool {
		return r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	}) {
		if token == symbol || token == "_"+symbol {
			return true
		}
	}
	return false
}

func discoverRuntimeABIHash(runtimeDir string) string {
	// #nosec G304 -- runtimeDir is the private extraction directory created by this build.
	data, err := os.ReadFile(filepath.Join(runtimeDir, "rt_typed_carrier_abi.generated.h"))
	if err != nil {
		return ""
	}
	const marker = "#define SURGE_TYPED_CARRIER_ABI_MANIFEST_HASH \""
	start := bytes.Index(data, []byte(marker))
	if start < 0 {
		return ""
	}
	start += len(marker)
	rest := data[start:]
	end := bytes.IndexByte(rest, '"')
	if end != 64 {
		return ""
	}
	hash := string(rest[:end])
	for _, char := range hash {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return ""
		}
	}
	return hash
}
