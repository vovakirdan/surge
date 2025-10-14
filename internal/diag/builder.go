package diag

import "surge/internal/source"

func New(sev Severity, code Code, primary source.Span, msg string) Diagnostic {
	return Diagnostic{
		Severity: sev,
		Code:     code,
		Primary:  primary,
		Message:  msg,
		Notes:    nil,
		Fixes:    nil,
	}
}

func NewError(code Code, primary source.Span, msg string) Diagnostic {
	return New(SevError, code, primary, msg)
}

func (d Diagnostic) WithNote(sp source.Span, msg string) Diagnostic {
	d.Notes = append(d.Notes, Note{Span: sp, Msg: msg})
	return d
}

// WithFix appends a ready-to-use fix with default metadata (quick fix, always safe).
func (d Diagnostic) WithFix(title string, edits ...FixEdit) Diagnostic {
	if d.Fixes == nil {
		d.Fixes = make([]Fix, 0, 1)
	}
	d.Fixes = append(d.Fixes, Fix{
		Title:         title,
		Kind:          FixKindQuickFix,
		Applicability: FixApplicabilityAlwaysSafe,
		Edits:         edits,
	})
	return d
}

// WithFixSuggestion appends a fully configured fix structure (materialised or lazy).
func (d Diagnostic) WithFixSuggestion(fix Fix) Diagnostic {
	d.Fixes = append(d.Fixes, fix)
	return d
}
