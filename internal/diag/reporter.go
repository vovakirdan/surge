package diag

import "surge/internal/source"

// Reporter — минимальный контракт получения диагностик от фаз.
// Реализации: BagReporter (кладёт в Bag), NopReporter, MultiReporter (fan-out).
type Reporter interface {
    Report(code Code, sev Severity, primary source.Span, msg string, notes []Note, fixes []Fix)
}

// BagReporter — адаптер, который пишет в *Bag.
type BagReporter struct { Bag *Bag }

func (r BagReporter) Report(code Code, sev Severity, primary source.Span, msg string, notes []Note, fixes []Fix) {
    if r.Bag == nil { return }
    r.Bag.Add(Diagnostic{
        Severity: sev, Code: code, Message: msg,
        Primary: primary, Notes: notes, Fixes: fixes,
    })
}
