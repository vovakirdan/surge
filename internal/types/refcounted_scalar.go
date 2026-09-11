package types

// IsRefCountedScalar reports whether values of this type are arbitrary-precision
// scalars whose heap form owns a reference-counted block.
//
// These are the types that break the coincidence `IsCopy` used to rely on: a
// value is freely duplicable at the surface, yet duplicating one has to bump a
// count and abandoning one has to drop it. Recognising them is what makes them
// reclaimable without changing what `let b = a` means.
//
// WidthAny int, uint and float qualify. An int or uint may instead be an
// inline fixnum (odd low bit), and NULL is canonical zero. Their lifecycle
// must test for a non-NULL heap word before any count access or lifecycle call.
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
	return tt.Width == WidthAny && (tt.Kind == KindInt || tt.Kind == KindUint || tt.Kind == KindFloat)
}
