package trace

import "time"

// Kind represents the type of trace event.
type Kind uint8

const (
	KindSpanBegin Kind = iota + 1 // span start
	KindSpanEnd                   // span end
	KindPoint                     // instant event
	KindHeartbeat                 // periodic liveness signal
)

// String returns the string representation of Kind.
func (k Kind) String() string {
	switch k {
	case KindSpanBegin:
		return "begin"
	case KindSpanEnd:
		return "end"
	case KindPoint:
		return "point"
	case KindHeartbeat:
		return "heartbeat"
	default:
		return "unknown"
	}
}

// Scope indicates the granularity level of the event.
// Lower numeric values represent higher-level/coarser events.
type Scope uint8

const (
	ScopeDriver Scope = iota + 1 // top-level driver operations (highest level)
	ScopePass                    // compilation passes (lex, parse, sema, borrow)
	ScopeModule                  // per-module processing (more detailed)
	ScopeNode                    // AST node level (most detailed, future)
)

// String returns the string representation of Scope.
func (s Scope) String() string {
	switch s {
	case ScopeDriver:
		return "driver"
	case ScopePass:
		return "pass"
	case ScopeModule:
		return "module"
	case ScopeNode:
		return "node"
	default:
		return "unknown"
	}
}

// Event represents a single trace event.
type Event struct {
	Time     time.Time         // wall-clock timestamp
	Seq      uint64            // global sequence number (monotonic)
	Kind     Kind              // event kind
	Scope    Scope             // granularity level
	SpanID   uint64            // unique span identifier
	ParentID uint64            // parent span (0 if root)
	GID      uint64            // goroutine ID (for concurrent spans)
	Name     string            // e.g., "parse", "sema", "module:foo/bar"
	Detail   string            // optional detail message
	Extra    map[string]string // extensible key-value pairs
}
