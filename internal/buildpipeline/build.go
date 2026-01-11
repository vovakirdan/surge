// Package buildpipeline orchestrates the compilation process.
package buildpipeline

import (
	"context"
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

	"surge/internal/backend/llvm"
	"surge/internal/driver"
	"surge/internal/mir"
	runtimeembed "surge/runtime"
)

// BuildRequest configures output generation for a compilation.
type BuildRequest struct {
	CompileRequest
	OutputName    string
	OutputRoot    string
	Profile       string
	Backend       Backend
	EmitMIR       bool
	EmitLLVM      bool
	KeepTmp       bool
	PrintCommands bool
	ManifestRoot  string
	ManifestFound bool
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

	keepTmp := req.KeepTmp || req.EmitMIR || req.EmitLLVM
	if req.Backend == BackendLLVM || keepTmp {
		if err := os.MkdirAll(tmpDir, 0o750); err != nil {
			return result, fmt.Errorf("failed to create tmp dir: %w", err)
		}
	}

	if req.EmitMIR {
		mirPath := filepath.Join(tmpDir, "out.mir")
		if err := writeMIRDump(mirPath, compileRes.MIR, compileRes.Diagnose); err != nil {
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
		llvmIR, err := llvm.EmitModule(compileRes.MIR, compileRes.Diagnose.Sema.TypeInterner, compileRes.Diagnose.Symbols.Table)
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
		if err := buildLLVMOutput(tmpDir, outputPath, req.PrintCommands); err != nil {
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

func writeMIRDump(targetPath string, mod *mir.Module, result *driver.DiagnoseResult) error {
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
	if err := mir.DumpModule(file, mod, result.Sema.TypeInterner, mir.DumpOptions{}); err != nil {
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

func buildLLVMOutput(tmpDir, outputPath string, printCommands bool) error {
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
	objs := make([]string, 0, len(sources))
	for _, src := range sources {
		base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
		obj := filepath.Join(runtimeDir, base+".o")
		args := []string{"-c", "-std=c11"}
		if runtime.GOOS != "windows" {
			args = append(args, "-pthread")
		}
		args = append(args, src, "-o", obj)
		if err := runCommand(printCommands, "clang", args...); err != nil {
			return nil, err
		}
		objs = append(objs, obj)
	}
	return objs, nil
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
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return err
		}
		return fmt.Errorf("%s: %s", name, msg)
	}
	return nil
}
