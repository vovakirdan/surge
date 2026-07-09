package buildpipeline

import "surge/internal/sema"

// crossingBackendGuardApplies reports whether compile-time crossing surfaces
// must be stopped before lowering for this backend/form selection. An empty
// backend means no executable backend was selected; every non-empty backend/form
// pair remains blocked until it is explicitly recorded as transport-capable.
func crossingBackendGuardApplies(backend Backend, form sema.CrossingLoweringKind) bool {
	if backend == "" {
		return false
	}
	return !backendSupportsCrossingForm(backend, form)
}

func crossingBackendGuardAppliesForRequest(req *CompileRequest, form sema.CrossingLoweringKind) bool {
	if req == nil {
		return false
	}
	if req.CrossingFormsForTest != nil && req.CrossingFormsForTest[form] {
		return false
	}
	return crossingBackendGuardApplies(req.Backend, form)
}

func backendSupportsCrossingForm(backend Backend, form sema.CrossingLoweringKind) bool {
	return false
}
