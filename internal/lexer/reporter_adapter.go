package lexer

import "surge/internal/diag"

// ReporterAdapter адаптирует diag.Reporter для использования в лексере
type ReporterAdapter struct {
	Bag *diag.Bag
}

// Reporter returns a diag.Reporter that forwards diagnostics to the adapter's bag.
func (r *ReporterAdapter) Reporter() diag.Reporter {
	return &diag.BagReporter{Bag: r.Bag}
}
