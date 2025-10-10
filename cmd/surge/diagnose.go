package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"surge/internal/diagfmt"
	"surge/internal/driver"
)

var diagCmd = &cobra.Command{
	Use:   "diag [flags] file.sg",
	Short: "Run diagnostics on a surge source file",
	Long:  `Run diagnostics to find syntax and semantic errors in surge source files`,
	Args:  cobra.ExactArgs(1),
	RunE:  runDiagnose,
}

func init() {
	diagCmd.Flags().String("format", "pretty", "output format (pretty|json|sarif)")
	diagCmd.Flags().String("stages", "syntax", "diagnostic stages to run (tokenize|syntax|sema|all)")
}

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

	// Выполняем диагностику
	result, err := driver.Diagnose(filePath, stage, maxDiagnostics)
	if err != nil {
		return fmt.Errorf("diagnosis failed: %w", err)
	}

	// Определяем код выхода
	exitCode := 0
	if result.Bag.HasErrors() {
		exitCode = 1
	}

	// Выводим диагностику в выбранном формате
	switch format {
	case "pretty":
		colorFlag, _ := cmd.Root().PersistentFlags().GetString("color")
		useColor := colorFlag == "on" || (colorFlag == "auto" && isTerminal(os.Stdout))
		opts := diagfmt.PrettyOpts{
			Color:   useColor,
			Context: 2,
		}
		diagfmt.Pretty(os.Stdout, result.Bag, result.FileSet, opts)
	case "json":
		jsonOpts := diagfmt.JSONOpts{
			IncludePositions: true,
			PathMode:         diagfmt.PathModeAuto,
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

	// Устанавливаем код выхода
	if exitCode != 0 {
		os.Exit(exitCode)
	}

	return nil
}
