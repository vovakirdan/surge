package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"surge/internal/asyncrt"
	"surge/internal/buildpipeline"
	"surge/internal/project"
	"surge/internal/vm"
)

var runCmd = &cobra.Command{
	Use:   "run [flags] [file.sg|directory] [-- <program-args>...]",
	Short: "Build and execute a Surge program",
	Long: `Build a Surge source file or module directory and execute the result.
Arguments after "--" are passed to the program via rt_argv().`,
	Args: cobra.ArbitraryArgs,
	RunE: runExecution,
}

func init() {
	runCmd.Flags().String("backend", string(buildpipeline.BackendLLVM), "execution backend (llvm, vm)")
	runCmd.Flags().String("ui", "off", "user interface (auto|on|off)")
	runCmd.Flags().Bool("vm-trace", false, "enable VM execution tracing")
	runCmd.Flags().Bool("vm-debug", false, "enable VM debugger")
	runCmd.Flags().String("vm-debug-script", "", "run VM debugger commands from file")
	runCmd.Flags().StringArray("vm-break", nil, "add VM breakpoint <file:line> (repeatable)")
	runCmd.Flags().StringArray("vm-break-fn", nil, "add VM function breakpoint <name> (repeatable)")
	runCmd.Flags().String("vm-record", "", "record VM run to NDJSON log")
	runCmd.Flags().String("vm-replay", "", "replay VM run from NDJSON log")
	runCmd.Flags().Bool("fuzz-scheduler", false, "enable fuzzed async scheduling")
	runCmd.Flags().Uint64("fuzz-seed", 1, "seed for fuzzed async scheduling (default 1)")
	runCmd.Flags().Bool("real-time", false, "use real-time async timers (monotonic clock)")
	runCmd.Flags().Bool("unsafe", false, "run even if diagnostics report errors")
}

func runExecution(cmd *cobra.Command, args []string) error {
	argsBeforeDash, argsAfterDash := splitArgsAtDash(cmd, args)

	manifest, manifestFound, err := loadProjectManifest(".")
	if err != nil {
		return err
	}
	var (
		targetPath  string
		dirInfo     *runDirInfo
		programArgs []string
		baseDir     string
		rootKind    project.ModuleKind
		outputName  string
	)
	if manifestFound {
		targetPath, dirInfo, err = resolveProjectRunTarget(manifest)
		if err != nil {
			return err
		}
		baseDir = manifest.Root
		rootKind = project.ModuleKindBinary
		outputName = manifest.Config.Package.Name
		programArgs = argsAfterDash
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
		programArgs = append(programArgs, argsBeforeDash[1:]...)
		if len(argsAfterDash) > 0 {
			programArgs = append(programArgs, argsAfterDash...)
		}
	}
	if outputName == "" {
		outputName = "a.out"
	}

	backendValue, err := cmd.Flags().GetString("backend")
	if err != nil {
		return fmt.Errorf("failed to get backend flag: %w", err)
	}
	uiValue, err := cmd.Flags().GetString("ui")
	if err != nil {
		return err
	}
	uiModeValue, err := readUIMode(uiValue)
	if err != nil {
		return err
	}

	vmTrace, err := cmd.Flags().GetBool("vm-trace")
	if err != nil {
		return fmt.Errorf("failed to get vm-trace flag: %w", err)
	}
	vmDebug, err := cmd.Flags().GetBool("vm-debug")
	if err != nil {
		return fmt.Errorf("failed to get vm-debug flag: %w", err)
	}
	vmDebugScript, err := cmd.Flags().GetString("vm-debug-script")
	if err != nil {
		return fmt.Errorf("failed to get vm-debug-script flag: %w", err)
	}
	vmBreaks, err := cmd.Flags().GetStringArray("vm-break")
	if err != nil {
		return fmt.Errorf("failed to get vm-break flag: %w", err)
	}
	vmBreakFns, err := cmd.Flags().GetStringArray("vm-break-fn")
	if err != nil {
		return fmt.Errorf("failed to get vm-break-fn flag: %w", err)
	}

	vmRecordPath, err := cmd.Flags().GetString("vm-record")
	if err != nil {
		return fmt.Errorf("failed to get vm-record flag: %w", err)
	}
	vmReplayPath, err := cmd.Flags().GetString("vm-replay")
	if err != nil {
		return fmt.Errorf("failed to get vm-replay flag: %w", err)
	}
	fuzzScheduler, err := cmd.Flags().GetBool("fuzz-scheduler")
	if err != nil {
		return fmt.Errorf("failed to get fuzz-scheduler flag: %w", err)
	}
	fuzzSeed, err := cmd.Flags().GetUint64("fuzz-seed")
	if err != nil {
		return fmt.Errorf("failed to get fuzz-seed flag: %w", err)
	}
	realTime, err := cmd.Flags().GetBool("real-time")
	if err != nil {
		return fmt.Errorf("failed to get real-time flag: %w", err)
	}
	unsafeRun, err := cmd.Flags().GetBool("unsafe")
	if err != nil {
		return fmt.Errorf("failed to get unsafe flag: %w", err)
	}
	maxDiagnostics, err := cmd.Root().PersistentFlags().GetInt("max-diagnostics")
	if err != nil {
		return fmt.Errorf("failed to get max-diagnostics flag: %w", err)
	}
	if vmRecordPath != "" && vmReplayPath != "" {
		return fmt.Errorf("--vm-record and --vm-replay are mutually exclusive")
	}

	if !vmDebug && (vmDebugScript != "" || len(vmBreaks) > 0 || len(vmBreakFns) > 0) {
		return fmt.Errorf("--vm-debug is required when using --vm-debug-script/--vm-break/--vm-break-fn")
	}
	if vmDebug && (vmRecordPath != "" || vmReplayPath != "") {
		return fmt.Errorf("--vm-record/--vm-replay are not supported with --vm-debug")
	}

	if backendValue != string(buildpipeline.BackendVM) && backendValue != string(buildpipeline.BackendLLVM) {
		return fmt.Errorf("unsupported backend: %s (supported: vm, llvm)", backendValue)
	}
	if backendValue != string(buildpipeline.BackendVM) && (vmTrace || vmDebug || vmDebugScript != "" || len(vmBreaks) > 0 || len(vmBreakFns) > 0 || vmRecordPath != "" || vmReplayPath != "" || fuzzScheduler || realTime) {
		return fmt.Errorf("VM-only flags require --backend=vm")
	}

	useTUI := shouldUseTUI(uiModeValue)
	files, fileErr := collectProjectFiles(targetPath, dirInfo)
	if fileErr != nil && len(files) == 0 && targetPath != "" {
		files = []string{targetPath}
	}
	displayFiles := displayFileList(files, baseDir)

	compileReq := buildpipeline.CompileRequest{
		TargetPath:            targetPath,
		BaseDir:               baseDir,
		RootKind:              rootKind,
		MaxDiagnostics:        maxDiagnostics,
		DirInfo:               toPipelineDirInfo(dirInfo),
		AllowDiagnosticsError: unsafeRun,
		Files:                 displayFiles,
	}

	outputRoot := baseDir
	if outputRoot == "" {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			cwd = "."
		}
		outputRoot = cwd
	}

	if backendValue == string(buildpipeline.BackendLLVM) {
		buildReq := buildpipeline.BuildRequest{
			CompileRequest: compileReq,
			OutputName:     outputName,
			OutputRoot:     outputRoot,
			Profile:        "debug",
			Backend:        buildpipeline.BackendLLVM,
		}
		if manifestFound {
			buildReq.ManifestRoot = manifest.Root
			buildReq.ManifestFound = true
		}

		var buildRes buildpipeline.BuildResult
		if useTUI && len(displayFiles) > 0 {
			buildRes, err = runBuildWithUI(cmd.Context(), "surge run", displayFiles, &buildReq)
		} else {
			buildRes, err = buildpipeline.Build(cmd.Context(), &buildReq)
		}
		if err != nil {
			printStageTimings(os.Stdout, buildRes.Timings, false, true)
			return err
		}

		runStart := time.Now()
		runErr := runBinary(buildRes.OutputPath, programArgs, outputRoot)
		buildRes.Timings.Set(buildpipeline.StageRun, time.Since(runStart))
		printStageTimings(os.Stdout, buildRes.Timings, true, true)
		if runErr != nil {
			var exitErr *exec.ExitError
			if errors.As(runErr, &exitErr) {
				os.Exit(exitErr.ExitCode())
			}
			return runErr
		}
		os.Exit(0)
		return nil
	}

	var compileRes buildpipeline.CompileResult
	if useTUI && len(displayFiles) > 0 {
		compileRes, err = runCompileWithUI(cmd.Context(), "surge run", displayFiles, &compileReq)
	} else {
		compileRes, err = buildpipeline.Compile(cmd.Context(), &compileReq)
	}
	if err != nil {
		printStageTimings(os.Stdout, compileRes.Timings, false, true)
		return err
	}

	// Create VM runtime
	var rt vm.Runtime = vm.NewRuntimeWithArgs(programArgs)
	var recordBuf bytes.Buffer
	var recorder *vm.Recorder
	if vmRecordPath != "" {
		recorder = vm.NewRecorder(&recordBuf)
		rt = vm.NewRecordingRuntime(rt, recorder)
	}

	var tracer *vm.Tracer
	if vmTrace {
		tracer = vm.NewTracer(os.Stderr, compileRes.Diagnose.FileSet)
	}

	vmInstance := vm.New(compileRes.MIR, rt, compileRes.Diagnose.FileSet, compileRes.Diagnose.Sema.TypeInterner, tracer)
	timerMode := asyncrt.TimerModeVirtual
	if realTime {
		timerMode = asyncrt.TimerModeReal
	}
	vmInstance.AsyncConfig = asyncrt.Config{
		Deterministic: !fuzzScheduler,
		Fuzz:          fuzzScheduler,
		Seed:          fuzzSeed,
		TimerMode:     timerMode,
	}
	if recorder != nil {
		vmInstance.Recorder = recorder
	}
	if vmReplayPath != "" {
		// #nosec G304 -- path comes from user-provided CLI flag
		logBytes, err := os.ReadFile(vmReplayPath)
		if err != nil {
			return fmt.Errorf("failed to read vm-replay log: %w", err)
		}
		rp := vm.NewReplayerFromBytes(logBytes)
		vmInstance.Replayer = rp
		vmInstance.RT = vm.NewReplayRuntime(vmInstance, rp)
	}

	if vmDebug {
		interactive := vmDebugScript == ""
		var in io.Reader = os.Stdin
		if !interactive {
			// #nosec G304 -- path comes from user-provided CLI flag
			script, err := os.ReadFile(vmDebugScript)
			if err != nil {
				return fmt.Errorf("failed to open vm-debug-script: %w", err)
			}
			in = bytes.NewReader(script)
		}

		dbg := vm.NewDebugger(vmInstance, in, os.Stdout, interactive)
		bps := dbg.Breakpoints()
		for _, spec := range vmBreaks {
			file, line, err := vm.ParseFileLineSpec(spec)
			if err != nil {
				return fmt.Errorf("invalid --vm-break %q: %w", spec, err)
			}
			if _, err := bps.AddFileLine(file, line); err != nil {
				return fmt.Errorf("invalid --vm-break %q: %w", spec, err)
			}
		}
		for _, name := range vmBreakFns {
			if _, err := bps.AddFuncEntry(name); err != nil {
				return fmt.Errorf("invalid --vm-break-fn %q: %w", name, err)
			}
		}

		runStart := time.Now()
		res, vmErr := dbg.Run()
		compileRes.Timings.Set(buildpipeline.StageRun, time.Since(runStart))
		printStageTimings(os.Stdout, compileRes.Timings, true, true)
		if vmErr != nil {
			fmt.Fprint(os.Stderr, vmErr.FormatWithFiles(compileRes.Diagnose.FileSet))
			os.Exit(1)
		}
		if res.Quit {
			os.Exit(125)
		}
		os.Exit(res.ExitCode)
		return nil
	}

	runStart := time.Now()
	vmErr := vmInstance.Run()
	compileRes.Timings.Set(buildpipeline.StageRun, time.Since(runStart))

	if recorder != nil {
		if err := recorder.Err(); err != nil {
			printStageTimings(os.Stdout, compileRes.Timings, true, true)
			fmt.Fprintf(os.Stderr, "vm record failed: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(vmRecordPath, recordBuf.Bytes(), 0o600); err != nil {
			printStageTimings(os.Stdout, compileRes.Timings, true, true)
			fmt.Fprintf(os.Stderr, "vm record write failed: %v\n", err)
			os.Exit(1)
		}
	}
	if vmErr != nil {
		printStageTimings(os.Stdout, compileRes.Timings, true, true)
		fmt.Fprint(os.Stderr, vmErr.FormatWithFiles(compileRes.Diagnose.FileSet))
		os.Exit(1)
	}

	printStageTimings(os.Stdout, compileRes.Timings, true, true)
	os.Exit(vmInstance.ExitCode)
	return nil
}

type runDirInfo struct {
	path      string
	fileCount int
}

func resolveRunTarget(inputPath string) (string, *runDirInfo, error) {
	info, err := os.Stat(inputPath)
	if err != nil {
		return "", nil, fmt.Errorf("failed to stat path: %w", err)
	}
	if !info.IsDir() {
		return inputPath, nil, nil
	}

	sgFiles, err := listSGFiles(inputPath)
	if err != nil {
		return "", nil, fmt.Errorf("failed to list files in directory: %w", err)
	}

	if len(sgFiles) == 0 {
		return "", nil, fmt.Errorf("no .sg files found in directory %q", inputPath)
	}
	return sgFiles[0], &runDirInfo{path: inputPath, fileCount: len(sgFiles)}, nil
}

func splitArgsAtDash(cmd *cobra.Command, args []string) (before, after []string) {
	if cmd == nil {
		return args, nil
	}
	dashIdx := cmd.Flags().ArgsLenAtDash()
	if dashIdx < 0 || dashIdx > len(args) {
		return args, nil
	}
	return args[:dashIdx], args[dashIdx:]
}

func runBinary(path string, args []string, workDir string) error {
	cmd := exec.Command(path, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if workDir != "" {
		cmd.Dir = workDir
	}
	return cmd.Run()
}
