package diag

const (
	// SemaEntrypointStdinArity indicates that stdin startup does not have exactly one parameter.
	SemaEntrypointStdinArity Code = 3183
	// SemaEntrypointStdinDefault indicates a default on the stdin parameter.
	SemaEntrypointStdinDefault Code = 3184
	// SemaEntrypointValue refuses source references to a synchronous root entrypoint.
	SemaEntrypointValue Code = 3226
)
