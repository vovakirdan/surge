package symbols

import (
	"fmt"
	"strings"

	"surge/internal/diag"
	"surge/internal/source"
)

func (fr *fileResolver) reportIntrinsicError(name source.StringID, span source.Span, code diag.Code, detail string) {
	if fr.resolver == nil || fr.resolver.reporter == nil {
		return
	}
	if fr.builder == nil || fr.builder.StringsInterner == nil {
		return
	}
	nameStr := fr.builder.StringsInterner.MustLookup(name)
	msg := fmt.Sprintf("invalid intrinsic '%s': %s", nameStr, detail)
	if b := diag.ReportError(fr.resolver.reporter, code, span, msg); b != nil {
		b.Emit()
	}
}

func (fr *fileResolver) moduleAllowsIntrinsic() bool {
	// Allow @intrinsic everywhere - the attribute validation ensures
	// intrinsic functions have no body and intrinsic types are valid
	return true
}

func isProtectedModule(path string) bool {
	if path == "" {
		return false
	}
	trimmed := strings.Trim(path, "/")
	return trimmed == "core" || strings.HasPrefix(trimmed, "core/") || trimmed == "stdlib" || strings.HasPrefix(trimmed, "stdlib/")
}
