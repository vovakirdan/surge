package mir

import (
	"strings"
	"testing"

	"surge/internal/hir"
	"surge/internal/source"
	"surge/internal/types"
)

func TestLowerUintLiteralPreservesKindAndText(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	b := in.Builtins()
	alias := in.RegisterAlias(in.Strings.Intern("Count"), source.Span{})
	in.SetAliasTarget(alias, b.Uint)
	lowerer := &funcLowerer{types: in}
	for _, row := range []struct {
		name, text string
		typ        types.TypeID
		cached     uint64
		inline     bool
	}{
		{"zero", "0", b.Uint, 0, true},
		{"inline_max", "9223372036854775807", b.Uint, 9223372036854775807, true},
		{"u64_max", "18446744073709551615", b.Uint, ^uint64(0), false},
		{"above_u64", "18446744073709551616", b.Uint, 0, false},
		{"decimal310", "1" + strings.Repeat("0", 310), b.Uint, 0, false},
		{"hex_above_u64", "0x10000000000000000", b.Uint, 0, false},
		{"alias_uint", "18446744073709551616", alias, 0, false},
		{"fixed_u64", "18446744073709551615", b.Uint64, ^uint64(0), false},
	} {
		t.Run(row.name, func(t *testing.T) {
			// HIR keeps the full spelling even when its int64 cache is zero.
			// Lower the real HIR literal instead of fabricating the expected MIR.
			op := lowerer.lowerLiteral(row.typ, hir.LiteralData{Kind: hir.LiteralInt, Text: row.text})
			if op.Kind != OperandConst || op.Type != row.typ || op.Const.Type != row.typ ||
				op.Const.Kind != ConstUint || op.Const.Text != row.text || op.Const.UintValue != row.cached {
				t.Fatalf("uint literal lost its typed spelling: %+v; want type=%d text=%q cache=%d", op, row.typ, row.text, row.cached)
			}
			if inline := ConstFoldsToFixnum(in, &op.Const); inline != row.inline {
				t.Fatalf("literal inline=%v, want %v: %+v", inline, row.inline, op.Const)
			}
		})
	}
}
