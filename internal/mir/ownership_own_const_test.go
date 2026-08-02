package mir_test

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/mir"
)

func TestOwnershipOwnConstantPreservesMintingSource(t *testing.T) {
	mod, typesIn, semaRes := lowerForOwnership(t, `
fn main() {
    let value: own string = own "hello";
}
`)
	findings := mir.VerifyOwnership(mod, typesIn, semaRes)
	if got := findingsIn(findings, "main"); len(got) != 0 {
		t.Errorf("own constant should carry its freshly minted value, got:\n%s", joinLines(got))
	}

	var sawOwnConst bool
	for _, fn := range mod.Funcs {
		if fn == nil || fn.Name != "main" {
			continue
		}
		for bi := range fn.Blocks {
			for ii := range fn.Blocks[bi].Instrs {
				ins := &fn.Blocks[bi].Instrs[ii]
				if ins.Kind == mir.InstrAssign && ins.Assign.Src.Kind == mir.RValueUnaryOp &&
					ins.Assign.Src.Unary.Op == ast.ExprUnaryOwn &&
					ins.Assign.Src.Unary.Operand.Kind == mir.OperandConst {
					sawOwnConst = true
				}
			}
		}
	}
	if !sawOwnConst {
		t.Fatal("real lowering did not produce the own-constant transfer shape")
	}
}

func TestOwnershipOwnPlaceTracesOwnedDefinition(t *testing.T) {
	env := newOwnershipEnv(t)
	b := newFn("own_owned_place")
	source := b.param("source", env.str, true)
	owned := b.local("owned", env.str, true)
	b.block([]mir.Instr{
		{Kind: mir.InstrAssign, Assign: mir.AssignInstr{
			Dst: place(owned),
			Src: mir.RValue{Kind: mir.RValueUnaryOp, Unary: mir.UnaryOp{
				Op: ast.ExprUnaryOwn, Operand: opCopy(source, env.str),
			}},
		}},
		dropL(owned),
	}, retTerm())

	requireClean(t, env.verify(b.done()))
}

func TestOwnershipOwnDoesNotLaunderAlias(t *testing.T) {
	env := newOwnershipEnv(t)
	b := newFn("own_alias")
	source := b.param("source", env.str, true)
	alias := b.local("alias", env.str, true)
	owned := b.local("owned", env.str, true)
	b.block([]mir.Instr{
		{Kind: mir.InstrAssign, Assign: mir.AssignInstr{
			Dst: place(alias),
			Src: mir.RValue{Kind: mir.RValueUse, Use: opCopy(source, env.str)},
		}},
		{Kind: mir.InstrAssign, Assign: mir.AssignInstr{
			Dst: place(owned),
			Src: mir.RValue{Kind: mir.RValueUnaryOp, Unary: mir.UnaryOp{
				Op: ast.ExprUnaryOwn, Operand: opCopy(alias, env.str),
			}},
		}},
		dropL(owned),
	}, retTerm())

	requireFindings(t, env.verify(b.done()),
		"own_alias: drop of L2(owned) (def bb0#0) at bb0#2")
}

func TestOwnershipOwnProjectedCopyFailsClosed(t *testing.T) {
	env := newOwnershipEnv(t)
	b := newFn("own_projected_copy")
	source := b.param("source", env.holder, true)
	owned := b.local("owned", env.flt, true)
	b.block([]mir.Instr{
		{Kind: mir.InstrAssign, Assign: mir.AssignInstr{
			Dst: place(owned),
			Src: mir.RValue{Kind: mir.RValueUnaryOp, Unary: mir.UnaryOp{
				Op: ast.ExprUnaryOwn,
				Operand: mir.Operand{
					Kind: mir.OperandCopy,
					Type: env.flt,
					Place: mir.Place{
						Kind:  mir.PlaceLocal,
						Local: source,
						Proj:  []mir.PlaceProj{{Kind: mir.PlaceProjField, FieldName: "v"}},
					},
				},
			}},
		}},
		dropL(owned),
	}, retTerm())

	requireFindings(t, env.verify(b.done()),
		"own_projected_copy: drop of L1(owned) (def bb0#0) at bb0#1")
}
