package diag

import "surge/internal/source"

// Reporter — минимальный контракт получения диагностик от фаз.
// Реализации: BagReporter (кладёт в Bag), NopReporter, MultiReporter (fan-out).
type Reporter interface {
	Report(code Code, sev Severity, primary source.Span, msg string, notes []Note, fixes []Fix)
}

// ReportBuilder accumulates diagnostic details before emitting to Reporter.
type ReportBuilder struct {
	reporter Reporter
	diag     Diagnostic
	emitted  bool
}

// NewReportBuilder constructs a builder bound to Reporter.
func NewReportBuilder(r Reporter, sev Severity, code Code, primary source.Span, msg string) *ReportBuilder {
	return &ReportBuilder{
		reporter: r,
		diag: Diagnostic{
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

// WithFix appends ready-to-use fix with default metadata.
func (b *ReportBuilder) WithFix(title string, edits ...FixEdit) *ReportBuilder {
	if b == nil {
		return nil
	}
	b.diag = b.diag.WithFix(title, edits...)
	return b
}

// WithFixSuggestion appends configured fix (materialised or lazy).
func (b *ReportBuilder) WithFixSuggestion(fix Fix) *ReportBuilder {
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
	if b.reporter != nil {
		b.reporter.Report(b.diag.Code, b.diag.Severity, b.diag.Primary, b.diag.Message, b.diag.Notes, b.diag.Fixes)
	}
	b.emitted = true
}

// Diagnostic returns accumulated diagnostic without emitting.
func (b *ReportBuilder) Diagnostic() Diagnostic {
	if b == nil {
		return Diagnostic{}
	}
	return b.diag
}

// BagReporter — адаптер, который пишет в *Bag.
type BagReporter struct{ Bag *Bag }

func (r BagReporter) Report(code Code, sev Severity, primary source.Span, msg string, notes []Note, fixes []Fix) {
	if r.Bag == nil {
		return
	}
	r.Bag.Add(Diagnostic{
		Severity: sev, Code: code, Message: msg,
		Primary: primary, Notes: notes, Fixes: fixes,
	})
}
