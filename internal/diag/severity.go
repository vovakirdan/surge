package diag

// Severity defines the importance of a diagnostic.
type Severity uint8

const (
	// SevInfo is for informational diagnostics.
	SevInfo Severity = iota
	// SevWarning is for warning diagnostics.
	SevWarning
	SevError
)

func (s Severity) String() string {
	switch s {
	case SevInfo:
		return "INFO"
	case SevWarning:
		return "WARNING"
	case SevError:
		return "ERROR"
	}
	return "UNKNOWN"
}
