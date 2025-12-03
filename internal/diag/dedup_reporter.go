package diag

import "surge/internal/source"

type dedupKey struct {
	code  Code
	sev   Severity
	file  source.FileID
	start uint32
	end   uint32
	msg   string
}

// DedupReporter wraps another Reporter and suppresses duplicate diagnostics
// with the same code, severity, primary span and message.
type DedupReporter struct {
	next Reporter
	seen map[dedupKey]struct{}
}

// NewDedupReporter returns a Reporter that filters out duplicates while
// forwarding unique diagnostics to the provided reporter.
func NewDedupReporter(next Reporter) *DedupReporter {
	return &DedupReporter{
		next: next,
		seen: make(map[dedupKey]struct{}),
	}
}

func (r *DedupReporter) Report(code Code, sev Severity, primary source.Span, msg string, notes []Note, fixes []*Fix) {
	if r == nil {
		return
	}
	key := dedupKey{
		code:  code,
		sev:   sev,
		file:  primary.File,
		start: primary.Start,
		end:   primary.End,
		msg:   msg,
	}
	if _, ok := r.seen[key]; ok {
		return
	}
	r.seen[key] = struct{}{}
	if r.next != nil {
		r.next.Report(code, sev, primary, msg, notes, fixes)
	}
}
