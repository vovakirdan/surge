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
	if file == nil {
		return
	}
	srcFileID := file.Span.File

	errs := errorsInFile(bag, srcFileID)
	emitted := make(map[diag.Code]struct{}, 6)

	if len(errs) > 0 && file.DialectEvidence != nil {
		classification := (dialect.Classifier{}).Classify(file.DialectEvidence)
		if alienHintsEligible(classification) {
			switch classification.Kind {
			case dialect.Rust:
				maybeEmitAlienHint(emitted, opts.Reporter, diag.AlnRustImplTrait, file.DialectEvidence, errs, isRustImplTraitHint, rustImplTraitMessage)
				maybeEmitAlienHint(emitted, opts.Reporter, diag.AlnRustAttribute, file.DialectEvidence, errs, isRustAttributeHint, rustAttributeMessage)
				maybeEmitAlienHint(emitted, opts.Reporter, diag.AlnRustMacroCall, file.DialectEvidence, errs, isRustMacroHint, rustMacroMessage)
				maybeEmitAlienHint(emitted, opts.Reporter, diag.AlnRustImplicitRet, file.DialectEvidence, errs, isRustImplicitReturnHint, rustImplicitReturnMessage)
			case dialect.Go:
				maybeEmitAlienHint(emitted, opts.Reporter, diag.AlnGoDefer, file.DialectEvidence, errs, isGoDeferHint, goDeferMessage)
			case dialect.TypeScript:
				maybeEmitAlienHint(emitted, opts.Reporter, diag.AlnTSInterface, file.DialectEvidence, errs, isTSInterfaceHint, tsInterfaceMessage)
			case dialect.Python:
				maybeEmitAlienHintPythonNoneType(emitted, opts.Reporter, file.DialectEvidence, errs)
			}
		}
	}

	maybeEmitAlienHintPythonNoneAlias(emitted, opts.Reporter, builder, file, errs)
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
	if c.Kind == dialect.Unknown {
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

func alienHintThreshold(kind dialect.Kind) int {
	switch kind {
	case dialect.Rust:
		return 6
	case dialect.Go:
		return 5
	case dialect.TypeScript:
		return 4
	case dialect.Python:
		return 4
	default:
		return 100
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
	if h.Dialect != dialect.Rust {
		return false
	}
	return strings.Contains(h.Reason, "`impl`") || strings.Contains(h.Reason, "`trait`")
}

func rustImplTraitMessage(dialect.Hint) string {
	return dialect.RenderAlienHint(dialect.Rust, dialect.RenderInput{
		Kind:         dialect.AlienHintImplTrait,
		Detected:     "`impl` / `trait` syntax",
		SurgeExample: "contract Derive<T> { fn derive(self: T) -> string; } extern<Foo> { fn derive(self: &Foo) -> string { return \"\"; } }",
	})
}

func isRustAttributeHint(h dialect.Hint) bool {
	return h.Dialect == dialect.Rust && strings.Contains(h.Reason, "`#[...]`")
}

func rustAttributeMessage(dialect.Hint) string {
	return dialect.RenderAlienHint(dialect.Rust, dialect.RenderInput{
		Kind:         dialect.AlienHintAttribute,
		Detected:     "attribute syntax `#[...]`",
		SurgeExample: "@align(8) type Foo = { x: int };",
	})
}

func isRustMacroHint(h dialect.Hint) bool {
	if h.Dialect != dialect.Rust {
		return false
	}
	return strings.Contains(h.Reason, "rust macro call")
}

func rustMacroMessage(h dialect.Hint) string {
	if strings.Contains(h.Reason, "`println!`") {
		return dialect.RenderAlienHint(dialect.Rust, dialect.RenderInput{
			Kind:         dialect.AlienHintMacroCall,
			Detected:     "macro call `println!(...)`",
			SurgeExample: "print(\"hi\");",
		})
	}
	return dialect.RenderAlienHint(dialect.Rust, dialect.RenderInput{
		Kind:         dialect.AlienHintMacroCall,
		Detected:     "macro call syntax `name!(...)`",
		SurgeExample: "name();",
	})
}

func isRustImplicitReturnHint(h dialect.Hint) bool {
	if h.Dialect != dialect.Rust {
		return false
	}
	return strings.Contains(h.Reason, "implicit return")
}

func rustImplicitReturnMessage(dialect.Hint) string {
	return dialect.RenderAlienHint(dialect.Rust, dialect.RenderInput{
		Kind:         dialect.AlienHintImplicitReturn,
		Detected:     "implicit return without ';'",
		SurgeExample: "fn foo() -> int { return 1; }",
	})
}

func isGoDeferHint(h dialect.Hint) bool {
	return h.Dialect == dialect.Go && strings.Contains(h.Reason, "`defer`")
}

func goDeferMessage(dialect.Hint) string {
	return dialect.RenderAlienHint(dialect.Go, dialect.RenderInput{
		Kind:         dialect.AlienHintGoDefer,
		Detected:     "`defer`",
		SurgeExample: "@raii type Resource = { handle: int };",
	})
}

func isTSInterfaceHint(h dialect.Hint) bool {
	return h.Dialect == dialect.TypeScript && strings.Contains(h.Reason, "`interface`")
}

func tsInterfaceMessage(dialect.Hint) string {
	return dialect.RenderAlienHint(dialect.TypeScript, dialect.RenderInput{
		Kind:         dialect.AlienHintTSInterface,
		Detected:     "`interface` / `extends`",
		SurgeExample: "contract FooLike<T> { fn foo(self: T) -> int; } type Foo = { foo: int };",
	})
}

func maybeEmitAlienHintPythonNoneType(emitted map[diag.Code]struct{}, reporter diag.Reporter, e *dialect.Evidence, errs []*diag.Diagnostic) {
	if reporter == nil || e == nil {
		return
	}
	if _, ok := emitted[diag.AlnPythonNoneType]; ok {
		return
	}
	hint, ok := firstHint(e, func(h dialect.Hint) bool {
		return h.Dialect == dialect.Python && strings.Contains(h.Reason, "`None`")
	})
	if !ok {
		return
	}
	if !anyUnknownTypeNoneError(errs, hint.Span) {
		return
	}
	msg := dialect.RenderAlienHint(dialect.Python, dialect.RenderInput{
		Kind:         dialect.AlienHintPythonNoneType,
		Detected:     "`None`",
		SurgeExample: "fn foo() -> nothing { return; }",
	})
	diag.ReportInfo(reporter, diag.AlnPythonNoneType, hint.Span, msg).Emit()
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

func maybeEmitAlienHintPythonNoneAlias(
	emitted map[diag.Code]struct{},
	reporter diag.Reporter,
	builder *ast.Builder,
	file *ast.File,
	errs []*diag.Diagnostic,
) {
	if reporter == nil || builder == nil || file == nil {
		return
	}
	if _, ok := emitted[diag.AlnPythonNoneAlias]; ok {
		return
	}
	if len(errs) != 0 {
		return
	}
	if !hasTypeAliasNoneToNothing(builder, file) {
		return
	}
	span, ok := firstExplicitReturnTypeSpan(builder, file, "None")
	if !ok {
		return
	}
	msg := dialect.RenderAlienHint(dialect.Python, dialect.RenderInput{
		Kind:         dialect.AlienHintPythonNoneAlias,
		Detected:     "`None` alias",
		SurgeExample: "fn foo() -> nothing { return; }",
	})
	diag.ReportInfo(reporter, diag.AlnPythonNoneAlias, span, msg).Emit()
	emitted[diag.AlnPythonNoneAlias] = struct{}{}
}

func hasTypeAliasNoneToNothing(builder *ast.Builder, file *ast.File) bool {
	if builder == nil || builder.Items == nil || builder.Types == nil || builder.StringsInterner == nil || file == nil {
		return false
	}
	for _, itemID := range file.Items {
		typeItem, ok := builder.Items.Type(itemID)
		if !ok || typeItem.Kind != ast.TypeDeclAlias {
			continue
		}
		name, ok := builder.StringsInterner.Lookup(typeItem.Name)
		if !ok || name != "None" {
			continue
		}
		alias := builder.Items.TypeAlias(typeItem)
		if alias == nil {
			continue
		}
		if typeExprIsBarePath(builder, alias.Target, "nothing") {
			return true
		}
	}
	return false
}

func firstExplicitReturnTypeSpan(builder *ast.Builder, file *ast.File, typeName string) (source.Span, bool) {
	if builder == nil || builder.Items == nil || builder.Types == nil || file == nil {
		return source.Span{}, false
	}
	for _, itemID := range file.Items {
		fn, ok := builder.Items.Fn(itemID)
		if !ok || fn == nil {
			continue
		}
		if fn.ReturnSpan == (source.Span{}) {
			continue
		}
		if !typeExprIsBarePath(builder, fn.ReturnType, typeName) {
			continue
		}
		if typeExpr := builder.Types.Get(fn.ReturnType); typeExpr != nil {
			return typeExpr.Span, true
		}
		return fn.ReturnSpan, true
	}
	return source.Span{}, false
}

func typeExprIsBarePath(builder *ast.Builder, typeID ast.TypeID, name string) bool {
	if builder == nil || builder.Types == nil || builder.StringsInterner == nil || typeID == ast.NoTypeID {
		return false
	}
	path, ok := builder.Types.Path(typeID)
	if !ok || len(path.Segments) != 1 {
		return false
	}
	seg := path.Segments[0]
	if len(seg.Generics) != 0 {
		return false
	}
	segName, ok := builder.StringsInterner.Lookup(seg.Name)
	return ok && segName == name
}
