package directive

import (
	"fmt"
	"io"
	"path/filepath"
)

// RunnerConfig configures directive execution.
type RunnerConfig struct {
	// Filter limits execution to specific namespaces (empty = all).
	Filter []string

	// Output is where to write execution status.
	Output io.Writer
}

// RunResult contains the outcome of running directives.
type RunResult struct {
	Total   int
	Skipped int
	Passed  int
	Failed  int
}

// Runner executes directive scenarios.
// Stage 1: This is a stub that only prints scenario names.
type Runner struct {
	config   RunnerConfig
	registry *Registry
}

// NewRunner creates a directive runner.
func NewRunner(registry *Registry, config RunnerConfig) *Runner {
	return &Runner{
		config:   config,
		registry: registry,
	}
}

// Run executes (or stubs) all matching scenarios.
// Stage 1 implementation: prints names and reports SKIPPED.
func (r *Runner) Run() RunResult {
	scenarios := r.registry.FilterByNamespace(r.config.Filter)

	result := RunResult{
		Total: len(scenarios),
	}

	for i := range scenarios {
		s := &scenarios[i]
		location := formatLocation(s)
		fmt.Fprintf(r.config.Output, "Running test: %s (%s) ... SKIPPED (execution not implemented)\n",
			location, s.Namespace)
		result.Skipped++
	}

	// Print summary
	fmt.Fprintln(r.config.Output)
	fmt.Fprintf(r.config.Output, "Directive execution summary: %d total, %d skipped, %d passed, %d failed\n",
		result.Total, result.Skipped, result.Passed, result.Failed)

	return result
}

// formatLocation returns a human-readable location string.
func formatLocation(s *Scenario) string {
	// Extract filename from path
	file := filepath.Base(s.SourceFile)
	return fmt.Sprintf("%s#%d", file, s.Index)
}
