package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"surge/internal/backend/llvm"
	"surge/internal/driver"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/project"
)

var buildCmd = &cobra.Command{
	Use:   "build [flags] [path]",
	Short: "Build a surge project",
	Long:  "Build a surge project using surge.toml as the entrypoint definition.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  buildExecution,
}

func buildExecution(cmd *cobra.Command, args []string) error {
	release, err := cmd.Flags().GetBool("release")
	if err != nil {
		return err
	}
	dev, err := cmd.Flags().GetBool("dev")
	if err != nil {
		return err
	}
	backend, err := cmd.Flags().GetString("backend")
	if err != nil {
		return err
	}
	emitMIR, err := cmd.Flags().GetBool("emit-mir")
	if err != nil {
		return err
	}
	emitLLVM, err := cmd.Flags().GetBool("emit-llvm")
	if err != nil {
		return err
	}
	keepTmpFlag, err := cmd.Flags().GetBool("keep-tmp")
	if err != nil {
		return err
	}
	printCommands, err := cmd.Flags().GetBool("print-commands")
	if err != nil {
		return err
	}

	if release && dev {
		return fmt.Errorf("--release and --dev are mutually exclusive")
	}
	if emitLLVM && backend != "llvm" {
		return fmt.Errorf("--emit-llvm requires --backend=llvm")
	}

	argsBeforeDash, _ := splitArgsAtDash(cmd, args)

	manifest, manifestFound, err := loadProjectManifest(".")
	if err != nil {
		return err
	}
	var (
		targetPath string
		dirInfo    *runDirInfo
		baseDir    string
		rootKind   project.ModuleKind
		outputName string
	)
	if manifestFound {
		targetPath, dirInfo, err = resolveProjectRunTarget(manifest)
		if err != nil {
			return err
		}
		baseDir = manifest.Root
		rootKind = project.ModuleKindBinary
		outputName = manifest.Config.Package.Name
	} else {
		if len(argsBeforeDash) == 0 || filepath.Clean(argsBeforeDash[0]) == "." {
			return errors.New(noSurgeTomlMessage)
		}
		inputPath := argsBeforeDash[0]
		targetPath, dirInfo, err = resolveRunTarget(inputPath)
		if err != nil {
			return err
		}
		outputName = outputNameFromPath(inputPath, dirInfo)
	}
	if outputName == "" {
		outputName = "a.out"
	}
	if backend != "vm" && backend != "llvm" {
		return fmt.Errorf("unsupported backend: %s (supported: vm, llvm)", backend)
	}

	maxDiagnostics, err := cmd.Root().PersistentFlags().GetInt("max-diagnostics")
	if err != nil {
		return fmt.Errorf("failed to get max-diagnostics flag: %w", err)
	}

	result, mirMod, err := compileToMIR(cmd, targetPath, baseDir, rootKind, maxDiagnostics, dirInfo)
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	buildDir := filepath.Join(cwd, "build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return fmt.Errorf("failed to create build dir: %w", err)
	}
	outputPath := filepath.Join(buildDir, outputName)
	tmpDir := filepath.Join(buildDir, ".tmp", outputName)
	keepTmp := keepTmpFlag || emitMIR || emitLLVM
	if backend == "llvm" || emitMIR || emitLLVM || keepTmpFlag {
		if err := os.MkdirAll(tmpDir, 0o755); err != nil {
			return fmt.Errorf("failed to create tmp dir: %w", err)
		}
	}

	if emitMIR {
		mirPath := filepath.Join(tmpDir, "out.mir")
		if err := writeMIRDump(mirPath, mirMod, result); err != nil {
			return err
		}
	}

	switch backend {
	case "vm":
		script, err := buildVMWrapperScript(manifest, manifestFound, targetPath, baseDir)
		if err != nil {
			return err
		}
		if err := os.WriteFile(outputPath, []byte(script), 0o644); err != nil {
			return fmt.Errorf("failed to write build output %q: %w", outputPath, err)
		}
		if err := os.Chmod(outputPath, 0o755); err != nil {
			return fmt.Errorf("failed to mark build output executable: %w", err)
		}

	case "llvm":
		if err := ensureClangAvailable(); err != nil {
			return err
		}
		llPath := filepath.Join(tmpDir, "out.ll")
		llvmIR, err := llvm.EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
		if err != nil {
			return fmt.Errorf("LLVM emit failed: %w", err)
		}
		if err := os.WriteFile(llPath, []byte(llvmIR), 0o644); err != nil {
			return fmt.Errorf("failed to write LLVM IR: %w", err)
		}
		if err := buildLLVMOutput(tmpDir, outputPath, printCommands); err != nil {
			return err
		}
	}

	if !keepTmp {
		if err := os.RemoveAll(tmpDir); err != nil {
			return fmt.Errorf("failed to clean tmp dir: %w", err)
		}
	}

	fmt.Fprintf(os.Stdout, "built %s\n", outputPath)
	return nil
}

func outputNameFromPath(inputPath string, dirInfo *runDirInfo) string {
	if dirInfo != nil {
		return filepath.Base(dirInfo.path)
	}
	base := filepath.Base(inputPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func compileToMIR(cmd *cobra.Command, targetPath, baseDir string, rootKind project.ModuleKind, maxDiagnostics int, dirInfo *runDirInfo) (*driver.DiagnoseResult, *mir.Module, error) {
	opts := driver.DiagnoseOptions{
		Stage:              driver.DiagnoseStageSema,
		MaxDiagnostics:     maxDiagnostics,
		EmitHIR:            true,
		EmitInstantiations: true,
		BaseDir:            baseDir,
		RootKind:           rootKind,
	}
	result, err := driver.DiagnoseWithOptions(cmd.Context(), targetPath, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("compilation failed: %w", err)
	}
	if result.Bag != nil && result.Bag.HasErrors() {
		for _, d := range result.Bag.Items() {
			fmt.Fprintln(os.Stderr, d.Message)
		}
		return nil, nil, fmt.Errorf("diagnostics reported errors")
	}
	if err := validateEntrypoints(result); err != nil {
		return nil, nil, err
	}
	if dirInfo != nil && dirInfo.fileCount > 1 {
		meta := result.RootModuleMeta()
		if meta == nil {
			return nil, nil, fmt.Errorf("failed to resolve module metadata for %q", dirInfo.path)
		}
		if !meta.HasModulePragma {
			return nil, nil, fmt.Errorf("directory %q is not a module; add pragma module/binary to all .sg files or run a file", dirInfo.path)
		}
	}

	if result.HIR == nil {
		return nil, nil, fmt.Errorf("HIR not available")
	}
	if result.Instantiations == nil {
		return nil, nil, fmt.Errorf("instantiation map not available")
	}
	if result.Sema == nil {
		return nil, nil, fmt.Errorf("semantic analysis result not available")
	}

	hirModule, err := driver.CombineHIRWithModules(cmd.Context(), result)
	if err != nil {
		return nil, nil, fmt.Errorf("HIR merge failed: %w", err)
	}
	if hirModule == nil {
		hirModule = result.HIR
	}

	mm, err := mono.MonomorphizeModule(hirModule, result.Instantiations, result.Sema, mono.Options{
		MaxDepth: 64,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("monomorphization failed: %w", err)
	}

	mirMod, err := mir.LowerModule(mm, result.Sema)
	if err != nil {
		return nil, nil, fmt.Errorf("MIR lowering failed: %w", err)
	}

	for _, f := range mirMod.Funcs {
		mir.SimplifyCFG(f)
		mir.RecognizeSwitchTag(f)
		mir.SimplifyCFG(f)
	}

	if err := mir.LowerAsyncStateMachine(mirMod, result.Sema, result.Symbols.Table); err != nil {
		return nil, nil, fmt.Errorf("async lowering failed: %w", err)
	}
	for _, f := range mirMod.Funcs {
		mir.SimplifyCFG(f)
	}

	if err := mir.Validate(mirMod, result.Sema.TypeInterner); err != nil {
		return nil, nil, fmt.Errorf("MIR validation failed: %w", err)
	}

	return result, mirMod, nil
}

func writeMIRDump(path string, mod *mir.Module, result *driver.DiagnoseResult) error {
	if mod == nil || result == nil || result.Sema == nil {
		return fmt.Errorf("missing MIR or type information")
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to write MIR dump: %w", err)
	}
	defer file.Close()
	if err := mir.DumpModule(file, mod, result.Sema.TypeInterner, mir.DumpOptions{}); err != nil {
		return fmt.Errorf("failed to dump MIR: %w", err)
	}
	return nil
}

func buildVMWrapperScript(manifest *projectManifest, manifestFound bool, targetPath, baseDir string) (string, error) {
	if manifestFound {
		return fmt.Sprintf("#!/bin/sh\nset -e\ncd %q\nexec surge run --backend=vm -- \"$@\"\n", manifest.Root), nil
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
	return fmt.Sprintf("#!/bin/sh\nset -e\ncd %q\nexec surge run --backend=vm %q -- \"$@\"\n", baseDir, absPath), nil
}

func ensureClangAvailable() error {
	if _, err := exec.LookPath("clang"); err != nil {
		return fmt.Errorf("clang not found; install with: sudo apt-get update && sudo apt-get install -y clang llvm lld")
	}
	return nil
}

func buildLLVMOutput(tmpDir, outputPath string, printCommands bool) error {
	runtimeObjs, err := compileRuntime(tmpDir, printCommands)
	if err != nil {
		return err
	}
	libPath, err := archiveRuntime(tmpDir, runtimeObjs, printCommands)
	if err != nil {
		return err
	}
	objPath := filepath.Join(tmpDir, "out.o")
	llPath := filepath.Join(tmpDir, "out.ll")
	if err := runCommand(printCommands, "clang", "-c", "-x", "ir", llPath, "-o", objPath); err != nil {
		return err
	}
	if err := runCommand(printCommands, "clang", objPath, libPath, "-o", outputPath); err != nil {
		return err
	}
	return nil
}

func compileRuntime(tmpDir string, printCommands bool) ([]string, error) {
	sources := []string{
		filepath.Join("runtime", "native", "rt_alloc.c"),
		filepath.Join("runtime", "native", "rt_io.c"),
		filepath.Join("runtime", "native", "rt_string.c"),
		filepath.Join("runtime", "native", "rt_range.c"),
		filepath.Join("runtime", "native", "rt_entry.c"),
	}
	objs := make([]string, 0, len(sources))
	for _, src := range sources {
		base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
		obj := filepath.Join(tmpDir, base+".o")
		if err := runCommand(printCommands, "clang", "-c", "-std=c11", src, "-o", obj); err != nil {
			return nil, err
		}
		objs = append(objs, obj)
	}
	return objs, nil
}

func archiveRuntime(tmpDir string, objs []string, printCommands bool) (string, error) {
	if _, err := exec.LookPath("ar"); err != nil {
		return "", fmt.Errorf("ar not found; install with: sudo apt-get update && sudo apt-get install -y clang llvm lld")
	}
	libPath := filepath.Join(tmpDir, "libruntime_native.a")
	args := append([]string{"rcs", libPath}, objs...)
	if err := runCommand(printCommands, "ar", args...); err != nil {
		return "", err
	}
	return libPath, nil
}

func runCommand(printCommands bool, name string, args ...string) error {
	if printCommands {
		fmt.Fprintf(os.Stdout, "%s %s\n", name, strings.Join(args, " "))
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

// init registers the command-line flags for buildCmd.
// It adds the --release flag ("optimize for release") and the --dev flag ("development build with extra checks").
func init() {
	buildCmd.Flags().Bool("release", false, "optimize for release")
	buildCmd.Flags().Bool("dev", false, "development build with extra checks")
	buildCmd.Flags().String("backend", "vm", "build backend (vm, llvm)")
	buildCmd.Flags().Bool("emit-mir", false, "emit MIR dump to build/.tmp")
	buildCmd.Flags().Bool("emit-llvm", false, "emit LLVM IR to build/.tmp (llvm backend only)")
	buildCmd.Flags().Bool("keep-tmp", false, "preserve build/.tmp contents")
	buildCmd.Flags().Bool("print-commands", false, "print LLVM build commands")
}
