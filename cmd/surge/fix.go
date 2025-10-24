package main

// todo: tui mode
// флаг --interactive/--tui включает интерактивный режим замен

import (
	"encoding/json"
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
	fixCmd.Flags().Bool("preview", false, "preview changes without modifying files")
	fixCmd.Flags().String("format", "pretty", "preview output format (pretty|json)")
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
	preview, err := cmd.Flags().GetBool("preview")
	if err != nil {
		return err
	}
	previewFormat, err := cmd.Flags().GetString("format")
	if err != nil {
		return err
	}
	if !preview && previewFormat != "pretty" {
		return fmt.Errorf("--format is only supported in preview mode")
	}
	if preview && previewFormat != "pretty" && previewFormat != "json" {
		return fmt.Errorf("unknown preview format: %s", previewFormat)
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
		Preview:  preview,
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
		return runFixFile(targetPath, driverOpts, opts, preview, previewFormat)
	}
	return runFixDir(cmd, targetPath, driverOpts, opts, preview, previewFormat)
}

func runFixFile(path string, driverOpts driver.DiagnoseOptions, opts fix.ApplyOptions, preview bool, format string) error {
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
	return handleApplyResult(res, applyErr, preview, format)
}

func runFixDir(cmd *cobra.Command, path string, driverOpts driver.DiagnoseOptions, opts fix.ApplyOptions, preview bool, format string) error {
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
	return handleApplyResult(res, applyErr, preview, format)
}

func handleApplyResult(res *fix.ApplyResult, applyErr error, preview bool, format string) error {
	if res == nil {
		return applyErr
	}

	if preview {
		return handlePreviewResult(res, applyErr, format)
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

func handlePreviewResult(res *fix.ApplyResult, applyErr error, format string) error {
	switch format {
	case "json":
		return handlePreviewJSON(res, applyErr)
	default:
		return handlePreviewPretty(res, applyErr)
	}
}

func handlePreviewPretty(res *fix.ApplyResult, applyErr error) error {
	if errors.Is(applyErr, fix.ErrNoFixes) && len(res.Applied) == 0 {
		fmt.Fprintln(os.Stdout, "No applicable fixes found.")
		return nil
	}
	if applyErr != nil {
		return applyErr
	}

	if len(res.Applied) == 0 {
		fmt.Fprintln(os.Stdout, "No fixes selected for preview.")
		fmt.Fprintln(os.Stdout, "No files were modified (preview mode).")
	} else {
		fmt.Fprintf(os.Stdout, "Previewing %d fix(es):\n", len(res.Applied))
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
			if len(item.Previews) == 0 {
				fmt.Fprintln(os.Stdout, "    (no preview available)")
				continue
			}
			for _, h := range item.Previews {
				fmt.Fprintf(
					os.Stdout,
					"    @@ %s:%d-%d ⇒ %d-%d @@\n",
					h.Path,
					h.Before.StartLine,
					h.Before.EndLine,
					h.After.StartLine,
					h.After.EndLine,
				)
				fmt.Fprintln(os.Stdout, "    --- before ---")
				if len(h.Before.Lines) == 0 {
					fmt.Fprintln(os.Stdout, "    -")
				} else {
					for _, line := range h.Before.Lines {
						fmt.Fprintf(os.Stdout, "    - %s\n", line)
					}
				}
				fmt.Fprintln(os.Stdout, "    +++ after +++")
				if len(h.After.Lines) == 0 {
					fmt.Fprintln(os.Stdout, "    +")
				} else {
					for _, line := range h.After.Lines {
						fmt.Fprintf(os.Stdout, "    + %s\n", line)
					}
				}
				fmt.Fprintln(os.Stdout)
			}
		}
		if len(res.FileChanges) > 0 {
			fmt.Fprintln(os.Stdout, "Files that would change:")
			for _, change := range res.FileChanges {
				fmt.Fprintf(os.Stdout, "  %s (%d edits)\n", change.Path, change.EditCount)
			}
		}
		fmt.Fprintln(os.Stdout, "No files were modified (preview mode).")
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
	return nil
}

func handlePreviewJSON(res *fix.ApplyResult, applyErr error) error {
	status := "ok"
	var errMsg string
	if errors.Is(applyErr, fix.ErrNoFixes) && len(res.Applied) == 0 {
		status = "no_fixes"
	} else if applyErr != nil {
		status = "error"
		errMsg = applyErr.Error()
	}

	output := previewOutput{
		Status:      status,
		Applied:     make([]appliedPreviewJSON, 0, len(res.Applied)),
		Skipped:     make([]skippedPreviewJSON, 0, len(res.Skipped)),
		FileChanges: make([]fileChangePreviewJSON, 0, len(res.FileChanges)),
		Error:       errMsg,
	}

	for _, item := range res.Applied {
		previewItem := appliedPreviewJSON{
			ID:            item.ID,
			Title:         item.Title,
			Code:          item.Code.ID(),
			Message:       item.Message,
			Applicability: item.Applicability.String(),
			PrimaryPath:   item.PrimaryPath,
			EditCount:     item.EditCount,
			Previews:      make([]previewHunkJSON, 0, len(item.Previews)),
		}
		for _, h := range item.Previews {
			previewItem.Previews = append(previewItem.Previews, previewHunkJSON{
				Path: h.Path,
				Before: previewRangeJSON{
					StartLine: h.Before.StartLine,
					EndLine:   h.Before.EndLine,
					Lines:     append([]string(nil), h.Before.Lines...),
				},
				After: previewRangeJSON{
					StartLine: h.After.StartLine,
					EndLine:   h.After.EndLine,
					Lines:     append([]string(nil), h.After.Lines...),
				},
			})
		}
		output.Applied = append(output.Applied, previewItem)
	}

	for _, skip := range res.Skipped {
		output.Skipped = append(output.Skipped, skippedPreviewJSON{
			ID:     skip.ID,
			Title:  skip.Title,
			Reason: skip.Reason,
		})
	}

	for _, change := range res.FileChanges {
		output.FileChanges = append(output.FileChanges, fileChangePreviewJSON{
			Path:      change.Path,
			EditCount: change.EditCount,
		})
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		return err
	}

	if applyErr != nil && !errors.Is(applyErr, fix.ErrNoFixes) {
		return applyErr
	}
	return nil
}

type previewOutput struct {
	Status      string                  `json:"status"`
	Applied     []appliedPreviewJSON    `json:"applied"`
	Skipped     []skippedPreviewJSON    `json:"skipped"`
	FileChanges []fileChangePreviewJSON `json:"file_changes"`
	Error       string                  `json:"error,omitempty"`
}

type appliedPreviewJSON struct {
	ID            string            `json:"id"`
	Title         string            `json:"title"`
	Code          string            `json:"code"`
	Message       string            `json:"message"`
	Applicability string            `json:"applicability"`
	PrimaryPath   string            `json:"primary_path"`
	EditCount     int               `json:"edit_count"`
	Previews      []previewHunkJSON `json:"previews"`
}

type skippedPreviewJSON struct {
	ID     string `json:"id,omitempty"`
	Title  string `json:"title,omitempty"`
	Reason string `json:"reason"`
}

type fileChangePreviewJSON struct {
	Path      string `json:"path"`
	EditCount int    `json:"edit_count"`
}

type previewHunkJSON struct {
	Path   string           `json:"path"`
	Before previewRangeJSON `json:"before"`
	After  previewRangeJSON `json:"after"`
}

type previewRangeJSON struct {
	StartLine uint32   `json:"start_line"`
	EndLine   uint32   `json:"end_line"`
	Lines     []string `json:"lines"`
}
