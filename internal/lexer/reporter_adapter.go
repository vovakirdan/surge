package lexer

import "surge/internal/diag"

// ReporterAdapter адаптирует diag.Reporter для использования в лексере
type ReporterAdapter struct {
	Bag *diag.Bag
}

func (r *ReporterAdapter) Reporter() diag.Reporter {
	return &diag.BagReporter{Bag: r.Bag}
}
