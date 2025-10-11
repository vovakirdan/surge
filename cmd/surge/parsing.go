package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
	"surge/internal/diagfmt"
	"surge/internal/driver"
)

var parseCmd = &cobra.Command{
	Use:   "parse [flags] <file.sg|directory>",
	Short: "Parse a surge source file or directory and output AST",
	Long:  `Parse analyzes a surge source file or all *.sg files in a directory and outputs their Abstract Syntax Trees`,
	Args:  cobra.ExactArgs(1),
	RunE:  runParse,
}

func init() {
	parseCmd.Flags().String("format", "pretty", "output format (pretty|json)")
	parseCmd.Flags().Int("jobs", 0, "max parallel workers for directory processing (0=auto)")
}

func runParse(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	format, err := cmd.Flags().GetString("format")
	if err != nil {
		return fmt.Errorf("failed to get format flag: %w", err)
	}

	maxDiagnostics, err := cmd.Root().PersistentFlags().GetInt("max-diagnostics")
	if err != nil {
		return fmt.Errorf("failed to get max-diagnostics flag: %w", err)
	}

	quiet, err := cmd.Root().PersistentFlags().GetBool("quiet")
	if err != nil {
		return fmt.Errorf("failed to get quiet flag: %w", err)
	}

	// Проверяем, файл это или директория
	st, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat path: %w", err)
	}

	if !st.IsDir() {
		// Парсинг одного файла
		result, err := driver.Parse(filePath, maxDiagnostics)
		if err != nil {
			return fmt.Errorf("parsing failed: %w", err)
		}

		if result.Bag.HasErrors() || result.Bag.HasWarnings() {
			colorFlag, _ := cmd.Root().PersistentFlags().GetString("color")
			useColor := colorFlag == "on" || (colorFlag == "auto" && isTerminal(os.Stderr))
			opts := diagfmt.PrettyOpts{
				Color:   useColor,
				Context: 2,
			}
			diagfmt.Pretty(os.Stderr, result.Bag, result.FileSet, opts)
		}

		switch format {
		case "pretty":
			return diagfmt.FormatASTPretty(os.Stdout, result.Builder, result.FileID, result.FileSet)
		case "json":
			return diagfmt.FormatASTJSON(os.Stdout, result.Builder, result.FileID)
		default:
			return fmt.Errorf("unknown format: %s", format)
		}
	}

	// Парсинг директории
	jobs, err := cmd.Flags().GetInt("jobs")
	if err != nil {
		return fmt.Errorf("failed to get jobs flag: %w", err)
	}

	if jobs <= 0 {
		jobs = runtime.GOMAXPROCS(0)
	}

	fs, _, results, err := driver.ParseDir(cmd.Context(), filePath, maxDiagnostics, jobs)
	if err != nil {
		return fmt.Errorf("parsing failed: %w", err)
	}

	// Обрабатываем результаты (они уже отсортированы)
	colorFlag, _ := cmd.Root().PersistentFlags().GetString("color")
	useColor := colorFlag == "on" || (colorFlag == "auto" && isTerminal(os.Stderr))
	prettyOpts := diagfmt.PrettyOpts{
		Color:   useColor,
		Context: 2,
	}

	for _, r := range results {
		// Выводим диагностику в stderr, если есть
		if r.Bag.HasErrors() || r.Bag.HasWarnings() {
			diagfmt.Pretty(os.Stderr, r.Bag, fs, prettyOpts)
		}

		// Выводим заголовок файла, если не quiet
		if !quiet {
			fmt.Fprintf(os.Stdout, "== %s ==\n", r.Path)
		}

		// Выводим AST в выбранном формате
		switch format {
		case "pretty":
			if err := diagfmt.FormatASTPretty(os.Stdout, r.Builder, r.FileID, fs); err != nil {
				return err
			}
		case "json":
			if err := diagfmt.FormatASTJSON(os.Stdout, r.Builder, r.FileID); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown format: %s", format)
		}

		// Добавляем пустую строку между файлами для читаемости
		if !quiet && format == "pretty" {
			fmt.Fprintln(os.Stdout)
		}
	}

	return nil
}