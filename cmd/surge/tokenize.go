package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"surge/internal/diagfmt"
	"surge/internal/driver"
)

var tokenizeCmd = &cobra.Command{
	Use:   "tokenize [flags] file.sg",
	Short: "Tokenize a surge source file",
	Long:  `Tokenize breaks down a surge source file into its constituent tokens`,
	Args:  cobra.ExactArgs(1),
	RunE:  runTokenize,
}

func init() {
	tokenizeCmd.Flags().String("format", "pretty", "output format (pretty|json)")
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

	// Выполняем токенизацию
	result, err := driver.Tokenize(filePath, maxDiagnostics)
	if err != nil {
		return fmt.Errorf("tokenization failed: %w", err)
	}

	// Выводим диагностику в stderr, если есть
	if result.Bag.HasErrors() || result.Bag.HasWarnings() {
		colorFlag, _ := cmd.Root().PersistentFlags().GetString("color")
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
