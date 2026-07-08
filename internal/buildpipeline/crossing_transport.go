package buildpipeline

// crossingBackendGuardApplies reports whether compile-time crossing surfaces
// must be stopped before lowering for this backend selection. An empty backend
// means no executable backend was selected; every non-empty backend remains
// blocked until it is explicitly recorded as transport-capable.
func crossingBackendGuardApplies(backend Backend) bool {
	if backend == "" {
		return false
	}
	return !backendHasCrossingTransport(backend)
}

func backendHasCrossingTransport(backend Backend) bool {
	return false
}
