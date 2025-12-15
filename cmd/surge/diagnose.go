package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"surge/internal/diag"
	"surge/internal/diagfmt"
	"surge/internal/directive"
	"surge/internal/driver"
	"surge/internal/hir"
	"surge/internal/mono"
	"surge/internal/parser"
	"surge/internal/source"
	"surge/internal/trace"
)

var diagCmd = &cobra.Command{
	Use:   "diag [flags] <file.sg|directory>",
	Short: "Run diagnostics on a surge source file or directory",
	Long:  `Run diagnostics to find syntax and semantic issues in surge source files or all *.sg files within a directory`,
	Args:  cobra.ExactArgs(1),
	RunE:  runDiagnose,
}

// init registers CLI flags for the diag command used by runDiagnose.
// It configures output format, diagnostic stages, warning handling, concurrency,
// note/suggestion inclusion, and whether to emit absolute file paths.
func init() {
	diagCmd.Flags().String("format", "pretty", "output format (pretty|json|sarif|short)")
	diagCmd.Flags().String("stages", "all", "diagnostic stages to run (tokenize|syntax|sema|all)")
	diagCmd.Flags().Bool("no-warnings", false, "ignore warnings in diagnostics")
	diagCmd.Flags().Bool("warnings-as-errors", false, "treat warnings as errors")
	diagCmd.Flags().Bool("no-alien-hints", false, "disable extra alien-hint diagnostics (enabled by default)")
	diagCmd.Flags().Int("jobs", 0, "max parallel workers for directory processing (0=auto)")
	diagCmd.Flags().Bool("with-notes", false, "include diagnostic notes in output")
	diagCmd.Flags().Bool("suggest", false, "include fix suggestions in output")
	diagCmd.Flags().Bool("preview", false, "preview changes without modifying files")
	diagCmd.Flags().Bool("fullpath", false, "emit absolute file paths in output")
	diagCmd.Flags().Bool("disk-cache", false, "enable persistent disk cache for module metadata (experimental)")
	diagCmd.Flags().String("directives", "off", "directive processing mode (off|collect|gen|run)")
	diagCmd.Flags().String("directives-filter", "test", "comma-separated directive namespaces to process")
	diagCmd.Flags().Bool("emit-hir", false, "emit HIR (High-level IR) representation after successful analysis")
	diagCmd.Flags().Bool("emit-borrow", false, "emit borrow graph + move plan (requires HIR)")
	diagCmd.Flags().Bool("emit-instantiations", false, "emit generic instantiation map (requires sema)")
	diagCmd.Flags().Bool("emit-mono", false, "emit monomorphized HIR (requires sema)")
	diagCmd.Flags().Bool("mono-dce", false, "enable DCE for monomorphized output (experimental)")
	diagCmd.Flags().Int("mono-max-depth", 64, "max monomorphization recursion depth")
}

// runDiagnose executes the "diag" command: it parses command flags, runs diagnostics
// for the provided path (single file or directory), formats the results in the chosen
// output format (pretty, json, or sarif), and exits with a non-zero status when any
// diagnostics contain errors.
//
// It returns nil on successful completion; otherwise it returns an error for flag
// retrieval failures, unknown flag values, filesystem/stat errors, diagnosis failures,
// or formatting/encoding errors.
func runDiagnose(cmd *cobra.Command, args []string) error {
	// Ensure trace is dumped on panic
	defer dumpTraceOnPanic()

	filePath := args[0]

	// Получаем флаги
	format, err := cmd.Flags().GetString("format")
	if err != nil {
		return fmt.Errorf("failed to get format flag: %w", err)
	}

	stagesStr, err := cmd.Flags().GetString("stages")
	if err != nil {
		return fmt.Errorf("failed to get stages flag: %w", err)
	}

	maxDiagnostics, err := cmd.Root().PersistentFlags().GetInt("max-diagnostics")
	if err != nil {
		return fmt.Errorf("failed to get max-diagnostics flag: %w", err)
	}

	showTimings, err := cmd.Root().PersistentFlags().GetBool("timings")
	if err != nil {
		return fmt.Errorf("failed to get timings flag: %w", err)
	}

	noWarnings, err := cmd.Flags().GetBool("no-warnings")
	if err != nil {
		return fmt.Errorf("failed to get no-warnings flag: %w", err)
	}

	warningsAsErrors, err := cmd.Flags().GetBool("warnings-as-errors")
	if err != nil {
		return fmt.Errorf("failed to get warnings-as-errors flag: %w", err)
	}

	noAlienHints, err := cmd.Flags().GetBool("no-alien-hints")
	if err != nil {
		return fmt.Errorf("failed to get no-alien-hints flag: %w", err)
	}

	if noWarnings && warningsAsErrors {
		return fmt.Errorf("no-warnings and warnings-as-errors flags cannot be used together")
	}

	withNotes, err := cmd.Flags().GetBool("with-notes")
	if err != nil {
		return fmt.Errorf("failed to get with-notes flag: %w", err)
	}

	suggest, err := cmd.Flags().GetBool("suggest")
	if err != nil {
		return fmt.Errorf("failed to get suggest flag: %w", err)
	}

	preview, err := cmd.Flags().GetBool("preview")
	if err != nil {
		return fmt.Errorf("failed to get preview flag: %w", err)
	}

	fullPath, err := cmd.Flags().GetBool("fullpath")
	if err != nil {
		return fmt.Errorf("failed to get fullpath flag: %w", err)
	}

	enableDiskCache, err := cmd.Flags().GetBool("disk-cache")
	if err != nil {
		return fmt.Errorf("failed to get disk-cache flag: %w", err)
	}

	directivesStr, err := cmd.Flags().GetString("directives")
	if err != nil {
		return fmt.Errorf("failed to get directives flag: %w", err)
	}

	directivesFilterStr, err := cmd.Flags().GetString("directives-filter")
	if err != nil {
		return fmt.Errorf("failed to get directives-filter flag: %w", err)
	}

	emitHIR, err := cmd.Flags().GetBool("emit-hir")
	if err != nil {
		return fmt.Errorf("failed to get emit-hir flag: %w", err)
	}
	emitBorrow, err := cmd.Flags().GetBool("emit-borrow")
	if err != nil {
		return fmt.Errorf("failed to get emit-borrow flag: %w", err)
	}
	if emitBorrow {
		emitHIR = true
	}
	emitInstantiations, err := cmd.Flags().GetBool("emit-instantiations")
	if err != nil {
		return fmt.Errorf("failed to get emit-instantiations flag: %w", err)
	}
	emitMono, err := cmd.Flags().GetBool("emit-mono")
	if err != nil {
		return fmt.Errorf("failed to get emit-mono flag: %w", err)
	}
	monoDCE, err := cmd.Flags().GetBool("mono-dce")
	if err != nil {
		return fmt.Errorf("failed to get mono-dce flag: %w", err)
	}
	monoMaxDepth, err := cmd.Flags().GetInt("mono-max-depth")
	if err != nil {
		return fmt.Errorf("failed to get mono-max-depth flag: %w", err)
	}

	// Parse comma-separated filter
	var directiveFilter []string
	for _, ns := range strings.Split(directivesFilterStr, ",") {
		ns = strings.TrimSpace(ns)
		if ns != "" {
			directiveFilter = append(directiveFilter, ns)
		}
	}

	// Конвертируем строку стадии в тип
	var stage driver.DiagnoseStage
	switch stagesStr {
	case "tokenize":
		stage = driver.DiagnoseStageTokenize
	case "syntax":
		stage = driver.DiagnoseStageSyntax
	case "sema":
		stage = driver.DiagnoseStageSema
	case "all":
		stage = driver.DiagnoseStageAll
	default:
		return fmt.Errorf("unknown stages value: %s", stagesStr)
	}
	if emitInstantiations && stage != driver.DiagnoseStageSema && stage != driver.DiagnoseStageAll {
		return fmt.Errorf("--emit-instantiations requires --stages sema|all")
	}
	if emitMono && stage != driver.DiagnoseStageSema && stage != driver.DiagnoseStageAll {
		return fmt.Errorf("--emit-mono requires --stages sema|all")
	}

	// Конвертируем строку режима директив в тип
	var directiveMode parser.DirectiveMode
	switch directivesStr {
	case "off":
		directiveMode = parser.DirectiveModeOff
	case "collect":
		directiveMode = parser.DirectiveModeCollect
	case "gen":
		directiveMode = parser.DirectiveModeGen
	case "run":
		directiveMode = parser.DirectiveModeRun
	default:
		return fmt.Errorf("unknown directives value: %s", directivesStr)
	}

	// Создаём опции диагностики
	printHIR := emitHIR || emitBorrow
	buildHIR := printHIR || emitMono
	buildInstantiations := emitInstantiations || emitMono
	opts := driver.DiagnoseOptions{
		Stage:              stage,
		MaxDiagnostics:     maxDiagnostics,
		IgnoreWarnings:     noWarnings,
		WarningsAsErrors:   warningsAsErrors,
		NoAlienHints:       noAlienHints,
		EnableTimings:      showTimings,
		EnableDiskCache:    enableDiskCache,
		DirectiveMode:      directiveMode,
		DirectiveFilter:    directiveFilter,
		EmitHIR:            buildHIR,
		EmitInstantiations: buildInstantiations,
	}

	st, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat path: %w", err)
	}
	if st.IsDir() && emitMono {
		return fmt.Errorf("--emit-mono is only supported for single files")
	}

	cleanup, err := setupProfiling(cmd)
	if err != nil {
		return err
	}

	var (
		exitCode  int
		resultErr error
	)

	runFile := func() (int, error) {
		result, err := driver.DiagnoseWithOptions(cmd.Context(), filePath, opts)
		if err != nil {
			return 0, fmt.Errorf("diagnosis failed: %w", err)
		}

		exit := 0
		if result.Bag.HasErrors() {
			exit = 1
		}

		pathMode := diagfmt.PathModeAuto
		if fullPath {
			pathMode = diagfmt.PathModeAbsolute
		}
		showFixes := suggest || preview

		var colorFlag string
		colorFlag, err = cmd.Root().PersistentFlags().GetString("color")
		if err != nil {
			return 0, err
		}
		useColor := colorFlag == "on" || (colorFlag == "auto" && isTerminal(os.Stdout))

		switch format {
		case "pretty":
			opts := diagfmt.PrettyOpts{
				Color:       useColor,
				Context:     2,
				PathMode:    pathMode,
				ShowNotes:   withNotes,
				ShowFixes:   showFixes,
				ShowPreview: preview,
			}
			diagfmt.Pretty(os.Stdout, result.Bag, result.FileSet, opts)
		case "short":
			output := diag.FormatGoldenDiagnostics(result.Bag.Items(), result.FileSet, withNotes)
			if output != "" {
				fmt.Fprintln(os.Stdout, output)
			}
		case "json":
			jsonOpts := diagfmt.JSONOpts{
				IncludePositions: true,
				PathMode:         pathMode,
				IncludeNotes:     withNotes,
				IncludeFixes:     showFixes,
				IncludePreviews:  preview,
			}
			var semantics *diagfmt.SemanticsInput
			if result.Symbols != nil && result.Builder != nil && result.FileID != 0 {
				jsonOpts.IncludeSemantics = true
				semantics = &diagfmt.SemanticsInput{
					Builder: result.Builder,
					FileID:  result.FileID,
					Result:  result.Symbols,
				}
			}
			if err = diagfmt.JSON(os.Stdout, result.Bag, result.FileSet, jsonOpts, semantics); err != nil {
				return 0, fmt.Errorf("failed to format diagnostics: %w", err)
			}
		case "sarif":
			meta := diagfmt.SarifRunMeta{
				ToolName:    "surge",
				ToolVersion: "0.1.0",
			}
			diagfmt.Sarif(os.Stdout, result.Bag, result.FileSet, meta)
		default:
			return 0, fmt.Errorf("unknown format: %s", format)
		}

		// Run directive scenarios if requested
		if directiveMode == parser.DirectiveModeRun && result.DirectiveRegistry != nil {
			runner := directive.NewRunner(result.DirectiveRegistry, directive.RunnerConfig{
				Filter: directiveFilter,
				Output: os.Stdout,
			})
			runResult := runner.Run()
			if runResult.Failed > 0 {
				exit = 1
			}
		}

		// Emit HIR (+ optional borrow artefacts) if requested
		if printHIR && result.HIR != nil && result.Sema != nil {
			fmt.Fprintln(os.Stdout, "\n== HIR ==")
			var interner = result.Sema.TypeInterner
			if err := hir.DumpWithOptions(os.Stdout, result.HIR, interner, hir.DumpOptions{EmitBorrow: emitBorrow}); err != nil {
				return 0, fmt.Errorf("failed to dump HIR: %w", err)
			}
		}

		// Emit instantiation map if requested
		if emitInstantiations && result.Instantiations != nil && result.Sema != nil && result.Symbols != nil && result.Builder != nil {
			fmt.Fprintln(os.Stdout, "\n== INSTANTIATIONS ==")
			if err := mono.Dump(os.Stdout, result.Instantiations, result.FileSet, result.Symbols, result.Builder.StringsInterner, result.Sema.TypeInterner, mono.DumpOptions{PathMode: "relative"}); err != nil {
				return 0, fmt.Errorf("failed to dump instantiations: %w", err)
			}
		}

		// Emit monomorphized HIR if requested
		if emitMono && result.HIR != nil && result.Instantiations != nil && result.Sema != nil {
			fmt.Fprintln(os.Stdout, "\n== MONO ==")
			mm, err := mono.MonomorphizeModule(result.HIR, result.Instantiations, result.Sema, mono.Options{
				MaxDepth:  monoMaxDepth,
				EnableDCE: monoDCE,
			})
			if err != nil {
				return 0, fmt.Errorf("failed to monomorphize: %w", err)
			}
			if err := mono.DumpMonoModule(os.Stdout, mm, mono.MonoDumpOptions{}); err != nil {
				return 0, fmt.Errorf("failed to dump mono: %w", err)
			}
		}

		return exit, nil
	}

	runDir := func() (int, error) {
		jobs, err := cmd.Flags().GetInt("jobs")
		if err != nil {
			return 0, fmt.Errorf("failed to get jobs flag: %w", err)
		}

		fs, results, err := driver.DiagnoseDirWithOptions(cmd.Context(), filePath, opts, jobs)
		if err != nil {
			return 0, fmt.Errorf("diagnosis failed: %w", err)
		}

		exit := 0
		for _, r := range results {
			if r.Bag.HasErrors() {
				exit = 1
				break
			}
		}

		colorFlag, err := cmd.Root().PersistentFlags().GetString("color")
		if err != nil {
			return 0, err
		}
		useColor := colorFlag == "on" || (colorFlag == "auto" && isTerminal(os.Stdout))
		pathMode := diagfmt.PathModeAuto
		if fullPath {
			pathMode = diagfmt.PathModeAbsolute
		}
		showFixes := suggest || preview
		prettyOpts := diagfmt.PrettyOpts{
			Color:       useColor,
			Context:     2,
			PathMode:    pathMode,
			ShowNotes:   withNotes,
			ShowFixes:   showFixes,
			ShowPreview: preview,
		}
		jsonOpts := diagfmt.JSONOpts{
			IncludePositions: true,
			PathMode:         pathMode,
			IncludeNotes:     withNotes,
			IncludeFixes:     showFixes,
			IncludePreviews:  preview,
		}
		meta := diagfmt.SarifRunMeta{
			ToolName:    "surge",
			ToolVersion: "0.1.0",
		}

		switch format {
		case "short":
			allDiagnostics := make([]*diag.Diagnostic, 0, len(results))
			for _, r := range results {
				allDiagnostics = append(allDiagnostics, r.Bag.Items()...)
			}
			output := diag.FormatGoldenDiagnostics(allDiagnostics, fs, withNotes)
			if output != "" {
				fmt.Fprintln(os.Stdout, output)
			}
		case "pretty":
			for idx, r := range results {
				if idx > 0 {
					fmt.Fprintln(os.Stdout)
				}

				displayPath := r.Path
				if r.FileID != 0 {
					file := fs.Get(r.FileID)
					mode := "auto"
					if fullPath {
						mode = "absolute"
					}
					displayPath = file.FormatPath(mode, fs.BaseDir())
				} else if fullPath {
					if abs, err := source.AbsolutePath(displayPath); err == nil {
						displayPath = abs
					}
				}

				fmt.Fprintf(os.Stdout, "== %s ==\n", displayPath)
				diagfmt.Pretty(os.Stdout, r.Bag, fs, prettyOpts)
			}
		case "json":
			output := make(map[string]diagfmt.DiagnosticsOutput, len(results))
			for _, r := range results {
				displayPath := r.Path
				if r.FileID != 0 {
					file := fs.Get(r.FileID)
					mode := "auto"
					if fullPath {
						mode = "absolute"
					}
					displayPath = file.FormatPath(mode, fs.BaseDir())
				} else if fullPath {
					if abs, err := source.AbsolutePath(displayPath); err == nil {
						displayPath = abs
					}
				}
				optsForFile := jsonOpts
				var semantics *diagfmt.SemanticsInput
				if r.Symbols != nil && r.Builder != nil && r.ASTFile != 0 {
					optsForFile.IncludeSemantics = true
					semantics = &diagfmt.SemanticsInput{
						Builder: r.Builder,
						FileID:  r.ASTFile,
						Result:  r.Symbols,
					}
				}
				data, buildErr := diagfmt.BuildDiagnosticsOutput(r.Bag, fs, optsForFile, semantics)
				if buildErr != nil {
					return 0, fmt.Errorf("failed to build diagnostics output: %w", buildErr)
				}
				output[displayPath] = data
			}
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(output); err != nil {
				return 0, fmt.Errorf("failed to encode diagnostics output: %w", err)
			}
		case "sarif":
			for _, r := range results {
				diagfmt.Sarif(os.Stdout, r.Bag, fs, meta)
			}
		default:
			return 0, fmt.Errorf("unknown format: %s", format)
		}

		return exit, nil
	}

	if !st.IsDir() {
		exitCode, resultErr = runFile()
	} else {
		exitCode, resultErr = runDir()
	}

	// Always cleanup profiler
	cleanup()

	if resultErr != nil {
		// Cleanup tracer explicitly because PersistentPostRun is not called on error
		if tracer := trace.FromContext(cmd.Context()); tracer != nil && tracer != trace.Nop {
			_ = tracer.Flush()
			_ = tracer.Close()
		}
		return resultErr
	}
	if exitCode != 0 {
		// Cleanup tracer explicitly because PersistentPostRun is not called on error
		if tracer := trace.FromContext(cmd.Context()); tracer != nil && tracer != trace.Nop {
			_ = tracer.Flush()
			_ = tracer.Close()
		}
		// Suppress cobra usage output on diagnostic errors
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return fmt.Errorf("") // Silent error - diagnostics already printed
	}
	return nil
}
