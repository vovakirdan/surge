package debug

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

type Depth uint8

const (
	DepthOff   Depth = 0
	DepthFunc  Depth = 1
	DepthCalls Depth = 2
	DepthLoops Depth = 3
	DepthAll   Depth = 4
)

type Config struct {
	// Enabled is auto-derived from Path != "" but you can force it.
	Enabled bool

	// Path is a hierarchical selector:
	//   "sema/const_eval" -> matches "sema.const_eval.*"
	//   "sema/const_eval/ensureConstEvaluated" -> matches exactly that function scope and its children
	Path string

	// Depth: off|func|calls|loops|all
	Depth string

	// Level: trace|debug|info|warn|error
	Level string

	// Format: pretty|json
	Format string

	// OutPath: "" => stderr; otherwise open file with truncate.
	OutPath string
}

var (
	enabled atomic.Bool

	curLogger atomic.Pointer[zerolog.Logger]

	// scopePrefix is canonical dot-path, e.g. "sema.const_eval"
	// or "sema.const_eval.ensureConstEvaluated"
	scopePrefix atomic.Value // string

	// current verbosity
	curDepth atomic.Uint32 // Depth
)

var nopLogger = zerolog.Nop()

func Init(cfg Config) error {
	path := strings.TrimSpace(cfg.Path)
	if path == "" && !cfg.Enabled {
		Disable()
		return nil
	}
	if path == "" && cfg.Enabled {
		return errors.New("debug: enabled but Path is empty")
	}

	enabled.Store(true)
	scopePrefix.Store(pathToScopePrefix(path))
	curDepth.Store(uint32(parseDepth(cfg.Depth)))
	zerolog.SetGlobalLevel(parseLevel(cfg.Level))

	out, err := openWriter(cfg.OutPath)
	if err != nil {
		return err
	}

	var w io.Writer = out
	if cfg.Format == "" || strings.EqualFold(cfg.Format, "pretty") || strings.EqualFold(cfg.Format, "console") {
		w = zerolog.ConsoleWriter{Out: out, TimeFormat: time.RFC3339Nano}
	}

	l := zerolog.New(w).With().Timestamp().Logger()
	curLogger.Store(&l)
	return nil
}

func Disable() {
	enabled.Store(false)
	scopePrefix.Store("")
	curDepth.Store(uint32(DepthOff))
	curLogger.Store(&nopLogger)
}

func Allowed(scope string, min Depth) bool {
	if !enabled.Load() {
		return false
	}
	if Depth(curDepth.Load()) < min {
		return false
	}
	pfx, _ := scopePrefix.Load().(string)
	if pfx == "" {
		return false
	}
	// Prefix match:
	// enabled "sema.const_eval" matches "sema.const_eval" and "sema.const_eval.*"
	return scope == pfx || strings.HasPrefix(scope, pfx+".")
}

func FuncSpan(scope string, fn string, kv ...any) func() {
	// FuncSpan should only appear at DepthFunc and above
	if !Allowed(scope, DepthFunc) {
		return func() {}
	}

	log := logger(scope)
	start := time.Now()

	ev := log.Debug().
		Str("fn", fn).
		Str("event", "enter")
	addKV(ev, kv...)
	ev.Msg("enter")

	return func() {
		ev2 := log.Debug().
			Str("fn", fn).
			Str("event", "exit").
			Dur("dur", time.Since(start))
		ev2.Msg("exit")
	}
}

// Point is a generic debug marker.
func Point(scope string, min Depth, msg string, kv ...any) {
	if !Allowed(scope, min) {
		return
	}
	l := logger(scope)
	ev := l.Debug().Str("event", "point")
	addKV(ev, kv...)
	ev.Msg(msg)
}

// Dump is a semantic alias for Point: meant for "state snapshots".
func Dump(scope string, min Depth, key string, value any, extraKV ...any) {
	if !Allowed(scope, min) {
		return
	}
	l := logger(scope)
	ev := l.Debug().Str("event", "dump")
	// Store under "key" and "value" to keep format stable.
	ev.Str("key", key)
	ev.Interface("value", value)
	addKV(ev, extraKV...)
	ev.Msg("dump")
}

func CurrentDepth() Depth { return Depth(curDepth.Load()) }

func logger(scope string) zerolog.Logger {
	ptr := curLogger.Load()
	if ptr == nil {
		return nopLogger
	}
	return (*ptr).With().Str("scope", scope).Logger()
}

func addKV(ev *zerolog.Event, kv ...any) {
	// kv is pairs: "k1", v1, "k2", v2 ...
	n := len(kv)
	for i := 0; i+1 < n; i += 2 {
		kAny := kv[i]
		v := kv[i+1]

		ks, ok := kAny.(string)
		if !ok || ks == "" {
			continue
		}
		ev.Interface(ks, v)
	}
}

func pathToScopePrefix(path string) string {
	// Normalize:
	//   "sema/const_eval/ensureConstEvaluated" -> "sema.const_eval.ensureConstEvaluated"
	p := strings.Trim(path, "/")
	p = strings.ReplaceAll(p, "/", ".")
	p = strings.ReplaceAll(p, "\\", ".")
	return strings.ToLower(p)
}

func parseDepth(s string) Depth {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "off", "0", "none":
		return DepthOff
	case "", "func":
		return DepthFunc
	case "calls":
		return DepthCalls
	case "loops":
		return DepthLoops
	case "all":
		return DepthAll
	default:
		return DepthFunc
	}
}

func parseLevel(s string) zerolog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return zerolog.TraceLevel
	case "", "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.DebugLevel
	}
}

func openWriter(path string) (io.Writer, error) {
	if strings.TrimSpace(path) == "" {
		return os.Stderr, nil
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	// truncate
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	return f, nil
}
