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
	// I/O primitives
	"rt_write_stdout",
	"rt_read_stdin",
	// String access
	"rt_string_ptr",
	"rt_string_len",
	"rt_string_from_bytes",
	// High-level I/O
	"readline",
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
	"__len",
	"__to",
	"__is",
	"__heir",
	"exit",
	"default",
	// Type introspection
	"size_of",
	"align_of",
	// Concurrency primitives (Mutex, RwLock, Condition, Semaphore)
	"new",
	"lock",
	"unlock",
	"try_lock",
	"read_lock",
	"read_unlock",
	"write_lock",
	"write_unlock",
	"try_read_lock",
	"try_write_lock",
	// Condition
	"new",
	"wait",
	"notify_one",
	"notify_all",
	// Semaphore
	"acquire",
	"release",
	"try_acquire",
	// Task utilities
	"checkpoint",
	// Channel operations
	"send",
	"recv",
	"try_send",
	"try_recv",
	"close",
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
	// Allow @intrinsic in any core/ or stdlib/ module for flexibility
	trimmed := strings.Trim(fr.modulePath, "/")
	if trimmed == "core" || strings.HasPrefix(trimmed, "core/") {
		return true
	}
	if trimmed == "stdlib" || strings.HasPrefix(trimmed, "stdlib/") {
		return true
	}
	// Also check file path for flexibility
	if fr.filePath != "" {
		path := filepath.ToSlash(fr.filePath)
		path = strings.TrimSuffix(path, ".sg")
		if strings.Contains(path, "/core") || strings.Contains(path, "/stdlib") {
			return true
		}
	}
	return false
}

func isProtectedModule(path string) bool {
	if path == "" {
		return false
	}
	trimmed := strings.Trim(path, "/")
	return trimmed == "core" || strings.HasPrefix(trimmed, "core/") || trimmed == "stdlib" || strings.HasPrefix(trimmed, "stdlib/")
}
