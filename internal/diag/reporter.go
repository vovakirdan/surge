package diag

import "surge/internal/source"

// Reporter — минимальный контракт получения диагностик от фаз.
// Реализации: BagReporter (кладёт в Bag), NopReporter, MultiReporter (fan-out).
type Reporter interface {
	Report(code Code, sev Severity, primary source.Span, msg string, notes []Note, fixes []*Fix)
}

// DiagnosticReporter is the structured half of the contract, implemented by
// every reporter that forwards or stores whole diagnostics.
//
// Report's parameter list predates the Help channel, and widening it would have
// meant `nil` at twenty-eight call sites that have no help to give. A reporter
// that accepts the whole diagnostic never has to be widened again; one that
// does not still receives everything the old signature could carry.
type DiagnosticReporter interface {
	Reporter
	ReportDiagnostic(d *Diagnostic)
}

// ForwardDiagnostic passes one diagnostic on through the richest channel the
// next reporter offers. It exists so a wrapper outside this package can chain
// without having to know which half of the contract its target implements.
func ForwardDiagnostic(next Reporter, d *Diagnostic) {
	emitTo(next, d)
}

// emitTo sends one diagnostic through the richest channel a reporter offers.
func emitTo(r Reporter, d *Diagnostic) {
	if r == nil || d == nil {
		return
	}
	if structured, ok := r.(DiagnosticReporter); ok {
		structured.ReportDiagnostic(d)
		return
	}
	r.Report(d.Code, d.Severity, d.Primary, d.Message, d.Notes, d.Fixes)
}

// ReportBuilder accumulates diagnostic details before emitting to Reporter.
type ReportBuilder struct {
	reporter Reporter
	diag     *Diagnostic
	emitted  bool
}

// NewReportBuilder constructs a builder bound to Reporter.
func NewReportBuilder(r Reporter, sev Severity, code Code, primary source.Span, msg string) *ReportBuilder {
	return &ReportBuilder{
		reporter: r,
		diag: &Diagnostic{
			Severity: sev,
			Code:     code,
			Message:  msg,
			Primary:  primary,
		},
	}
}

// ReportError is a shortcut for SevError diagnostics.
func ReportError(r Reporter, code Code, primary source.Span, msg string) *ReportBuilder {
	return NewReportBuilder(r, SevError, code, primary, msg)
}

// ReportWarning is a shortcut for SevWarning diagnostics.
func ReportWarning(r Reporter, code Code, primary source.Span, msg string) *ReportBuilder {
	return NewReportBuilder(r, SevWarning, code, primary, msg)
}

// ReportInfo is a shortcut for SevInfo diagnostics.
func ReportInfo(r Reporter, code Code, primary source.Span, msg string) *ReportBuilder {
	return NewReportBuilder(r, SevInfo, code, primary, msg)
}

// WithNote appends a note to diagnostic.
func (b *ReportBuilder) WithNote(sp source.Span, msg string) *ReportBuilder {
	if b == nil {
		return nil
	}
	b.diag.Notes = append(b.diag.Notes, Note{Span: sp, Msg: msg})
	return b
}

// WithHelp appends an actionable way out.
func (b *ReportBuilder) WithHelp(sp source.Span, msg string) *ReportBuilder {
	if b == nil {
		return nil
	}
	b.diag.Help = append(b.diag.Help, Note{Span: sp, Msg: msg})
	return b
}

// WithFix appends ready-to-use fix with default metadata.
func (b *ReportBuilder) WithFix(title string, edits ...FixEdit) *ReportBuilder {
	if b == nil {
		return nil
	}
	b.diag = b.diag.WithFix(title, edits...)
	return b
}

// WithFixSuggestion appends configured fix (materialised or lazy).
func (b *ReportBuilder) WithFixSuggestion(fix *Fix) *ReportBuilder {
	if b == nil {
		return nil
	}
	b.diag = b.diag.WithFixSuggestion(fix)
	return b
}

// Emit sends diagnostic to underlying reporter exactly once.
func (b *ReportBuilder) Emit() {
	if b == nil || b.emitted {
		return
	}
	emitTo(b.reporter, b.diag)
	b.emitted = true
}

// Diagnostic returns accumulated diagnostic without emitting.
func (b *ReportBuilder) Diagnostic() *Diagnostic {
	if b == nil {
		return &Diagnostic{}
	}
	return b.diag
}

// BagReporter — адаптер, который пишет в *Bag.
type BagReporter struct{ Bag *Bag }

// Report adds a diagnostic to the bag.
func (r BagReporter) Report(code Code, sev Severity, primary source.Span, msg string, notes []Note, fixes []*Fix) {
	r.ReportDiagnostic(&Diagnostic{
		Severity: sev, Code: code, Message: msg,
		Primary: primary, Notes: notes, Fixes: fixes,
	})
}

// ReportDiagnostic adds a whole diagnostic, Help included, to the bag.
func (r BagReporter) ReportDiagnostic(d *Diagnostic) {
	if r.Bag == nil || d == nil {
		return
	}
	r.Bag.Add(d)
}

// ReporterBag returns the underlying diagnostic bag for reporters that expose
// one directly or through a standard wrapper.
func ReporterBag(r Reporter) *Bag {
	switch rr := r.(type) {
	case BagReporter:
		return rr.Bag
	case *BagReporter:
		if rr == nil {
			return nil
		}
		return rr.Bag
	case *DedupReporter:
		if rr == nil {
			return nil
		}
		return ReporterBag(rr.next)
	default:
		return nil
	}
}
