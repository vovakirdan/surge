package sema

// A result whose only non-input roots are expired locals is a refusal, not an
// unproved source: analyze closed every function exit at the function scope,
// and closeOutcome's checkExpired published SEM3139 for each such root at that
// exit, or its own Pending when the owner is missing. Unknown and Capture
// roots (a callable result always carries Unknown) keep the obligation.
func returnOriginRefusedResult(result returnOriginValue) bool {
	refused := false
	for _, root := range result.roots {
		switch {
		case root.kind == returnOriginParam:
		case root.kind == returnOriginLocal && root.expired:
			refused = true
		default:
			return false
		}
	}
	return refused
}
