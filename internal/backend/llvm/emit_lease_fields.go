package llvm

import "surge/internal/types"

// A crossing state is a struct of captures, and one capture is not owned by
// it: the anchor of an `on far_handle { ... }` block. The caller keeps the
// handle, the body reaches the channel through the owner-side pin, and the
// state carries a copy of the token that nobody reads and nobody may release.
// MIR names those fields per state type (Module.CrossingLeaseFields); the
// release glue of the state skips them, so an unshipped state -- a refused,
// torn-down or shutdown-swept request -- gives back only what the state
// owns, and the caller's own drop of the handle stays the one release
// (RV2-DEBT-324).
func (e *Emitter) crossingLeaseField(state types.TypeID, field int) bool {
	if e == nil || e.mod == nil || dropLeaseFieldsNegativeControl {
		return false
	}
	for _, index := range e.mod.CrossingLeaseFields[state] {
		if index == field {
			return true
		}
	}
	return false
}

// dropLeaseFieldsNegativeControl restores the pre-fix glue -- the lease
// field released like an owned member -- for the negative control of the
// RV2-DEBT-324 rows. Never set outside a test.
var dropLeaseFieldsNegativeControl bool
