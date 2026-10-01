package sema

import (
	"fmt"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

// exitCodeConversionHelp explains why an Option or Erring value is not an int.
// Their exit code is the ExitCode contract's `__exit_code()`, which the
// generated entry calls on main's result; it is deliberately not a `__to(int)`
// conversion, so `let x: int = o` and `o to int` stay ordinary type errors.
func (tc *typeChecker) exitCodeConversionHelp(from, target types.TypeID) (string, bool) {
	if tc == nil || tc.types == nil || from == types.NoTypeID || tc.resolveAlias(target) != tc.types.Builtins().Int {
		return "", false
	}
	if _, ok := tc.optionPayload(from); ok {
		return "an Option is not an int: read the payload with `compare` (`Some(v) => v`) or `.safe()`; " +
			"`.__exit_code()` gives its process exit code (Some => 0, nothing => 1)", true
	}
	if _, _, ok := tc.resultPayload(from); ok {
		return "an Erring is not an int: read the value with `compare` (`Success(v) => v`) or `.safe()`; " +
			"`.__exit_code()` gives its process exit code (Success => 0, error => its code)", true
	}
	return "", false
}

// reportMismatchToInt reports a SemaTypeMismatch and, when the value is an
// Option or Erring expected as an int, says why no conversion exists.
func (tc *typeChecker) reportMismatchToInt(span source.Span, from, target types.TypeID, format string, args ...any) {
	help, ok := tc.exitCodeConversionHelp(from, target)
	if !ok {
		tc.report(diag.SemaTypeMismatch, span, format, args...)
		return
	}
	if tc.reporter == nil {
		return
	}
	if b := diag.ReportError(tc.reporter, diag.SemaTypeMismatch, span, fmt.Sprintf(format, args...)); b != nil {
		b.WithHelp(span, help)
		b.Emit()
	}
}
