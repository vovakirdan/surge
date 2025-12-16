package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"surge/internal/driver"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/vm"
)

var runCmd = &cobra.Command{
	Use:   "run [flags] <file.sg>",
	Short: "Compile and execute a Surge program",
	Long:  `Compile a Surge source file to MIR and execute it using the VM backend`,
	Args:  cobra.ExactArgs(1),
	RunE:  runExecution,
}

func init() {
	runCmd.Flags().String("backend", "vm", "execution backend (vm)")
	runCmd.Flags().Bool("vm-trace", false, "enable VM execution tracing")
}

func runExecution(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	// Get flags
	backend, err := cmd.Flags().GetString("backend")
	if err != nil {
		return fmt.Errorf("failed to get backend flag: %w", err)
	}

	vmTrace, err := cmd.Flags().GetBool("vm-trace")
	if err != nil {
		return fmt.Errorf("failed to get vm-trace flag: %w", err)
	}

	// Only VM backend supported for now
	if backend != "vm" {
		return fmt.Errorf("unsupported backend: %s (only 'vm' is supported)", backend)
	}

	// Compile source to MIR
	opts := driver.DiagnoseOptions{
		Stage:              driver.DiagnoseStageSema,
		EmitHIR:            true,
		EmitInstantiations: true,
	}

	result, err := driver.DiagnoseWithOptions(cmd.Context(), filePath, opts)
	if err != nil {
		return fmt.Errorf("compilation failed: %w", err)
	}

	// Check for errors
	if result.Bag.HasErrors() {
		// Print diagnostics and exit
		for _, d := range result.Bag.Items() {
			fmt.Fprintln(os.Stderr, d.Message)
		}
		os.Exit(1)
	}

	// Build HIR (should already be built with EmitHIR=true)
	if result.HIR == nil {
		return fmt.Errorf("HIR not available")
	}
	if result.Instantiations == nil {
		return fmt.Errorf("instantiation map not available")
	}
	if result.Sema == nil {
		return fmt.Errorf("semantic analysis result not available")
	}

	// Monomorphize
	mm, err := mono.MonomorphizeModule(result.HIR, result.Instantiations, result.Sema, mono.Options{
		MaxDepth: 64,
	})
	if err != nil {
		return fmt.Errorf("monomorphization failed: %w", err)
	}

	// Lower to MIR
	mirMod, err := mir.LowerModule(mm, result.Sema)
	if err != nil {
		return fmt.Errorf("MIR lowering failed: %w", err)
	}

	// Simplify CFG and recognize switch patterns
	for _, f := range mirMod.Funcs {
		mir.SimplifyCFG(f)
		mir.RecognizeSwitchTag(f)
		mir.SimplifyCFG(f)
	}

	// Validate MIR
	if err := mir.Validate(mirMod, result.Sema.TypeInterner); err != nil {
		return fmt.Errorf("MIR validation failed: %w", err)
	}

	// Create VM
	rt := vm.NewDefaultRuntime()

	var tracer *vm.Tracer
	if vmTrace {
		tracer = vm.NewTracer(os.Stderr, result.FileSet)
	}

	vmInstance := vm.New(mirMod, rt, result.FileSet, result.Sema.TypeInterner, tracer)

	// Execute
	if vmErr := vmInstance.Run(); vmErr != nil {
		// Print panic with backtrace
		fmt.Fprint(os.Stderr, vmErr.FormatWithFiles(result.FileSet))
		os.Exit(1)
	}

	// Return with exit code from runtime
	os.Exit(vmInstance.ExitCode)
	return nil
}
