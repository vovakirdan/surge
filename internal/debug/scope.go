package debug

import "strings"

// Helpers for canonical scope naming.
// You can keep using raw strings, but these reduce typos.

const (
	ScopeSemaConstEval = "sema.const_eval"
)

// Fn scopes: sema.const_eval.ensureConstEvaluated
func Fn(base string, fn string) string {
	if base == "" {
		return strings.ToLower(fn)
	}
	return base + "." + strings.ToLower(fn)
}
