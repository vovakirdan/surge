package diag

// Physical-layout diagnostics are post-sema errors. They have their own
// stable codes so backend layout failures never masquerade as clone errors or
// leak internal TypeIDs to users.
const (
	SemaLayoutOverflow             Code = 3180
	SemaLayoutUnsupportedAlignment Code = 3181
	SemaLayoutDeferred             Code = 3182
)
