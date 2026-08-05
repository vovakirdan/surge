package diag

const (
	// SemaCloneHookConflict marks a type whose canonical `__clone` implementation
	// is not unique: two or more equally specific declarations remain after the
	// deterministic specificity ranking.
	SemaCloneHookConflict Code = 3185
	// SemaCloneHookNotVisible marks a use site that cannot access the canonical
	// `__clone` implementation chosen for its type. Selection is program-wide, so
	// a less specific but visible declaration is never substituted.
	SemaCloneHookNotVisible Code = 3186
)

// cloneCodeDescriptions holds the human-readable titles for clone canonicality
// diagnostics. Merged into codeDescription by init() so Title()/String() resolve
// them like any other code.
var cloneCodeDescriptions = map[Code]string{
	SemaCloneHookConflict:   "a type must have exactly one canonical `__clone` implementation",
	SemaCloneHookNotVisible: "the canonical `__clone` implementation is not visible from this module",
}

func init() {
	for c, d := range cloneCodeDescriptions {
		codeDescription[c] = d
	}
}
