package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/dialect"
	"surge/internal/source"
)

const (
	alienHintRustThreshold       = 6
	alienHintGoThreshold         = 5
	alienHintTypeScriptThreshold = 5
	alienHintPythonThreshold     = 4

	alienHintDominanceMargin = 2
)

func emitAlienHints(builder *ast.Builder, fileID ast.FileID, opts Options) {
	if builder == nil || fileID == ast.NoFileID {
		return
	}
	if !opts.AlienHints || opts.Reporter == nil {
		return
	}
	bag := opts.Bag
	if bag == nil {
		if br, ok := opts.Reporter.(*diag.BagReporter); ok {
			bag = br.Bag
		}
	}
	if bag == nil {
		return
	}

	file := builder.Files.Get(fileID)
	if file == nil || file.DialectEvidence == nil {
		return
	}
	srcFileID := file.Span.File

	errs := errorsInFile(bag, srcFileID)
	if len(errs) == 0 {
		return
	}

	classification := (dialect.Classifier{}).Classify(file.DialectEvidence)
	if !alienHintsEligible(classification) {
		return
	}

	emitted := make(map[diag.Code]struct{}, 4)
	switch classification.Kind {
	case dialect.DialectRust:
		maybeEmitAlienHint(emitted, opts.Reporter, diag.AlnRustImplTrait, file.DialectEvidence, errs, isRustImplTraitHint, rustImplTraitMessage)
		maybeEmitAlienHint(emitted, opts.Reporter, diag.AlnRustAttribute, file.DialectEvidence, errs, isRustAttributeHint, rustAttributeMessage)
		maybeEmitAlienHint(emitted, opts.Reporter, diag.AlnRustMacroCall, file.DialectEvidence, errs, isRustMacroHint, rustMacroMessage)
	case dialect.DialectGo:
		maybeEmitAlienHint(emitted, opts.Reporter, diag.AlnGoDefer, file.DialectEvidence, errs, isGoDeferHint, goDeferMessage)
	case dialect.DialectTypeScript:
		maybeEmitAlienHint(emitted, opts.Reporter, diag.AlnTSInterface, file.DialectEvidence, errs, isTSInterfaceHint, tsInterfaceMessage)
	case dialect.DialectPython:
		maybeEmitAlienHintPythonNoneType(emitted, opts.Reporter, file.DialectEvidence, errs)
	}
}

func errorsInFile(bag *diag.Bag, fileID source.FileID) []*diag.Diagnostic {
	if bag == nil {
		return nil
	}
	items := bag.Items()
	if len(items) == 0 {
		return nil
	}
	out := make([]*diag.Diagnostic, 0, len(items))
	for _, d := range items {
		if d == nil || d.Severity < diag.SevError {
			continue
		}
		if d.Primary.File != fileID {
			continue
		}
		out = append(out, d)
	}
	return out
}

func alienHintsEligible(c dialect.Classification) bool {
	if c.Kind == dialect.DialectUnknown {
		return false
	}
	threshold := alienHintThreshold(c.Kind)
	if threshold == 0 || c.Score < threshold {
		return false
	}
	if c.RunnerUpScore > 0 && c.Score < c.RunnerUpScore+alienHintDominanceMargin {
		return false
	}
	return true
}

func alienHintThreshold(kind dialect.DialectKind) int {
	switch kind {
	case dialect.DialectRust:
		return alienHintRustThreshold
	case dialect.DialectGo:
		return alienHintGoThreshold
	case dialect.DialectTypeScript:
		return alienHintTypeScriptThreshold
	case dialect.DialectPython:
		return alienHintPythonThreshold
	default:
		return 0
	}
}

func spansOverlap(a, b source.Span) bool {
	if a.File != b.File {
		return false
	}
	if a == (source.Span{}) || b == (source.Span{}) {
		return false
	}
	return a.Start < b.End && b.Start < a.End
}

func anyErrorOverlaps(errs []*diag.Diagnostic, sp source.Span) bool {
	for _, d := range errs {
		if d == nil {
			continue
		}
		if spansOverlap(d.Primary, sp) {
			return true
		}
	}
	return false
}

type hintPredicate func(dialect.Hint) bool

func maybeEmitAlienHint(
	emitted map[diag.Code]struct{},
	reporter diag.Reporter,
	code diag.Code,
	e *dialect.Evidence,
	errs []*diag.Diagnostic,
	match hintPredicate,
	message func(dialect.Hint) string,
) {
	if reporter == nil || e == nil {
		return
	}
	if _, ok := emitted[code]; ok {
		return
	}
	hint, ok := firstHint(e, match)
	if !ok {
		return
	}
	if !anyErrorOverlaps(errs, hint.Span) {
		return
	}
	msg := message(hint)
	if msg == "" {
		return
	}
	diag.ReportInfo(reporter, code, hint.Span, msg).Emit()
	emitted[code] = struct{}{}
}

func firstHint(e *dialect.Evidence, match hintPredicate) (dialect.Hint, bool) {
	if e == nil || match == nil {
		return dialect.Hint{}, false
	}
	for _, h := range e.Hints() {
		if match(h) {
			return h, true
		}
	}
	return dialect.Hint{}, false
}

func isRustImplTraitHint(h dialect.Hint) bool {
	if h.Dialect != dialect.DialectRust {
		return false
	}
	return strings.Contains(h.Reason, "`impl`") || strings.Contains(h.Reason, "`trait`")
}

func rustImplTraitMessage(dialect.Hint) string {
	return "Looks like Rust `impl`/`trait` syntax. In Surge, use `contract` + `extern<T>`."
}

func isRustAttributeHint(h dialect.Hint) bool {
	return h.Dialect == dialect.DialectRust && strings.Contains(h.Reason, "`#[...]`")
}

func rustAttributeMessage(dialect.Hint) string {
	return "Rust attribute syntax `#[...]` detected. Surge uses `@name(args)` (e.g. `@align(8)`)."
}

func isRustMacroHint(h dialect.Hint) bool {
	if h.Dialect != dialect.DialectRust {
		return false
	}
	return strings.Contains(h.Reason, "rust macro call")
}

func rustMacroMessage(h dialect.Hint) string {
	if strings.Contains(h.Reason, "`println!`") {
		return "Rust macro call `println!(...)` detected. In Surge, use `print(...)`."
	}
	return "Rust macro call syntax `ident!(...)` detected. Surge doesn’t have `!` macros; try a normal call `name(...)`."
}

func isGoDeferHint(h dialect.Hint) bool {
	return h.Dialect == dialect.DialectGo && strings.Contains(h.Reason, "`defer`")
}

func goDeferMessage(dialect.Hint) string {
	return "Go `defer` detected. Surge doesn’t have `defer`; use explicit scope/cleanup (or a `@raii` type)."
}

func isTSInterfaceHint(h dialect.Hint) bool {
	return h.Dialect == dialect.DialectTypeScript && strings.Contains(h.Reason, "`interface`")
}

func tsInterfaceMessage(dialect.Hint) string {
	return "TypeScript `interface`/`extends` style detected. In Surge, use `contract` (structural) and `type Foo = { ... };` for data."
}

func maybeEmitAlienHintPythonNoneType(emitted map[diag.Code]struct{}, reporter diag.Reporter, e *dialect.Evidence, errs []*diag.Diagnostic) {
	if reporter == nil || e == nil {
		return
	}
	if _, ok := emitted[diag.AlnPythonNoneType]; ok {
		return
	}
	hint, ok := firstHint(e, func(h dialect.Hint) bool {
		return h.Dialect == dialect.DialectPython && strings.Contains(h.Reason, "`None`")
	})
	if !ok {
		return
	}
	if !anyUnknownTypeNoneError(errs, hint.Span) {
		return
	}
	diag.ReportInfo(reporter, diag.AlnPythonNoneType, hint.Span, "Python `None` used as a type. In Surge, the absence type/value is `nothing` (or `type None = nothing;`).").Emit()
	emitted[diag.AlnPythonNoneType] = struct{}{}
}

func anyUnknownTypeNoneError(errs []*diag.Diagnostic, sp source.Span) bool {
	for _, d := range errs {
		if d == nil {
			continue
		}
		if d.Code != diag.SemaUnresolvedSymbol {
			continue
		}
		if !spansOverlap(d.Primary, sp) {
			continue
		}
		if strings.Contains(d.Message, "unknown type None") {
			return true
		}
	}
	return false
}
