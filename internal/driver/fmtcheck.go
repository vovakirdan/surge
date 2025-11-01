package driver

import (
	"surge/internal/format"
	"surge/internal/source"
)

// RunFmtCheck formats and re-parses the file, validating the formatter keeps the
// top-level structure stable.
func RunFmtCheck(sf *source.File, maxDiagnostics int) (ok bool, msg string) {
	return format.CheckRoundTrip(sf, format.Options{}, maxDiagnostics)
}
