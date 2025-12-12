package dialect

import "surge/internal/source"

// Hint is a small piece of evidence suggesting a particular dialect.
// It is not itself a diagnostic; it is used to classify a file before emitting
// optional hint diagnostics in later stages.
type Hint struct {
	Dialect DialectKind
	Score   int
	Reason  string
	Span    source.Span
}

// Evidence aggregates per-file hints collected during tokenization/parsing.
type Evidence struct {
	hints []Hint
}

func NewEvidence() *Evidence {
	return &Evidence{
		hints: make([]Hint, 0, 16),
	}
}

func (e *Evidence) Add(h Hint) {
	if e == nil {
		return
	}
	e.hints = append(e.hints, h)
}

func (e *Evidence) Hints() []Hint {
	if e == nil {
		return nil
	}
	return e.hints
}
