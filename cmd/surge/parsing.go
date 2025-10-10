package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"surge/internal/diagfmt"
	"surge/internal/driver"
)

var parseCmd = &cobra.Command{
	Use:   "parse [flags] file.sg",
	Short: "Parse a surge source file and output AST",
	Long:  `Parse analyzes a surge source file and outputs its Abstract Syntax Tree`,
	Args:  cobra.ExactArgs(1),
	RunE:  runParse,
}

func init() {
	parseCmd.Flags().String("format", "pretty", "output format (pretty|json)")
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