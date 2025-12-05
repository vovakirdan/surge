package trace

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Tracer is the main interface for emitting trace events.
type Tracer interface {
	// Emit records a trace event. Must be goroutine-safe.
	Emit(ev *Event)

	// Flush ensures all buffered events are written.
	Flush() error

	// Close flushes and releases resources.
	Close() error

	// Level returns the current tracing level.
	Level() Level

	// Enabled returns true if tracing is active (Level > LevelOff).
	Enabled() bool
}

// StorageMode determines how events are stored.
type StorageMode uint8

const (
	ModeStream StorageMode = iota + 1 // immediate write
	ModeRing                          // circular buffer
	ModeBoth                          // stream + ring
)

// String returns the string representation of StorageMode.
func (m StorageMode) String() string {
	switch m {
	case ModeStream:
		return "stream"
	case ModeRing:
		return "ring"
	case ModeBoth:
		return "both"
	default:
		return "unknown"
	}
}

// ParseMode converts a string to StorageMode.
func ParseMode(s string) (StorageMode, error) {
	switch strings.ToLower(s) {
	case "stream":
		return ModeStream, nil
	case "ring":
		return ModeRing, nil
	case "both":
		return ModeBoth, nil
	default:
		return ModeRing, fmt.Errorf("invalid storage mode: %q (expected: stream|ring|both)", s)
	}
}

// Config holds tracer configuration.
type Config struct {
	Level      Level         // tracing level
	Mode       StorageMode   // storage mode
	Format     Format        // output format (FormatAuto for auto-detection)
	Output     io.Writer     // for stream mode (if nil, use OutputPath)
	OutputPath string        // alternative: file path ("-" for stderr)
	RingSize   int           // for ring mode (default 4096)
	Heartbeat  time.Duration // heartbeat interval (0 = disabled)
}

// New creates a Tracer based on Config.
func New(cfg Config) (Tracer, error) {
	if cfg.Level == LevelOff {
		return nopTracer{}, nil
	}

	// Default ring size
	if cfg.RingSize <= 0 {
		cfg.RingSize = 4096
	}

	// Determine output format
	format := cfg.Format
	if format == FormatAuto {
		// Auto-detect from file extension
		format = FormatText // default
		if cfg.OutputPath != "" && cfg.OutputPath != "-" {
			if strings.HasSuffix(cfg.OutputPath, ".ndjson") {
				format = FormatNDJSON
			} else if strings.HasSuffix(cfg.OutputPath, ".json") || strings.HasSuffix(cfg.OutputPath, ".chrome.json") {
				format = FormatChrome
			}
		}
	}

	switch cfg.Mode {
	case ModeStream:
		w, err := openOutput(cfg)
		if err != nil {
			return nil, err
		}
		return NewStreamTracer(w, cfg.Level, format), nil

	case ModeRing:
		return NewRingTracer(cfg.RingSize, cfg.Level), nil

	case ModeBoth:
		w, err := openOutput(cfg)
		if err != nil {
			return nil, err
		}
		stream := NewStreamTracer(w, cfg.Level, format)
		ring := NewRingTracer(cfg.RingSize, cfg.Level)
		return NewMultiTracer(cfg.Level, stream, ring), nil

	default:
		return nil, fmt.Errorf("unknown storage mode: %v", cfg.Mode)
	}
}

// openOutput opens the output writer from config.
func openOutput(cfg Config) (io.Writer, error) {
	if cfg.Output != nil {
		return cfg.Output, nil
	}

	if cfg.OutputPath == "" || cfg.OutputPath == "-" {
		return os.Stderr, nil
	}

	f, err := os.Create(cfg.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open trace output: %w", err)
	}

	return f, nil
}
