package mir

import (
	"surge/internal/numlit"
	"surge/internal/types"
)

// InRangeBigIntLiteral gives MIR ownership and LLVM folding the same answer.
// Text is authoritative; synthesized constants fall back to their value.
func InRangeBigIntLiteral(c *Const) (int64, bool) {
	if c == nil || c.Kind != ConstInt {
		return 0, false
	}
	return numlit.InlineInt(c.Text, c.IntValue)
}

// InRangeBigUintLiteral is the unsigned half of InRangeBigIntLiteral.
func InRangeBigUintLiteral(c *Const) (uint64, bool) {
	if c == nil || c.Kind != ConstUint {
		return 0, false
	}
	return numlit.InlineUint(c.Text, c.UintValue)
}

// ConstFoldsToFixnum excludes only proven inline arbitrary-precision numbers.
// References, fixed-width values and mismatched constant kinds do not qualify.
func ConstFoldsToFixnum(in *types.Interner, c *Const) bool {
	if in == nil || c == nil {
		return false
	}
	tt, ok := in.Lookup(resolveAliasAndOwn(in, c.Type))
	if !ok || tt.Width != types.WidthAny {
		return false
	}
	switch tt.Kind {
	case types.KindInt:
		_, ok = InRangeBigIntLiteral(c)
	case types.KindUint:
		_, ok = InRangeBigUintLiteral(c)
	default:
		return false
	}
	return ok
}
