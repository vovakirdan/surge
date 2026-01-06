package dialect

import "fmt"

// Kind represents a foreign language "dialect" that a Surge file may
// resemble.
type Kind uint8

const (
	// Unknown indicates the dialect was not identified.
	Unknown Kind = iota
	// Rust indicates Rust dialect.
	Rust
	Go
	TypeScript
	Python

	dialectKindCount
)

func (k Kind) String() string {
	switch k {
	case Rust:
		return "rust"
	case Go:
		return "go"
	case TypeScript:
		return "typescript"
	case Python:
		return "python"
	default:
		return "unknown"
	}
}

// GoString returns a string representation for debugging.
func (k Kind) GoString() string {
	return fmt.Sprintf("dialect.Kind(%s)", k.String())
}
