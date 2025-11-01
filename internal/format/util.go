package format

import "surge/internal/source"

func spanValid(sp source.Span) bool {
	return sp.File != 0 || sp.End > sp.Start
}
