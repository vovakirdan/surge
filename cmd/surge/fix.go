package main

// todo: tui mode
// флаг --interactive/--tui включает интерактивный режим замен

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/fix"
)

var fixCmd = &cobra.Command{
	Use:   "fix [flags] <file.sg|directory>",
	Short: "Apply available fixes to a source file or directory",
	Long:  "Run diagnostics, surface available fixes, and apply them according to the chosen strategy.",
	Args:  cobra.ExactArgs(1),
	RunE:  runFix,
}

func init() {
	fixCmd.Flags().Bool("all", false, "apply all safe fixes")
	fixCmd.Flags().Bool("once", false, "apply the first available fix (default)")
	fixCmd.Flags().String("id", "", "apply fix with a specific identifier")
}

func runFix(cmd *cobra.Command, args []string) error {
	targetPath := args[0]

	applyAll, err := cmd.Flags().GetBool("all")
	if err != nil {
		return err
	}
	applyOnceFlag, err := cmd.Flags().GetBool("once")
	if err != nil {
		return err
	}
	targetID, err := cmd.Flags().GetString("id")
	if err != nil {
		return err
	}

	if targetID != "" && (applyAll || applyOnceFlag) {
		return fmt.Errorf("--id cannot be combined with --all or --once")
	}
	if applyAll && applyOnceFlag {
		return fmt.Errorf("--all and --once are mutually exclusive")
	}

	mode := fix.ApplyModeOnce
	if targetID != "" {
		mode = fix.ApplyModeID
	} else if applyAll {
		mode = fix.ApplyModeAll
	}
	opts := fix.ApplyOptions{
		Mode:     mode,
		TargetID: targetID,
	}

	maxDiagnostics, err := cmd.Root().PersistentFlags().GetInt("max-diagnostics")
	if err != nil {
		return err
	}

	driverOpts := driver.DiagnoseOptions{
		Stage:          driver.DiagnoseStageSyntax,
		MaxDiagnostics: maxDiagnostics,
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return fmt.Errorf("fix: %w", err)
	}

	// если это директория, но передан id, то ошибка
	// так как id уникален только для одного файла
	if info.IsDir() && targetID != "" {
		return fmt.Errorf("fix: id can only be used with a single file")
	}

	if !info.IsDir() {
		return runFixFile(targetPath, driverOpts, opts)
	}
	return runFixDir(cmd, targetPath, driverOpts, opts)
}

func runFixFile(path string, driverOpts driver.DiagnoseOptions, opts fix.ApplyOptions) error {
	result, err := driver.DiagnoseWithOptions(path, driverOpts)
	if err != nil {
		return fmt.Errorf("fix: diagnose failed: %w", err)
	}
	var diagnostics []diag.Diagnostic
	if result.Bag != nil {
		result.Bag.Sort()
		diagnostics = append(diagnostics, result.Bag.Items()...)
	}
	res, applyErr := fix.Apply(result.FileSet, diagnostics, opts)
	return handleApplyResult(res, applyErr)
}

func runFixDir(cmd *cobra.Command, path string, driverOpts driver.DiagnoseOptions, opts fix.ApplyOptions) error {
	fs, results, err := driver.DiagnoseDirWithOptions(cmd.Context(), path, driverOpts, 0)
	if err != nil {
		return fmt.Errorf("fix: diagnose dir failed: %w", err)
	}

	allDiagnostics := make([]diag.Diagnostic, 0)
	for _, r := range results {
		if r.Bag == nil {
			continue
		}
		r.Bag.Sort()
		allDiagnostics = append(allDiagnostics, r.Bag.Items()...)
	}

	res, applyErr := fix.Apply(fs, allDiagnostics, opts)
	return handleApplyResult(res, applyErr)
}

func handleApplyResult(res *fix.ApplyResult, applyErr error) error {
	if res == nil {
		return applyErr
	}

	if len(res.Applied) > 0 {
		fmt.Fprintf(os.Stdout, "Applied %d fix(es):\n", len(res.Applied))
		for _, item := range res.Applied {
			location := item.PrimaryPath
			if location == "" {
				location = "(unknown location)"
			}
			fmt.Fprintf(
				os.Stdout,
				"  %s [%s] — %s (%d edits, %s)\n",
				item.Title,
				item.ID,
				location,
				item.EditCount,
				item.Applicability.String(),
			)
		}
	}

	if len(res.FileChanges) > 0 {
		fmt.Fprintln(os.Stdout, "Updated files:")
		for _, change := range res.FileChanges {
			fmt.Fprintf(os.Stdout, "  %s (%d edits)\n", change.Path, change.EditCount)
		}
	}

	if len(res.Skipped) > 0 {
		fmt.Fprintln(os.Stdout, "Skipped fixes:")
		for _, skip := range res.Skipped {
			id := skip.ID
			if id == "" {
				id = "(unnamed)"
			}
			if skip.Title != "" {
				fmt.Fprintf(os.Stdout, "  %s [%s]: %s\n", skip.Title, id, skip.Reason)
			} else {
				fmt.Fprintf(os.Stdout, "  [%s]: %s\n", id, skip.Reason)
			}
		}
	}

	if applyErr != nil {
		if errors.Is(applyErr, fix.ErrNoFixes) && len(res.Applied) == 0 {
			fmt.Fprintln(os.Stdout, "No applicable fixes found.")
			return nil
		}
		return applyErr
	}

	if len(res.Applied) == 0 {
		fmt.Fprintln(os.Stdout, "No fixes applied.")
	}
	return nil
}
