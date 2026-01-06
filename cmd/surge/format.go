package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"surge/internal/driver"
)

var fmtCmd = &cobra.Command{
	Use:   "fmt [flags] <path> [path...]",
	Short: "Format surge source files",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runFmt,
}

func init() {
	fmtCmd.Flags().Bool("check", false, "check if files are properly formatted")
	fmtCmd.Flags().String("format", "text", "output format (text|json)")
	fmtCmd.Flags().Bool("stdout", false, "print formatted code to stdout instead of rewriting files")
}

func runFmt(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	check, err := cmd.Flags().GetBool("check")
	if err != nil {
		return err
	}

	outputFormat, err := cmd.Flags().GetString("format")
	if err != nil {
		return err
	}

	writeToStdout, err := cmd.Flags().GetBool("stdout")
	if err != nil {
		return err
	}

	if writeToStdout && check {
		return fmt.Errorf("fmt: --stdout cannot be used with --check")
	}
	if writeToStdout && outputFormat != "text" {
		return fmt.Errorf("fmt: --stdout is only supported with text output")
	}

	maxDiagnostics, err := cmd.Root().PersistentFlags().GetInt("max-diagnostics")
	if err != nil {
		return err
	}

	quiet, err := cmd.Root().PersistentFlags().GetBool("quiet")
	if err != nil {
		return err
	}

	formatResults, err := driver.FormatPaths(cmd.Context(), args, driver.FormatOptions{
		Check:          check,
		MaxDiagnostics: maxDiagnostics,
		Stdout:         writeToStdout,
	})
	if err != nil {
		return err
	}

	var hasErrors bool
	var hasChanges bool

	switch outputFormat {
	case "text":
		if writeToStdout {
			renderFmtStdout(formatResults, &hasErrors)
			if hasErrors {
				return fmt.Errorf("fmt: failed to format some files")
			}
			return nil
		}
		renderFmtText(formatResults, check, quiet, &hasErrors, &hasChanges)
	case "json":
		if err := renderFmtJSON(formatResults, check); err != nil {
			return err
		}
	default:
		return fmt.Errorf("fmt: unsupported output format %q", outputFormat)
	}

	if hasErrors {
		return fmt.Errorf("fmt: failed to format some files")
	}
	if check && hasChanges {
		return fmt.Errorf("fmt: formatting changes required")
	}
	return nil
}

func renderFmtStdout(results []driver.FormatResult, hasErrors *bool) {
	for _, res := range results {
		if res.Err != nil {
			*hasErrors = true
			fmt.Fprintf(os.Stderr, "fmt: %s: %v\n", res.Path, res.Err)
			continue
		}

		_, _ = os.Stdout.Write(res.Formatted)
	}
}

func renderFmtText(results []driver.FormatResult, check, quiet bool, hasErrors, hasChanges *bool) {
	for _, res := range results {
		if res.Err != nil {
			*hasErrors = true
			fmt.Fprintf(os.Stderr, "fmt: %s: %v\n", res.Path, res.Err)
			continue
		}

		if check {
			if res.Changed {
				*hasChanges = true
				if !quiet {
					_, printErr := fmt.Fprintln(os.Stdout, res.Path)
					if printErr != nil {
						panic(printErr)
					}
				}
			}
			continue
		}

		if res.Changed && !quiet {
			_, printErr := fmt.Fprintf(os.Stdout, "reformatted %s\n", res.Path)
			if printErr != nil {
				panic(printErr)
			}
		}
	}
}

func renderFmtJSON(results []driver.FormatResult, check bool) error {
	type jsonResult struct {
		Path     string `json:"path"`
		Changed  bool   `json:"changed"`
		Error    string `json:"error,omitempty"`
		CheckRun bool   `json:"check"`
	}

	payload := make([]jsonResult, 0, len(results))
	for _, res := range results {
		jr := jsonResult{Path: res.Path, Changed: res.Changed, CheckRun: check}
		if res.Err != nil {
			jr.Error = res.Err.Error()
		}
		payload = append(payload, jr)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}
