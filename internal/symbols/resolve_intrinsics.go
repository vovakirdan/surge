package symbols

import (
	"fmt"
	"path/filepath"
	"strings"

	"surge/internal/diag"
	"surge/internal/source"
)

var intrinsicAllowedNamesList = []string{
	"rt_alloc",
	"rt_free",
	"rt_realloc",
	"rt_memcpy",
	"rt_memmove",
	"next",
	"await",
	"__abs",
	"__add",
	"__sub",
	"__mul",
	"__div",
	"__mod",
	"__index",
	"__index_set",
	"__range",
	"__bit_and",
	"__bit_or",
	"__bit_xor",
	"__shl",
	"__shr",
	"__lt",
	"__le",
	"__eq",
	"__ne",
	"__ge",
	"__gt",
	"__pos",
	"__neg",
	"__not",
	"__min_value",
	"__max_value",
	"__to",
	"__is",
	"__heir",
	"exit",
	"default",
}

var (
	intrinsicAllowedNames = func() map[string]struct{} {
		m := make(map[string]struct{}, len(intrinsicAllowedNamesList))
		for _, name := range intrinsicAllowedNamesList {
			m[name] = struct{}{}
		}
		return m
	}()
	intrinsicAllowedNamesDisplay = strings.Join(intrinsicAllowedNamesList, ", ")
)

func (fr *fileResolver) intrinsicNameAllowed(name source.StringID) bool {
	if name == source.NoStringID || fr.builder == nil || fr.builder.StringsInterner == nil {
		return false
	}
	nameStr := fr.builder.StringsInterner.MustLookup(name)
	_, ok := intrinsicAllowedNames[nameStr]
	return ok
}

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
	if isCoreIntrinsicsModule(fr.modulePath) {
		return true
	}
	if strings.Trim(fr.modulePath, "/") == "core/task" {
		return true
	}
	if fr.filePath == "" {
		return false
	}
	path := filepath.ToSlash(fr.filePath)
	path = strings.TrimSuffix(path, ".sg")
	path = strings.TrimSuffix(path, "/")
	if strings.HasSuffix(path, "/core") || strings.HasSuffix(path, "/core/intrinsics") {
		return true
	}
	return path == "core" || path == "core/intrinsics"
}

func isCoreIntrinsicsModule(path string) bool {
	if path == "" {
		return false
	}
	trimmed := strings.Trim(path, "/")
	return trimmed == "core" || trimmed == "core/intrinsics"
}

func isProtectedModule(path string) bool {
	if path == "" {
		return false
	}
	trimmed := strings.Trim(path, "/")
	return trimmed == "core" || strings.HasPrefix(trimmed, "core/") || trimmed == "stdlib" || strings.HasPrefix(trimmed, "stdlib/")
}
