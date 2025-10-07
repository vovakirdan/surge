package diag

type Severity uint8

const (
	SevInfo Severity = iota
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
