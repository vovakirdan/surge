package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/symbols"
)

func (tc *typeChecker) reportAliasMagicRedeclared(
	fn *ast.FnItem,
	aliasKey symbols.TypeKey,
	targetLabel string,
	name string,
	rival *magicDeclaration,
) {
	aliasLabel := typeKeyLabel(aliasKey)
	primary := fn.NameSpan
	if primary.End <= primary.Start {
		primary = fn.Span
	}
	b := diag.ReportError(tc.reporter, diag.SemaAliasMagicRedeclared, primary, fmt.Sprintf(
		"delete `%s` from `extern<%s>`: `%s` is another name for `%s`, which already declares `%s`",
		name, aliasLabel, aliasLabel, targetLabel, name,
	))
	if b == nil {
		return
	}
	if rival.compilerProvided {
		b.WithNote(primary, fmt.Sprintf(
			"the compiler supplies `%s` for `%s` itself, for every value of that type",
			name, targetLabel,
		))
	} else {
		b.WithNote(rival.span, fmt.Sprintf(
			"`%s` for `%s` is declared here in module %q",
			name, targetLabel, rival.modulePath,
		))
	}
	b.WithNote(primary, fmt.Sprintf(
		"an alias is transparent: `%s` and `%s` are one type, so one body has to serve both",
		aliasLabel, targetLabel,
	))
	b.WithNote(primary, fmt.Sprintf(
		"`%s` runs through a receiver spelling the compiler picks, never one written at the call site, "+
			"so with two bodies there is no way for you to say which one runs",
		name,
	))
	b.WithHelp(primary, fmt.Sprintf(
		"keep the body you want on `%s`, or give `%s` a type of its own instead of aliasing `%s`",
		targetLabel, aliasLabel, targetLabel,
	))
	if edit := aliasMagicRemovalFix(fn, name, aliasLabel); edit != nil {
		b.WithFixSuggestion(edit)
	}
	b.Emit()
}

type aliasMagicRemovalThunk struct {
	id    string
	title string
	span  source.Span
}

// aliasMagicRemovalFix deletes the alias's declaration, leaving the target's
// body to serve both spellings.
//
// It is offered for manual review rather than as an always-safe edit, because
// whether it preserves behaviour is a question only the author can answer:
// values spelled with the alias run this body today and would run the target's
// afterwards. When the two bodies agree that is invisible; when they disagree
// it is the whole point. `surge fix --all` therefore leaves it alone, and
// applying it by id is a deliberate act.
func aliasMagicRemovalFix(fn *ast.FnItem, name, aliasLabel string) *diag.Fix {
	span := fn.Span
	if span.End <= span.Start {
		return nil
	}
	id := fix.MakeFixID(diag.SemaAliasMagicRedeclared, span)
	title := fmt.Sprintf("delete `%s` from `extern<%s>`", name, aliasLabel)
	return &diag.Fix{
		ID:            id,
		Title:         title,
		Kind:          diag.FixKindQuickFix,
		Applicability: diag.FixApplicabilityManualReview,
		Thunk:         aliasMagicRemovalThunk{id: id, title: title, span: span},
	}
}

// ID identifies the guarded declaration removal.
func (t aliasMagicRemovalThunk) ID() string { return t.id }

// Build captures the declaration's current text as the guard, so a file that
// moved on since the diagnostic was produced refuses the edit instead of
// cutting an arbitrary range out of it.
func (t aliasMagicRemovalThunk) Build(ctx diag.FixBuildContext) (diag.Fix, error) {
	if ctx.FileSet == nil || !ctx.FileSet.HasFile(t.span.File) {
		return diag.Fix{}, fmt.Errorf("alias magic method fix: source file is unavailable")
	}
	file := ctx.FileSet.Get(t.span.File)
	if file == nil || t.span.Start > t.span.End || uint64(t.span.End) > uint64(len(file.Content)) {
		return diag.Fix{}, fmt.Errorf("alias magic method fix: invalid source range")
	}
	guard := string(file.Content[t.span.Start:t.span.End])
	return diag.Fix{
		ID:            t.id,
		Title:         t.title,
		Kind:          diag.FixKindQuickFix,
		Applicability: diag.FixApplicabilityManualReview,
		Edits:         []diag.TextEdit{{Span: t.span, OldText: guard}},
	}, nil
}
