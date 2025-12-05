package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"surge/internal/trace"
)

// setupTracing inspects trace-related flags and initializes the tracer.
// It returns a cleanup function and an error if initialization fails.
func setupTracing(cmd *cobra.Command) (func(), error) {
	root := cmd.Root()

	// Read trace configuration from flags
	traceOutput, err := root.PersistentFlags().GetString("trace")
	if err != nil {
		return nil, fmt.Errorf("failed to get trace flag: %w", err)
	}

	levelStr, err := root.PersistentFlags().GetString("trace-level")
	if err != nil {
		return nil, fmt.Errorf("failed to get trace-level flag: %w", err)
	}

	modeStr, err := root.PersistentFlags().GetString("trace-mode")
	if err != nil {
		return nil, fmt.Errorf("failed to get trace-mode flag: %w", err)
	}

	ringSize, err := root.PersistentFlags().GetInt("trace-ring-size")
	if err != nil {
		return nil, fmt.Errorf("failed to get trace-ring-size flag: %w", err)
	}

	heartbeatInterval, err := root.PersistentFlags().GetDuration("trace-heartbeat")
	if err != nil {
		return nil, fmt.Errorf("failed to get trace-heartbeat flag: %w", err)
	}

	// Parse level
	level, err := trace.ParseLevel(levelStr)
	if err != nil {
		return nil, fmt.Errorf("invalid trace level: %w", err)
	}

	// If level is off and no output specified, skip tracing
	if level == trace.LevelOff && traceOutput == "" {
		ctx := trace.WithTracer(cmd.Context(), trace.Nop)
		cmd.SetContext(ctx)
		return func() {}, nil
	}

	// Parse mode
	mode, err := trace.ParseMode(modeStr)
	if err != nil {
		return nil, fmt.Errorf("invalid trace mode: %w", err)
	}

	// Create tracer config
	cfg := trace.Config{
		Level:      level,
		Mode:       mode,
		OutputPath: traceOutput,
		RingSize:   ringSize,
		Heartbeat:  heartbeatInterval,
	}

	// Create tracer
	tracer, err := trace.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create tracer: %w", err)
	}

	// Attach tracer to context
	ctx := trace.WithTracer(cmd.Context(), tracer)
	cmd.SetContext(ctx)
	cmd.Root().SetContext(ctx)

	// Start heartbeat if configured
	var heartbeat *trace.Heartbeat
	if heartbeatInterval > 0 {
		heartbeat = trace.StartHeartbeat(tracer, heartbeatInterval)
	}

	// Return cleanup function
	cleanup := func() {
		// Stop heartbeat first
		if heartbeat != nil {
			heartbeat.Stop()
		}

		if err := tracer.Flush(); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "trace: flush error: %v\n", err)
		}
		if err := tracer.Close(); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "trace: close error: %v\n", err)
		}
	}

	return cleanup, nil
}
