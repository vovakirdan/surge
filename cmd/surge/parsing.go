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

var parseCmd = &cobra.Command{
	Use:   "parse [flags] <file.sg|directory>",
	Short: "Parse a surge source file or directory and output AST",
	Long:  `Parse analyzes a surge source file or all *.sg files in a directory and outputs their Abstract Syntax Trees`,
	Args:  cobra.ExactArgs(1),
	RunE:  runParse,
}

func init() {
	parseCmd.Flags().String("format", "pretty", "output format (pretty|json|tree)")
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
		var result *driver.ParseResult
		result, err = driver.Parse(filePath, maxDiagnostics)
		if err != nil {
			return fmt.Errorf("parsing failed: %w", err)
		}

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

		switch format {
		case "pretty":
			return diagfmt.FormatASTPretty(os.Stdout, result.Builder, result.FileID, result.FileSet)
		case "json":
			return diagfmt.FormatASTJSON(os.Stdout, result.Builder, result.FileID)
		case "tree":
			return diagfmt.FormatASTTree(os.Stdout, result.Builder, result.FileID, result.FileSet)
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
		if r.Bag.HasErrors() || r.Bag.HasWarnings() {
			diagfmt.Pretty(os.Stderr, r.Bag, fs, prettyOpts)
		}
	}

	switch format {
	case "pretty":
		for idx, r := range results {
			displayPath := r.Path
			if r.FileID != 0 && r.Builder != nil {
				astFile := r.Builder.Files.Get(r.FileID)
				sourceFileID := astFile.Span.File
				file := fs.Get(sourceFileID)
				displayPath = file.FormatPath("auto", fs.BaseDir())
			}

			if !quiet {
				_, printErr := fmt.Fprintf(os.Stdout, "== %s ==\n", displayPath)
				if printErr != nil {
					return printErr
				}
			}

			if r.Builder != nil {
				if err := diagfmt.FormatASTPretty(os.Stdout, r.Builder, r.FileID, fs); err != nil {
					return err
				}
			}

			if !quiet && idx < len(results)-1 {
				_, printErr := fmt.Fprintln(os.Stdout)
				if printErr != nil {
					return printErr
				}
			}
		}
	case "json":
		output := make(map[string]*diagfmt.ASTNodeOutput, len(results))
		for _, r := range results {
			displayPath := r.Path
			if r.FileID != 0 && r.Builder != nil {
				astFile := r.Builder.Files.Get(r.FileID)
				sourceFileID := astFile.Span.File
				file := fs.Get(sourceFileID)
				displayPath = file.FormatPath("auto", fs.BaseDir())
			}

			if r.Builder == nil {
				output[displayPath] = nil
				continue
			}

			node, err := diagfmt.BuildASTJSON(r.Builder, r.FileID)
			if err != nil {
				return err
			}
			// Ensure distinct pointer per iteration
			nodeCopy := node
			output[displayPath] = &nodeCopy
		}

		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(output); err != nil {
			return err
		}
	case "tree":
		for idx, r := range results {
			displayPath := r.Path
			if r.FileID != 0 && r.Builder != nil {
				astFile := r.Builder.Files.Get(r.FileID)
				sourceFileID := astFile.Span.File
				file := fs.Get(sourceFileID)
				displayPath = file.FormatPath("auto", fs.BaseDir())
			}

			if !quiet {
				_, printErr := fmt.Fprintf(os.Stdout, "== %s ==\n", displayPath)
				if printErr != nil {
					return printErr
				}
			}

			if r.Builder != nil {
				if err := diagfmt.FormatASTTree(os.Stdout, r.Builder, r.FileID, fs); err != nil {
					return err
				}
			}

			if !quiet && idx < len(results)-1 {
				_, printErr := fmt.Fprintln(os.Stdout)
				if printErr != nil {
					return printErr
				}
			}
		}
	default:
		return fmt.Errorf("unknown format: %s", format)
	}

	return nil
}
