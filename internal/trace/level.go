package trace

import "fmt"

// Level controls tracing verbosity.
type Level uint8

const (
	// LevelOff disables tracing.
	LevelOff    Level = iota // no tracing
	LevelError               // only emit on errors/crashes
	LevelPhase               // driver + pass boundaries
	LevelDetail              // module-level events
	LevelDebug               // everything including node-level
)

// String returns the string representation of Level.
func (l Level) String() string {
	switch l {
	case LevelOff:
		return "off"
	case LevelError:
		return "error"
	case LevelPhase:
		return "phase"
	case LevelDetail:
		return "detail"
	case LevelDebug:
		return "debug"
	default:
		return "unknown"
	}
}

// ParseLevel converts a string to a Level.
func ParseLevel(s string) (Level, error) {
	switch s {
	case "off", "OFF":
		return LevelOff, nil
	case "error", "ERROR":
		return LevelError, nil
	case "phase", "PHASE":
		return LevelPhase, nil
	case "detail", "DETAIL":
		return LevelDetail, nil
	case "debug", "DEBUG":
		return LevelDebug, nil
	default:
		return LevelOff, fmt.Errorf("invalid trace level: %q (expected: off|error|phase|detail|debug)", s)
	}
}

// ShouldEmit returns true if the given scope should emit at this level.
func (l Level) ShouldEmit(scope Scope) bool {
	switch l {
	case LevelOff:
		return false
	case LevelError:
		return false // error events always emitted via crash path
	case LevelPhase:
		return scope <= ScopePass
	case LevelDetail:
		return scope <= ScopeModule
	case LevelDebug:
		return true
	}
	return false
}
