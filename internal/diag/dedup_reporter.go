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

// Report filters out duplicate diagnostics and forwards unique ones.
func (r *DedupReporter) Report(code Code, sev Severity, primary source.Span, msg string, notes []Note, fixes []*Fix) {
	r.ReportDiagnostic(&Diagnostic{
		Severity: sev, Code: code, Message: msg,
		Primary: primary, Notes: notes, Fixes: fixes,
	})
}

// ReportDiagnostic filters duplicates on the same key and forwards the whole
// diagnostic. Help takes no part in the key: two reports that agree on code,
// severity, span and message are one diagnostic however differently they were
// advised.
func (r *DedupReporter) ReportDiagnostic(d *Diagnostic) {
	if r == nil || d == nil {
		return
	}
	key := dedupKey{
		code:  d.Code,
		sev:   d.Severity,
		file:  d.Primary.File,
		start: d.Primary.Start,
		end:   d.Primary.End,
		msg:   d.Message,
	}
	if _, ok := r.seen[key]; ok {
		return
	}
	r.seen[key] = struct{}{}
	emitTo(r.next, d)
}
