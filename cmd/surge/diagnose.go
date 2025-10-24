package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"surge/internal/diagfmt"
	"surge/internal/driver"
	"surge/internal/source"
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
	diagCmd.Flags().String("format", "pretty", "output format (pretty|json|sarif)")
	diagCmd.Flags().String("stages", "syntax", "diagnostic stages to run (tokenize|syntax|sema|all)")
	diagCmd.Flags().Bool("no-warnings", false, "ignore warnings in diagnostics")
	diagCmd.Flags().Bool("warnings-as-errors", false, "treat warnings as errors")
	diagCmd.Flags().Int("jobs", 0, "max parallel workers for directory processing (0=auto)")
	diagCmd.Flags().Bool("with-notes", false, "include diagnostic notes in output")
	diagCmd.Flags().Bool("suggest", false, "include fix suggestions in output")
	diagCmd.Flags().Bool("fullpath", false, "emit absolute file paths in output")
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

	noWarnings, err := cmd.Flags().GetBool("no-warnings")
	if err != nil {
		return fmt.Errorf("failed to get no-warnings flag: %w", err)
	}

	warningsAsErrors, err := cmd.Flags().GetBool("warnings-as-errors")
	if err != nil {
		return fmt.Errorf("failed to get warnings-as-errors flag: %w", err)
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

	fullPath, err := cmd.Flags().GetBool("fullpath")
	if err != nil {
		return fmt.Errorf("failed to get fullpath flag: %w", err)
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

	// Создаём опции диагностики
	opts := driver.DiagnoseOptions{
		Stage:            stage,
		MaxDiagnostics:   maxDiagnostics,
		IgnoreWarnings:   noWarnings,
		WarningsAsErrors: warningsAsErrors,
	}

	st, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat path: %w", err)
	}

	if !st.IsDir() {
		// Выполняем диагностику одного файла
		result, err := driver.DiagnoseWithOptions(filePath, opts)
		if err != nil {
			return fmt.Errorf("diagnosis failed: %w", err)
		}

		exitCode := 0
		if result.Bag.HasErrors() {
			exitCode = 1
		}

		pathMode := diagfmt.PathModeAuto
		if fullPath {
			pathMode = diagfmt.PathModeAbsolute
		}

		switch format {
		case "pretty":
			colorFlag, _ := cmd.Root().PersistentFlags().GetString("color")
			useColor := colorFlag == "on" || (colorFlag == "auto" && isTerminal(os.Stdout))
			opts := diagfmt.PrettyOpts{
				Color:     useColor,
				Context:   2,
				PathMode:  pathMode,
				ShowNotes: withNotes,
				ShowFixes: suggest,
			}
			diagfmt.Pretty(os.Stdout, result.Bag, result.FileSet, opts)
		case "json":
			jsonOpts := diagfmt.JSONOpts{
				IncludePositions: true,
				PathMode:         pathMode,
				IncludeNotes:     withNotes,
				IncludeFixes:     suggest,
			}
			err = diagfmt.JSON(os.Stdout, result.Bag, result.FileSet, jsonOpts)
		case "sarif":
			meta := diagfmt.SarifRunMeta{
				ToolName:    "surge",
				ToolVersion: "0.1.0",
			}
			diagfmt.Sarif(os.Stdout, result.Bag, result.FileSet, meta)
		default:
			return fmt.Errorf("unknown format: %s", format)
		}

		if err != nil {
			return fmt.Errorf("failed to format diagnostics: %w", err)
		}

		if exitCode != 0 {
			os.Exit(exitCode)
		}

		return nil
	}

	// Диагностика директории
	jobs, err := cmd.Flags().GetInt("jobs")
	if err != nil {
		return fmt.Errorf("failed to get jobs flag: %w", err)
	}

	fs, results, err := driver.DiagnoseDirWithOptions(cmd.Context(), filePath, opts, jobs)
	if err != nil {
		return fmt.Errorf("diagnosis failed: %w", err)
	}

	exitCode := 0
	for _, r := range results {
		if r.Bag.HasErrors() {
			exitCode = 1
			break
		}
	}

	colorFlag, _ := cmd.Root().PersistentFlags().GetString("color")
	useColor := colorFlag == "on" || (colorFlag == "auto" && isTerminal(os.Stdout))
	pathMode := diagfmt.PathModeAuto
	if fullPath {
		pathMode = diagfmt.PathModeAbsolute
	}
	prettyOpts := diagfmt.PrettyOpts{
		Color:     useColor,
		Context:   2,
		PathMode:  pathMode,
		ShowNotes: withNotes,
		ShowFixes: suggest,
	}
	jsonOpts := diagfmt.JSONOpts{
		IncludePositions: true,
		PathMode:         pathMode,
		IncludeNotes:     withNotes,
		IncludeFixes:     suggest,
	}
	meta := diagfmt.SarifRunMeta{
		ToolName:    "surge",
		ToolVersion: "0.1.0",
	}

	switch format {
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
			data, err := diagfmt.BuildDiagnosticsOutput(r.Bag, fs, jsonOpts)
			if err != nil {
				return fmt.Errorf("failed to build diagnostics output: %w", err)
			}
			output[displayPath] = data
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(output); err != nil {
			return fmt.Errorf("failed to encode diagnostics output: %w", err)
		}
	case "sarif":
		for _, r := range results {
			diagfmt.Sarif(os.Stdout, r.Bag, fs, meta)
		}
	default:
		return fmt.Errorf("unknown format: %s", format)
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}

	return nil
}
