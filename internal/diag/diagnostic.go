package diag

import "surge/internal/source"

// Note provides auxiliary context for a diagnostic message.
type Note struct {
	Span source.Span
	Msg  string
}

// TextEdit describes a textual change that can be applied to a source file.
// - Insertion: Span.Start == Span.End, NewText != "", OldText is optional guard.
// - Deletion:  Span.Start < Span.End, NewText == "", OldText is optional guard.
// - Replace:   Span.Start < Span.End, NewText != "", OldText is optional guard.
type TextEdit struct {
	Span    source.Span
	NewText string
	OldText string
}

// FixEdit is kept for transitional compatibility with older call sites.
// It aliases TextEdit and should be considered deprecated.
type FixEdit = TextEdit

// FixApplicability communicates how safe it is to apply a fix automatically.
type FixApplicability uint8

const (
	FixApplicabilityAlwaysSafe FixApplicability = iota
	FixApplicabilitySafeWithHeuristics
	FixApplicabilityManualReview
)

func (a FixApplicability) String() string {
	switch a {
	case FixApplicabilityAlwaysSafe:
		return "ALWAYS_SAFE"
	case FixApplicabilitySafeWithHeuristics:
		return "SAFE_WITH_HEURISTICS"
	case FixApplicabilityManualReview:
		return "MANUAL_REVIEW"
	default:
		return "UNKNOWN"
	}
}

// FixKind categorises the intent of a fix. Mirrors common LSP quick-fix kinds.
type FixKind uint8

const (
	FixKindQuickFix FixKind = iota
	FixKindRefactor
	FixKindRefactorRewrite
	FixKindSourceAction
)

func (k FixKind) String() string {
	switch k {
	case FixKindQuickFix:
		return "QUICK_FIX"
	case FixKindRefactor:
		return "REFACTOR"
	case FixKindRefactorRewrite:
		return "REFACTOR_REWRITE"
	case FixKindSourceAction:
		return "SOURCE_ACTION"
	default:
		return "UNKNOWN_KIND"
	}
}

// FixThunk allows deferring fix materialisation until formatting or application.
type FixThunk interface {
	ID() string
	Build(ctx FixBuildContext) (Fix, error)
}

// FixBuildContext supplies shared data needed to build lazy fixes.
type FixBuildContext struct {
	FileSet *source.FileSet
}

// Fix describes an actionable change that can repair a diagnostic.
type Fix struct {
	ID            string
	Title         string
	Kind          FixKind
	Applicability FixApplicability
	IsPreferred   bool
	Edits         []TextEdit
	Thunk         FixThunk
}

// Materialized reports whether the fix already contains concrete edits.
func (f Fix) Materialized() bool {
	return len(f.Edits) > 0
}

func (f Fix) ensureDefaults() Fix {
	if f.Kind > FixKindSourceAction {
		f.Kind = FixKindQuickFix
	}
	if f.Applicability > FixApplicabilityManualReview {
		f.Applicability = FixApplicabilityManualReview
	}
	return f
}

// Resolve materialises lazy fixes using provided context, inheriting defaults.
func (f Fix) Resolve(ctx FixBuildContext) (Fix, error) {
	if !f.Materialized() && f.Thunk != nil {
		built, err := f.Thunk.Build(ctx)
		if err != nil {
			return Fix{}, err
		}
		if built.ID == "" {
			built.ID = f.ID
		}
		if built.Title == "" {
			built.Title = f.Title
		}
		if built.Kind == 0 && f.Kind != 0 {
			built.Kind = f.Kind
		}
		if built.Applicability == 0 && f.Applicability != 0 {
			built.Applicability = f.Applicability
		}
		if f.IsPreferred {
			built.IsPreferred = true
		}
		return built.ensureDefaults(), nil
	}
	return f.ensureDefaults(), nil
}

// MaterializeFixes produces a slice of resolved fixes with lazy thunks expanded.
func MaterializeFixes(ctx FixBuildContext, fixes []Fix) ([]Fix, error) {
	if len(fixes) == 0 {
		return nil, nil
	}
	out := make([]Fix, len(fixes))
	for i := range fixes {
		resolved, err := fixes[i].Resolve(ctx)
		if err != nil {
			return nil, err
		}
		out[i] = resolved
	}
	return out, nil
}

// Diagnostic captures a single issue along with optional notes and fixes.
type Diagnostic struct {
	Severity Severity
	Code     Code
	Message  string
	Primary  source.Span
	Notes    []Note
	Fixes    []Fix
}
