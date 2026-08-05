package sema

import (
	"fmt"
	"slices"
	"sort"

	"surge/internal/diag"
	"surge/internal/source"
)

// CloneCanonicalityError is a post-merge clone selection failure carrying the
// source diagnostic the user sees. The driver consumes the diagnostic at the
// finalization seam instead of surfacing an infrastructure error.
type CloneCanonicalityError struct {
	diagnostic *diag.Diagnostic
	cause      error
}

func (e *CloneCanonicalityError) Error() string {
	if e == nil {
		return "clone canonicality failed"
	}
	if e.cause != nil {
		return e.cause.Error()
	}
	if e.diagnostic != nil {
		return e.diagnostic.Message
	}
	return "clone canonicality failed"
}

func (e *CloneCanonicalityError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Diagnostic returns a detached diagnostic for the driver finalization seam.
func (e *CloneCanonicalityError) Diagnostic() *diag.Diagnostic {
	if e == nil || e.diagnostic == nil {
		return nil
	}
	clone := *e.diagnostic
	clone.Notes = slices.Clone(e.diagnostic.Notes)
	clone.Fixes = slices.Clone(e.diagnostic.Fixes)
	return &clone
}

func newCloneNotClonableError(site source.Span, typeLabel string) error {
	return &CloneCanonicalityError{diagnostic: &diag.Diagnostic{
		Severity: diag.SevError,
		Code:     diag.SemaTypeNotClonable,
		Primary:  site,
		Message:  fmt.Sprintf("type %s is not clonable (no __clone method defined)", typeLabel),
	}}
}

func newCloneInvalidSignatureError(site source.Span, typeLabel string) error {
	return &CloneCanonicalityError{diagnostic: &diag.Diagnostic{
		Severity: diag.SevError,
		Code:     diag.SemaTypeNotClonable,
		Primary:  site,
		Message:  fmt.Sprintf("type %s has __clone but with invalid signature", typeLabel),
	}}
}

func newCloneConflictError(rivals []GlobalCloneHook, site source.Span, typeLabel string) error {
	ordered := slices.Clone(rivals)
	sort.SliceStable(ordered, func(i, j int) bool { return compareCloneHookDeclarations(&ordered[i], &ordered[j]) < 0 })
	diagnostic := &diag.Diagnostic{
		Severity: diag.SevError,
		Code:     diag.SemaCloneHookConflict,
		Primary:  site,
		Message: fmt.Sprintf(
			"type %q has %d equally specific canonical __clone implementations; a type must have exactly one",
			typeLabel, len(ordered),
		),
	}
	for i := range ordered {
		diagnostic.Notes = append(diagnostic.Notes, diag.Note{
			Span: ordered[i].Decl,
			Msg:  fmt.Sprintf("__clone for %s is declared here in module %q", typeLabel, ordered[i].ModulePath),
		})
	}
	return &CloneCanonicalityError{diagnostic: diagnostic}
}

func newCloneNotVisibleError(hook *GlobalCloneHook, view CloneUseView, site source.Span, typeLabel string) error {
	// No alternative implementation is named: selection is program-wide, so a
	// visible but less specific declaration is not a candidate for this type.
	diagnostic := &diag.Diagnostic{
		Severity: diag.SevError,
		Code:     diag.SemaCloneHookNotVisible,
		Primary:  site,
		Message: fmt.Sprintf(
			"the canonical __clone implementation for %q is not visible from module %q",
			typeLabel, view.AccessModule,
		),
		Notes: []diag.Note{{
			Span: hook.Decl,
			Msg:  fmt.Sprintf("it is declared here in module %q", hook.ModulePath),
		}},
	}
	// The declaration span covers the method name, not the position a `pub`
	// keyword would occupy, so the edit is described rather than applied.
	if hook.FilePrivate && hook.SourceKey != view.SourceKey {
		diagnostic.Notes = append(diagnostic.Notes, diag.Note{
			Span: hook.Decl,
			Msg:  "help: this declaration is file-private; move it to a shared file or export it",
		})
	} else {
		diagnostic.Notes = append(diagnostic.Notes, diag.Note{
			Span: hook.Decl,
			Msg:  "help: declare it `pub` so every user of the type clones it the same way",
		})
	}
	return &CloneCanonicalityError{diagnostic: diagnostic}
}

func compareCloneHookDeclarations(left, right *GlobalCloneHook) int {
	if left.SourceKey != right.SourceKey {
		if left.SourceKey < right.SourceKey {
			return -1
		}
		return 1
	}
	if left.Decl.Start != right.Decl.Start {
		if left.Decl.Start < right.Decl.Start {
			return -1
		}
		return 1
	}
	if left.BodyKey == right.BodyKey {
		return 0
	}
	if left.BodyKey < right.BodyKey {
		return -1
	}
	return 1
}
