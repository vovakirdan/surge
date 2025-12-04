package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"surge/internal/prof"
)

// setupProfiling inspects persistent profiling flags and enables the
// corresponding profilers. It returns a cleanup function that is safe to call
// multiple times.
func setupProfiling(cmd *cobra.Command) (func(), error) {
	root := cmd.Root()

	cpuProfile, err := root.PersistentFlags().GetString("cpu-profile")
	if err != nil {
		return nil, fmt.Errorf("failed to get cpu-profile flag: %w", err)
	}
	memProfile, err := root.PersistentFlags().GetString("mem-profile")
	if err != nil {
		return nil, fmt.Errorf("failed to get mem-profile flag: %w", err)
	}
	tracePath, err := root.PersistentFlags().GetString("runtime-trace")
	if err != nil {
		return nil, fmt.Errorf("failed to get runtime-trace flag: %w", err)
	}

	stopCPU := func() {}
	stopTrace := func() {}
	writeMem := func() {}

	if cpuProfile != "" {
		if err := prof.StartCPU(cpuProfile); err != nil {
			return nil, fmt.Errorf("failed to start cpu profile: %w", err)
		}
		stopCPU = prof.StopCPU
	}
	if tracePath != "" {
		if err := prof.StartTrace(tracePath); err != nil {
			// ensure cpu profile is stopped on error
			stopCPU()
			return nil, fmt.Errorf("failed to start trace: %w", err)
		}
		stopTrace = prof.StopTrace
	}
	if memProfile != "" {
		writeMem = func() {
			if err := prof.WriteMem(memProfile); err != nil {
				fmt.Fprintf(os.Stderr, "failed to write heap profile: %v\n", err)
			}
		}
	}

	cleaned := false
	cleanup := func() {
		if cleaned {
			return
		}
		cleaned = true
		stopTrace()
		stopCPU()
		writeMem()
	}

	return cleanup, nil
}
