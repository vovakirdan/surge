package diag

const (
	// SemaEntrypointStdinArity indicates that stdin startup does not have exactly one parameter.
	SemaEntrypointStdinArity Code = 3183
	// SemaEntrypointStdinDefault indicates a default on the stdin parameter.
	SemaEntrypointStdinDefault Code = 3184
)

// entrypointCodeDescriptions holds the human-readable titles for stdin startup
// diagnostics. Merged into codeDescription by init() so Title()/String() resolve
// them like any other code.
var entrypointCodeDescriptions = map[Code]string{
	SemaEntrypointStdinArity:   "@entrypoint('stdin') requires exactly one parameter",
	SemaEntrypointStdinDefault: "@entrypoint('stdin') parameter cannot have a default",
}

func init() {
	for c, d := range entrypointCodeDescriptions {
		codeDescription[c] = d
	}
}
