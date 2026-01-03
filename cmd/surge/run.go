package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"surge/internal/asyncrt"
	"surge/internal/driver"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/project"
	"surge/internal/vm"
)

var runCmd = &cobra.Command{
	Use:   "run [flags] [file.sg|directory] [-- <program-args>...]",
	Short: "Compile and execute a Surge program",
	Long: `Compile a Surge source file or module directory to MIR and execute it using the VM backend.
Arguments after "--" are passed to the program via rt_argv().`,
	Args: cobra.ArbitraryArgs,
	RunE: runExecution,
}

func init() {
	runCmd.Flags().String("backend", "vm", "execution backend (vm)")
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
	)
	if manifestFound {
		targetPath, dirInfo, err = resolveProjectRunTarget(manifest)
		if err != nil {
			return err
		}
		baseDir = manifest.Root
		rootKind = project.ModuleKindBinary
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
		programArgs = append(programArgs, argsBeforeDash[1:]...)
		if len(argsAfterDash) > 0 {
			programArgs = append(programArgs, argsAfterDash...)
		}
	}

	// Get flags
	backend, err := cmd.Flags().GetString("backend")
	if err != nil {
		return fmt.Errorf("failed to get backend flag: %w", err)
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

	// Only VM backend supported for now
	if backend != "vm" {
		return fmt.Errorf("unsupported backend: %s (only 'vm' is supported)", backend)
	}

	// Compile source to MIR
	opts := driver.DiagnoseOptions{
		Stage:              driver.DiagnoseStageSema,
		MaxDiagnostics:     maxDiagnostics,
		EmitHIR:            true,
		EmitInstantiations: true,
		BaseDir:            baseDir,
		RootKind:           rootKind,
	}

	result, err := driver.DiagnoseWithOptions(cmd.Context(), targetPath, &opts)
	if err != nil {
		return fmt.Errorf("compilation failed: %w", err)
	}

	// Check for errors
	if result.Bag != nil && result.Bag.HasErrors() {
		for _, d := range result.Bag.Items() {
			fmt.Fprintln(os.Stderr, d.Message)
		}
		if !unsafeRun {
			return fmt.Errorf("diagnostics reported errors")
		}
	}
	if validateErr := validateEntrypoints(result); validateErr != nil {
		return validateErr
	}

	if dirInfo != nil && dirInfo.fileCount > 1 {
		meta := result.RootModuleMeta()
		if meta == nil {
			return fmt.Errorf("failed to resolve module metadata for %q", dirInfo.path)
		}
		if !meta.HasModulePragma {
			return fmt.Errorf("directory %q is not a module; add pragma module/binary to all .sg files or run a file", dirInfo.path)
		}
	}

	// Build HIR (should already be built with EmitHIR=true)
	if result.HIR == nil {
		return fmt.Errorf("HIR not available")
	}
	if result.Instantiations == nil {
		return fmt.Errorf("instantiation map not available")
	}
	if result.Sema == nil {
		return fmt.Errorf("semantic analysis result not available")
	}

	hirModule, err := driver.CombineHIRWithModules(cmd.Context(), result)
	if err != nil {
		return fmt.Errorf("HIR merge failed: %w", err)
	}
	if hirModule == nil {
		hirModule = result.HIR
	}

	// Monomorphize
	mm, err := mono.MonomorphizeModule(hirModule, result.Instantiations, result.Sema, mono.Options{
		MaxDepth: 64,
	})
	if err != nil {
		return fmt.Errorf("monomorphization failed: %w", err)
	}

	// Lower to MIR
	mirMod, err := mir.LowerModule(mm, result.Sema)
	if err != nil {
		return fmt.Errorf("MIR lowering failed: %w", err)
	}

	// Simplify CFG and recognize switch patterns
	for _, f := range mirMod.Funcs {
		mir.SimplifyCFG(f)
		mir.RecognizeSwitchTag(f)
		mir.SimplifyCFG(f)
	}

	// Lower async functions to poll state machines.
	if err := mir.LowerAsyncStateMachine(mirMod, result.Sema, result.Symbols.Table); err != nil {
		return fmt.Errorf("async lowering failed: %w", err)
	}
	for _, f := range mirMod.Funcs {
		mir.SimplifyCFG(f)
	}

	// Validate MIR
	if err := mir.Validate(mirMod, result.Sema.TypeInterner); err != nil {
		return fmt.Errorf("MIR validation failed: %w", err)
	}

	// Create VM
	var rt vm.Runtime = vm.NewRuntimeWithArgs(programArgs)
	var recordBuf bytes.Buffer
	var recorder *vm.Recorder
	if vmRecordPath != "" {
		recorder = vm.NewRecorder(&recordBuf)
		rt = vm.NewRecordingRuntime(rt, recorder)
	}

	var tracer *vm.Tracer
	if vmTrace {
		tracer = vm.NewTracer(os.Stderr, result.FileSet)
	}

	vmInstance := vm.New(mirMod, rt, result.FileSet, result.Sema.TypeInterner, tracer)
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

		res, vmErr := dbg.Run()
		if vmErr != nil {
			fmt.Fprint(os.Stderr, vmErr.FormatWithFiles(result.FileSet))
			os.Exit(1)
		}
		if res.Quit {
			os.Exit(125)
		}
		os.Exit(res.ExitCode)
	}

	// Execute (non-debug mode).
	vmErr := vmInstance.Run()
	if recorder != nil {
		if err := recorder.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "vm record failed: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(vmRecordPath, recordBuf.Bytes(), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "vm record write failed: %v\n", err)
			os.Exit(1)
		}
	}
	if vmErr != nil {
		fmt.Fprint(os.Stderr, vmErr.FormatWithFiles(result.FileSet))
		os.Exit(1)
	}

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
	entries, err := os.ReadDir(inputPath)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read directory: %w", err)
	}
	sgFiles := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".sg" {
			continue
		}
		sgFiles = append(sgFiles, filepath.Join(inputPath, entry.Name()))
	}
	sort.Strings(sgFiles)
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
