package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"surge/internal/diagfmt"
	"surge/internal/driver"
)

var tokenizeCmd = &cobra.Command{
	Use:   "tokenize [flags] <file.sg|directory>",
	Short: "Tokenize a surge source file or directory",
	Long:  `Tokenize breaks down a surge source file or all *.sg files in a directory into their constituent tokens`,
	Args:  cobra.ExactArgs(1),
	RunE:  runTokenize,
}

func init() {
	tokenizeCmd.Flags().String("format", "pretty", "output format (pretty|json)")
	tokenizeCmd.Flags().Int("jobs", 0, "max parallel workers for directory processing (0=auto)")
}

func runTokenize(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	// Получаем флаги
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
		// Токенизация одного файла
		var result *driver.TokenizeResult
		result, err = driver.Tokenize(filePath, maxDiagnostics)
		if err != nil {
			return fmt.Errorf("tokenization failed: %w", err)
		}

		// Выводим диагностику в stderr, если есть
		if result.Bag.HasErrors() || result.Bag.HasWarnings() {
			var colorFlag string
			colorFlag, err = cmd.Root().PersistentFlags().GetString("color")
			if err != nil {
				return err
			}
			useColor := colorFlag == "on" || (colorFlag == "auto" && isTerminal(os.Stderr))
			opts := diagfmt.PrettyOpts{
				Color:   useColor,
				Context: 2,
			}
			diagfmt.Pretty(os.Stderr, result.Bag, result.FileSet, opts)
		}

		// Выводим токены в выбранном формате
		switch format {
		case "pretty":
			return diagfmt.FormatTokensPretty(os.Stdout, result.Tokens, result.FileSet)
		case "json":
			return diagfmt.FormatTokensJSON(os.Stdout, result.Tokens)
		default:
			return fmt.Errorf("unknown format: %s", format)
		}
	}

	// Токенизация директории
	jobs, err := cmd.Flags().GetInt("jobs")
	if err != nil {
		return fmt.Errorf("failed to get jobs flag: %w", err)
	}

	if jobs <= 0 {
		jobs = runtime.GOMAXPROCS(0)
	}

	fs, results, err := driver.TokenizeDir(cmd.Context(), filePath, maxDiagnostics, jobs)
	if err != nil {
		return fmt.Errorf("tokenization failed: %w", err)
	}

	// Обрабатываем диагностику (они уже отсортированы)
	var colorFlag string
	colorFlag, err = cmd.Root().PersistentFlags().GetString("color")
	if err != nil {
		return err
	}
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
	}

	switch format {
	case "pretty":
		for idx, r := range results {
			if !quiet {
				displayPath := r.Path
				if r.FileID != 0 {
					file := fs.Get(r.FileID)
					displayPath = file.FormatPath("auto", fs.BaseDir())
				}
				_, printErr := fmt.Fprintf(os.Stdout, "== %s ==\n", displayPath)
				if printErr != nil {
					return printErr
				}
			}

			if err := diagfmt.FormatTokensPretty(os.Stdout, r.Tokens, fs); err != nil {
				return err
			}

			if !quiet && idx < len(results)-1 {
				_, printErr := fmt.Fprintln(os.Stdout)
				if printErr != nil {
					return printErr
				}
			}
		}
	case "json":
		output := make(map[string][]diagfmt.TokenOutput, len(results))
		for _, r := range results {
			displayPath := r.Path
			if r.FileID != 0 {
				file := fs.Get(r.FileID)
				displayPath = file.FormatPath("auto", fs.BaseDir())
			}
			output[displayPath] = diagfmt.TokenOutputsJSON(r.Tokens)
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(output); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown format: %s", format)
	}

	return nil
}
