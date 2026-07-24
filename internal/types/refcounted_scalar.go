package types

// IsRefCountedScalar reports whether values of this type are arbitrary-precision
// scalars represented out of line by a reference-counted heap block.
//
// These are the types that break the coincidence `IsCopy` used to rely on: a
// value is freely duplicable at the surface, yet duplicating one has to bump a
// count and abandoning one has to drop it. Recognising them is what makes them
// reclaimable without changing what `let b = a` means.
//
// Only WidthAny `float` qualifies today. `float` has no inline form at all —
// every value is a heap block and every operation allocates one — which is why
// it goes first. `int` and `uint` follow once the same mechanism is proven:
// they carry an inline fixnum form (odd low bit) that a count must skip, so
// they add a branch to a mechanism rather than a mechanism.
//
// The fixed-width types are deliberately NOT included: `f32`/`float64`, `i8..i64`
// and `u8..u64` are machine words with no heap behind them and must keep
// costing a register move.
func (in *Interner) IsRefCountedScalar(id TypeID) bool {
	if in == nil || id == NoTypeID {
		return false
	}
	tt, ok := in.Lookup(id)
	if !ok {
		return false
	}
	return tt.Kind == KindFloat && tt.Width == WidthAny
}
