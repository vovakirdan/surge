package main

import (
    "os"

    "github.com/spf13/cobra"
    "golang.org/x/term"
    "surge/internal/version"
)

var rootCmd = &cobra.Command{
    Use:   "surge",
    Short: "Surge language compiler and toolchain",
    Long:  `Surge is a programming language compiler with diagnostic tools`,
}

// main configures the root CLI command (sets the version, registers subcommands, and defines persistent flags) and then executes it, exiting with status 1 if execution fails.
func main() {
    // Устанавливаем версию для автоматического флага --version
    rootCmd.Version = version.Version

    // Добавляем команды
    rootCmd.AddCommand(tokenizeCmd)
    rootCmd.AddCommand(parseCmd)
    rootCmd.AddCommand(diagCmd)
    rootCmd.AddCommand(fixCmd)
    rootCmd.AddCommand(versionCmd)
    rootCmd.AddCommand(initCmd)
    rootCmd.AddCommand(buildCmd)

	// Глобальные флаги
	rootCmd.PersistentFlags().String("color", "auto", "colorize output (auto|on|off)")
	rootCmd.PersistentFlags().Bool("quiet", false, "suppress non-essential output")
	rootCmd.PersistentFlags().Bool("timings", false, "show timing information")
	rootCmd.PersistentFlags().Int("max-diagnostics", 100, "maximum number of diagnostics to show")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// isTerminal проверяет, является ли файл терминалом
func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}