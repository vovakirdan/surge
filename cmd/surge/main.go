package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"golang.org/x/term"

	"surge/internal/version"
)

var rootCmd = &cobra.Command{
	Use:   "surge",
	Short: "Surge language compiler and toolchain",
	Long:  `Surge is a programming language compiler with diagnostic tools`,
}

var (
	timeoutCancel   context.CancelFunc
	timeoutDuration time.Duration
	traceCleanup    func()
)

// main configures the root CLI command (sets the version, registers subcommands, and defines persistent flags) and then executes it, exiting with status 1 if execution fails.
func main() {
	// Устанавливаем версию для автоматического флага --version
	rootCmd.Version = version.VersionString()
	rootCmd.PersistentPreRunE = applyTimeout
	rootCmd.PersistentPostRun = cleanupTimeout

	// Добавляем команды
	rootCmd.AddCommand(tokenizeCmd)
	rootCmd.AddCommand(parseCmd)
	rootCmd.AddCommand(diagCmd)
	rootCmd.AddCommand(fmtCmd)
	rootCmd.AddCommand(fixCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(buildCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(philosophyCmd)

	// Глобальные флаги
	rootCmd.PersistentFlags().String("color", "auto", "colorize output (auto|on|off)")
	rootCmd.PersistentFlags().Bool("quiet", false, "suppress non-essential output")
	rootCmd.PersistentFlags().Bool("timings", false, "show timing information")
	rootCmd.PersistentFlags().Int("max-diagnostics", 100, "maximum number of diagnostics to show")
	rootCmd.PersistentFlags().String("cpu-profile", "", "write CPU profile to file")
	rootCmd.PersistentFlags().String("mem-profile", "", "write heap profile to file")
	rootCmd.PersistentFlags().String("runtime-trace", "", "write Go runtime trace to file")
	rootCmd.PersistentFlags().Int("timeout", 30, "command timeout in seconds")

	// Tracing flags
	rootCmd.PersistentFlags().String("trace", "", "trace output file (- for stderr, empty to disable)")
	rootCmd.PersistentFlags().String("trace-level", "off", "trace level (off|error|phase|detail|debug)")
	rootCmd.PersistentFlags().String("trace-mode", "ring", "storage mode (stream|ring|both)")
	rootCmd.PersistentFlags().String("trace-format", "auto", "output format (auto|text|ndjson|chrome) - auto detects from file extension")
	rootCmd.PersistentFlags().Int("trace-ring-size", 4096, "ring buffer capacity for trace events")
	rootCmd.PersistentFlags().Duration("trace-heartbeat", 0, "heartbeat interval (0 to disable, e.g. 1s)")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// isTerminal проверяет, является ли файл терминалом
func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func applyTimeout(cmd *cobra.Command, _ []string) error {
	secs, err := cmd.Root().PersistentFlags().GetInt("timeout")
	if err != nil {
		return fmt.Errorf("failed to read timeout flag: %w", err)
	}
	if secs <= 0 {
		return fmt.Errorf("timeout must be greater than zero")
	}

	timeoutDuration = time.Duration(secs) * time.Second
	ctx, cancel := context.WithTimeout(cmd.Context(), timeoutDuration)
	timeoutCancel = cancel

	cmd.SetContext(ctx)
	cmd.Root().SetContext(ctx)

	go func() {
		<-ctx.Done()
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Fprintf(os.Stderr, "surge: command timed out after %s\n", timeoutDuration)
			os.Exit(1)
		}
	}()

	// Setup tracing
	cleanup, err := setupTracing(cmd)
	if err != nil {
		return fmt.Errorf("failed to setup tracing: %w", err)
	}
	traceCleanup = cleanup

	return nil
}

func cleanupTimeout(*cobra.Command, []string) {
	if timeoutCancel != nil {
		timeoutCancel()
		timeoutCancel = nil
	}
	if traceCleanup != nil {
		traceCleanup()
		traceCleanup = nil
	}
}
