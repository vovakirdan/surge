package buildpipeline

import "time"

// Stage describes a high-level pipeline phase.
type Stage string

const (
	// StageParse is the parsing stage.
	StageParse Stage = "parse"
	// StageDiagnose is the diagnose stage.
	StageDiagnose Stage = "diagnose"
	// StageLower is the lower stage.
	StageLower Stage = "lower"
	// StageBuild is the build stage.
	StageBuild Stage = "build"
	// StageLink is the link stage.
	StageLink Stage = "link"
	// StageRun is the run stage.
	StageRun Stage = "run"
)

// Status captures progress state within a stage.
type Status string

const (
	// StatusQueued indicates the task is waiting to start.
	StatusQueued Status = "queued"
	// StatusWorking indicates the task is currently working.
	StatusWorking Status = "working"
	// StatusDone indicates the task is done.
	StatusDone Status = "done"
	// StatusError indicates the task encountered an error.
	StatusError Status = "error"
)

// Event reports progress for a file (or for the overall pipeline when File is empty).
type Event struct {
	File    string
	Stage   Stage
	Status  Status
	Err     error
	Elapsed time.Duration
}

// ProgressSink consumes progress events.
type ProgressSink interface {
	OnEvent(Event)
}

// Backend selects the compilation backend.
type Backend string

const (
	// BackendVM selects the interpreter backend.
	BackendVM Backend = "vm"
	// BackendLLVM selects the LLVM backend.
	BackendLLVM Backend = "llvm"
)

// Timings holds stage durations.
type Timings struct {
	stages map[Stage]time.Duration
}

func (t *Timings) ensure() {
	if t.stages == nil {
		t.stages = make(map[Stage]time.Duration)
	}
}

// Set stores a duration for the given stage.
func (t *Timings) Set(stage Stage, dur time.Duration) {
	if t == nil {
		return
	}
	t.ensure()
	t.stages[stage] = dur
}

// Has reports whether a duration for stage is recorded.
func (t Timings) Has(stage Stage) bool {
	if t.stages == nil {
		return false
	}
	_, ok := t.stages[stage]
	return ok
}

// Duration returns the recorded duration for stage.
func (t Timings) Duration(stage Stage) time.Duration {
	if t.stages == nil {
		return 0
	}
	return t.stages[stage]
}

// Sum returns the sum of durations across the provided stages.
func (t Timings) Sum(stages ...Stage) time.Duration {
	if t.stages == nil {
		return 0
	}
	var total time.Duration
	for _, stage := range stages {
		total += t.stages[stage]
	}
	return total
}
