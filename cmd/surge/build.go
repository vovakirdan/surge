// Package main implements the surge CLI.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"surge/internal/buildpipeline"
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
	backendValue, err := cmd.Flags().GetString("backend")
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
	uiValue, err := cmd.Flags().GetString("ui")
	if err != nil {
		return err
	}

	if release && dev {
		return fmt.Errorf("--release and --dev are mutually exclusive")
	}
	if emitLLVM && backendValue != string(buildpipeline.BackendLLVM) {
		return fmt.Errorf("--emit-llvm requires --backend=llvm")
	}

	uiModeValue, err := readUIMode(uiValue)
	if err != nil {
		return err
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
	if backendValue != string(buildpipeline.BackendVM) && backendValue != string(buildpipeline.BackendLLVM) {
		return fmt.Errorf("unsupported backend: %s (supported: vm, llvm)", backendValue)
	}

	maxDiagnostics, err := cmd.Root().PersistentFlags().GetInt("max-diagnostics")
	if err != nil {
		return fmt.Errorf("failed to get max-diagnostics flag: %w", err)
	}

	useTUI := shouldUseTUI(uiModeValue)
	files, fileErr := collectProjectFiles(targetPath, dirInfo)
	if fileErr != nil && len(files) == 0 && targetPath != "" {
		files = []string{targetPath}
	}
	displayFiles := displayFileList(files, baseDir)

	profile := "debug"
	if release {
		profile = "release"
	}

	outputRoot := baseDir
	if outputRoot == "" {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			cwd = "."
		}
		outputRoot = cwd
	}

	compileReq := buildpipeline.CompileRequest{
		TargetPath:     targetPath,
		BaseDir:        baseDir,
		RootKind:       rootKind,
		MaxDiagnostics: maxDiagnostics,
		DirInfo:        toPipelineDirInfo(dirInfo),
		Files:          displayFiles,
	}

	buildReq := buildpipeline.BuildRequest{
		CompileRequest: compileReq,
		OutputName:     outputName,
		OutputRoot:     outputRoot,
		Profile:        profile,
		Backend:        buildpipeline.Backend(backendValue),
		EmitMIR:        emitMIR,
		EmitLLVM:       emitLLVM,
		KeepTmp:        keepTmpFlag,
		PrintCommands:  printCommands,
	}
	if manifestFound {
		buildReq.ManifestRoot = manifest.Root
		buildReq.ManifestFound = true
	}

	var buildRes buildpipeline.BuildResult
	if useTUI && len(displayFiles) > 0 {
		buildRes, err = runBuildWithUI(cmd.Context(), "surge build", displayFiles, &buildReq)
	} else {
		buildRes, err = buildpipeline.Build(cmd.Context(), &buildReq)
	}
	if err != nil {
		printStageTimings(os.Stdout, buildRes.Timings, false, false)
		return err
	}

	if keepTmpFlag {
		_, fprintfErr := fmt.Fprintf(os.Stdout, "tmp dir: %s\n", formatPathForOutput(outputRoot, buildRes.TmpDir))
		if fprintfErr != nil {
			return fprintfErr
		}
	}
	printStageTimings(os.Stdout, buildRes.Timings, true, false)
	_, fprintfErr := fmt.Fprintf(os.Stdout, "built %s\n", formatPathForOutput(outputRoot, buildRes.OutputPath))
	if fprintfErr != nil {
		return fprintfErr
	}
	return nil
}

func formatPathForOutput(root, path string) string {
	if root == "" || path == "" {
		return path
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	if strings.HasPrefix(rel, "..") {
		return path
	}
	return filepath.ToSlash(rel)
}

func init() {
	buildCmd.Flags().Bool("release", false, "optimize for release")
	buildCmd.Flags().Bool("dev", false, "development build with extra checks")
	buildCmd.Flags().String("backend", string(buildpipeline.BackendLLVM), "build backend (vm, llvm)")
	buildCmd.Flags().String("ui", "auto", "user interface (auto|on|off)")
	buildCmd.Flags().Bool("emit-mir", false, "emit MIR dump to target/.tmp")
	buildCmd.Flags().Bool("emit-llvm", false, "emit LLVM IR to target/.tmp (llvm backend only)")
	buildCmd.Flags().Bool("keep-tmp", false, "preserve target/.tmp contents")
	buildCmd.Flags().Bool("print-commands", false, "print LLVM build commands")
}
