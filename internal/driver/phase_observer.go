package driver

import "time"

// PhaseStatus reports whether a phase started or finished.
type PhaseStatus int

const (
	// PhaseStart indicates that a compilation phase has begun.
	PhaseStart PhaseStatus = iota
	PhaseEnd
)

// PhaseEvent describes a timing phase boundary.
type PhaseEvent struct {
	Name    string
	Status  PhaseStatus
	Elapsed time.Duration
}

// PhaseObserver receives phase events emitted during DiagnoseWithOptions.
type PhaseObserver func(PhaseEvent)
