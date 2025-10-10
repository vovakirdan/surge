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

func (d Diagnostic) WithFix(title string, edits ...FixEdit) Diagnostic {
	d.Fixes = append(d.Fixes, Fix{Title: title, Edits: edits})
	return d
}
