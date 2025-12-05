package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

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

	formatStr, err := root.PersistentFlags().GetString("trace-format")
	if err != nil {
		return nil, fmt.Errorf("failed to get trace-format flag: %w", err)
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

	// Auto-detect: if output is a file path and mode is ring, use stream instead
	// Ring mode is designed for in-memory buffering and dump-on-signal scenarios.
	// When the user specifies a file path, they expect immediate writes.
	if traceOutput != "" && traceOutput != "-" && mode == trace.ModeRing {
		mode = trace.ModeStream
	}

	// Parse format
	format, err := trace.ParseFormat(formatStr)
	if err != nil {
		return nil, fmt.Errorf("invalid trace format: %w", err)
	}

	// Create tracer config
	cfg := trace.Config{
		Level:      level,
		Mode:       mode,
		Format:     format,
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

	// Setup signal handling for graceful trace dump on interrupt
	setupSignalHandler(tracer, traceOutput, heartbeat)

	// Setup panic recovery
	setupPanicHandler(tracer, traceOutput, heartbeat)

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

// Global variables for panic recovery
var (
	panicTracer     trace.Tracer
	panicOutputPath string
	panicHeartbeat  *trace.Heartbeat
)

// setupPanicHandler stores tracer information globally for panic recovery.
// This ensures trace information can be accessed during panic handling.
func setupPanicHandler(tracer trace.Tracer, outputPath string, heartbeat *trace.Heartbeat) {
	panicTracer = tracer
	panicOutputPath = outputPath
	panicHeartbeat = heartbeat
}

// dumpTraceOnPanic should be called with defer at the entry point of commands.
// It recovers from panics and dumps the trace buffer before re-panicking.
// Usage: defer dumpTraceOnPanic()
func dumpTraceOnPanic() {
	if r := recover(); r != nil {
		fmt.Fprintf(os.Stderr, "\ntrace: panic detected: %v\n", r)

		// Stop heartbeat if running
		if panicHeartbeat != nil {
			panicHeartbeat.Stop()
		}

		// Dump ring buffer if available
		if panicTracer != nil {
			if rt := findRingTracer(panicTracer); rt != nil && panicOutputPath != "" {
				dumpPath := generateDumpPath(panicOutputPath, "panic")
				if f, err := os.Create(dumpPath); err == nil {
					if dumpErr := rt.Dump(f, trace.FormatText); dumpErr != nil {
						fmt.Fprintf(os.Stderr, "trace: dump error: %v\n", dumpErr)
					} else {
						fmt.Fprintf(os.Stderr, "trace: ring buffer saved to %s\n", dumpPath)
					}
					f.Close()
				} else {
					fmt.Fprintf(os.Stderr, "trace: failed to create dump file: %v\n", err)
				}
			}

			// Flush and close tracer
			if err := panicTracer.Flush(); err != nil {
				fmt.Fprintf(os.Stderr, "trace: flush error: %v\n", err)
			}
			if err := panicTracer.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "trace: close error: %v\n", err)
			}
		}

		// Re-panic to maintain normal panic behavior
		panic(r)
	}
}

// findRingTracer tries to extract a RingTracer from the given tracer.
// It handles both direct RingTracer and MultiTracer containing a RingTracer.
func findRingTracer(tracer trace.Tracer) *trace.RingTracer {
	if tracer == nil {
		return nil
	}

	// Direct RingTracer
	if rt, ok := tracer.(*trace.RingTracer); ok {
		return rt
	}

	// MultiTracer containing RingTracer - iterate through underlying tracers
	if mt, ok := tracer.(*trace.MultiTracer); ok {
		for _, t := range mt.Tracers() {
			if rt, ok := t.(*trace.RingTracer); ok {
				return rt
			}
		}
	}

	return nil
}

// setupSignalHandler installs signal handlers to save trace data on interruption.
// When SIGINT or SIGTERM is received, it dumps the ring buffer (if available) and exits.
func setupSignalHandler(tracer trace.Tracer, outputPath string, heartbeat *trace.Heartbeat) {
	if tracer == nil {
		return
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "\ntrace: received %v, saving trace data...\n", sig)

		// Stop heartbeat if running
		if heartbeat != nil {
			heartbeat.Stop()
		}

		// Dump ring buffer if available
		if rt := findRingTracer(tracer); rt != nil && outputPath != "" {
			dumpPath := generateDumpPath(outputPath, "interrupt")
			if f, err := os.Create(dumpPath); err == nil {
				if dumpErr := rt.Dump(f, trace.FormatText); dumpErr != nil {
					fmt.Fprintf(os.Stderr, "trace: dump error: %v\n", dumpErr)
				} else {
					fmt.Fprintf(os.Stderr, "trace: ring buffer saved to %s\n", dumpPath)
				}
				f.Close()
			} else {
				fmt.Fprintf(os.Stderr, "trace: failed to create dump file: %v\n", err)
			}
		}

		// Flush and close tracer
		if err := tracer.Flush(); err != nil {
			fmt.Fprintf(os.Stderr, "trace: flush error: %v\n", err)
		}
		if err := tracer.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "trace: close error: %v\n", err)
		}

		// Exit with signal-appropriate code
		if sig == syscall.SIGINT {
			os.Exit(130) // 128 + SIGINT
		} else {
			os.Exit(143) // 128 + SIGTERM
		}
	}()
}

// generateDumpPath creates a dump file path based on the original output path and reason.
// Examples:
//   - trace.log -> trace.interrupt.log
//   - /tmp/trace.log -> /tmp/trace.interrupt.log
//   - - (stderr) -> surge.interrupt.trace
func generateDumpPath(outputPath, reason string) string {
	if outputPath == "" || outputPath == "-" {
		return fmt.Sprintf("surge.%s.trace", reason)
	}

	ext := filepath.Ext(outputPath)
	base := outputPath[:len(outputPath)-len(ext)]
	if ext == "" {
		return fmt.Sprintf("%s.%s.trace", outputPath, reason)
	}
	return fmt.Sprintf("%s.%s%s", base, reason, ext)
}
