package dialect

import "fmt"

// DialectKind represents a foreign language "dialect" that a Surge file may
// resemble.
type DialectKind uint8

const (
	DialectUnknown DialectKind = iota
	DialectRust
	DialectGo
	DialectTypeScript
	DialectPython

	dialectKindCount
)

func (k DialectKind) String() string {
	switch k {
	case DialectRust:
		return "rust"
	case DialectGo:
		return "go"
	case DialectTypeScript:
		return "typescript"
	case DialectPython:
		return "python"
	default:
		return "unknown"
	}
}

func (k DialectKind) GoString() string {
	return fmt.Sprintf("DialectKind(%s)", k.String())
}
