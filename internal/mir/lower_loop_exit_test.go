package mir

import (
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/source"
	"surge/internal/types"
)

func loopDropNames(l *funcLowerer, b *Block) []string {
	var names []string
	for _, ins := range b.Instrs {
		if ins.Kind == InstrDrop {
			names = append(names, l.f.Locals[ins.Drop.Place.Local].Name)
		}
	}
	return names
}

func requireLoopDrops(t *testing.T, l *funcLowerer, b *Block, want ...string) {
	t.Helper()
	if got := loopDropNames(l, b); !slices.Equal(got, want) {
		t.Fatalf("bb%d drops = %v, want %v", b.ID, got, want)
	}
}

func TestGeneratedLoopLexicalDropOrderAndInitializerFrontier(t *testing.T) {
	for _, early := range []bool{false, true} {
		name := "normal"
		if early {
			name = "end_initializer_returns"
		}
		t.Run(name, func(t *testing.T) {
			in := types.NewInterner()
			f := in.Builtins().Float
			l := loopOwnershipLowerer(in)
			l.f.Result = f
			endValue := loopOwnershipFloat(f, 4.5)
			if early {
				endValue = &hir.Expr{Kind: hir.ExprBlock, Type: f, Data: hir.BlockExprData{Block: &hir.Block{Stmts: []hir.Stmt{
					{Kind: hir.StmtReturn, Data: hir.ReturnData{Value: loopOwnershipFloat(f, 7.5)}},
				}}}}
			}
			body := &hir.Block{Stmts: []hir.Stmt{
				loopOwnershipLet("current", 1, f, loopOwnershipFloat(f, 1.5)),
				loopOwnershipLet("end", 2, f, endValue),
				{Kind: hir.StmtBlock, Data: hir.BlockStmtData{Block: &hir.Block{Stmts: []hir.Stmt{
					loopOwnershipLet("x", 3, f, loopOwnershipFloat(f, 2.5)),
				}}}},
			}}
			if err := l.lowerBlock(body); err != nil {
				t.Fatal(err)
			}
			if len(l.tempDropFrames) != 0 {
				t.Fatal("lexical frames were not unwound")
			}
			if early {
				entry := &l.f.Blocks[l.f.Entry]
				if entry.Term.Kind != TermReturn {
					t.Fatal("initializer did not return on the entry path")
				}
				requireLoopDrops(t, l, entry, "current")
			} else {
				requireLoopDrops(t, l, l.curBlock(), "x", "end", "current")
			}
		})
	}
}

func TestGeneratedLoopReturnDetachesValueBeforeDrops(t *testing.T) {
	in := types.NewInterner()
	f := in.Builtins().Float
	l := loopOwnershipLowerer(in)
	l.f.Result = f
	if err := l.lowerBlock(&hir.Block{Stmts: []hir.Stmt{
		loopOwnershipLet("current", 1, f, loopOwnershipFloat(f, 1.5)),
		loopOwnershipLet("end", 2, f, loopOwnershipFloat(f, 4.5)),
		loopOwnershipLet("x", 3, f, loopOwnershipFloat(f, 2.5)),
		{Kind: hir.StmtReturn, Data: hir.ReturnData{Value: loopOwnershipRef("x", 3, f)}},
	}}); err != nil {
		t.Fatal(err)
	}
	b := l.curBlock()
	requireLoopDrops(t, l, b, "x", "end", "current")
	if b.Term.Kind != TermReturn || b.Term.Return.Value.Kind != OperandMove || b.Term.Return.Value.Place.Local == l.symToLocal[3] {
		t.Fatal("return did not transfer a detached result")
	}
	retained := false
	for _, ins := range b.Instrs {
		if ins.Kind == InstrDrop {
			break
		}
		if ins.Kind == InstrAssign && ins.Assign.Dst.Local == b.Term.Return.Value.Place.Local &&
			ins.Assign.Src.Kind == RValueUse && ins.Assign.Src.Use.Kind == OperandRetain && ins.Assign.Src.Use.Place.Local == l.symToLocal[3] {
			retained = true
		}
	}
	if !retained {
		t.Fatal("returned x was not retained before its lexical release")
	}
}

func TestGeneratedLoopBlockReturnsReleaseOnlyExitedFrames(t *testing.T) {
	for _, implicit := range []bool{false, true} {
		name := "ret"
		if implicit {
			name = "implicit_return"
		}
		t.Run(name, func(t *testing.T) {
			in := types.NewInterner()
			f := in.Builtins().Float
			l := loopOwnershipLowerer(in)
			outer := l.ensureLocal(1, "current", f, source.Span{})
			result := l.newTransferTemp(f, "result", source.Span{})
			l.tempDropFrames = [][]tempDropEntry{{{local: outer}}}
			exit := l.newBlock()
			l.returnStack = []returnCtx{{exit: exit, hasResult: true, result: Place{Local: result}, tempFrameDepth: 1}}
			value := loopOwnershipRef("x", 2, f)
			last := hir.Stmt{Kind: hir.StmtRet, Data: hir.RetData{Value: value}}
			if implicit {
				last = hir.Stmt{Kind: hir.StmtReturn, Data: hir.ReturnData{Value: value, IsImplicit: true}}
			}
			if err := l.lowerBlock(&hir.Block{Stmts: []hir.Stmt{
				loopOwnershipLet("x", 2, f, loopOwnershipFloat(f, 2.5)), last,
			}}); err != nil {
				t.Fatal(err)
			}
			b := l.curBlock()
			requireLoopDrops(t, l, b, "x")
			if b.Term.Kind != TermGoto || b.Term.Goto.Target != exit {
				t.Fatal("block return missed its exit")
			}
			stored := false
			for _, ins := range b.Instrs {
				if ins.Kind == InstrDrop {
					break
				}
				if ins.Kind == InstrAssign && ins.Assign.Dst.Local == result && ins.Assign.Src.Kind == RValueUse {
					stored = ins.Assign.Src.Use.Kind == OperandMove && ins.Assign.Src.Use.Place.Local != l.symToLocal[2]
				}
			}
			if !stored {
				t.Fatal("block result was not detached and stored before x released")
			}
			if len(l.tempDropFrames) != 1 || len(l.tempDropFrames[0]) != 1 {
				t.Fatal("block return removed the outer frame")
			}
		})
	}
}

// Typed float HIR exercises counted fast-loop ownership before the int/uint
// predicate transition. It does not claim source float ranges use this path,
// or that the VM implements float bounds arithmetic.
func TestNumericLoopLatchAndExitOwnership(t *testing.T) {
	for _, name := range []string{"fallthrough", "break", "continue", "expression_continue"} {
		t.Run(name, func(t *testing.T) {
			in := types.NewInterner()
			f := in.Builtins().Float
			l := loopOwnershipLowerer(in)
			current := loopOwnershipRef("current", 1, f)
			post := &hir.Expr{Kind: hir.ExprBinaryOp, Type: f, Data: hir.BinaryOpData{
				Op: ast.ExprBinaryAssign, Left: current, DropOverwritten: true,
				Right: &hir.Expr{Kind: hir.ExprBinaryOp, Type: f, Data: hir.BinaryOpData{
					Op: ast.ExprBinaryAdd, Left: current, Right: loopOwnershipFloat(f, 1),
				}},
			}}
			body := &hir.Block{Stmts: []hir.Stmt{loopOwnershipLet("x", 3, f, loopOwnershipFloat(f, 2.5))}}
			switch name {
			case "break":
				body.Stmts = append(body.Stmts, hir.Stmt{Kind: hir.StmtBreak, Data: hir.BreakData{}})
			case "continue":
				body.Stmts = append(body.Stmts, hir.Stmt{Kind: hir.StmtContinue, Data: hir.ContinueData{}})
			case "expression_continue":
				body.Stmts = append(body.Stmts, hir.Stmt{Kind: hir.StmtExpr, Data: hir.ExprStmtData{
					Expr: &hir.Expr{Kind: hir.ExprBlock, Type: in.Builtins().Nothing, Data: hir.BlockExprData{Block: &hir.Block{Stmts: []hir.Stmt{
						{Kind: hir.StmtContinue, Data: hir.ContinueData{}},
					}}}},
				}})
			}
			if err := l.lowerBlock(&hir.Block{Stmts: []hir.Stmt{
				loopOwnershipLet("current", 1, f, loopOwnershipFloat(f, 1.5)),
				loopOwnershipLet("end", 2, f, loopOwnershipFloat(f, 4.5)),
				{Kind: hir.StmtWhile, Data: hir.WhileData{Cond: boolLiteral(in.Builtins().Bool, true), Body: body, Post: post}},
			}}); err != nil {
				t.Fatal(err)
			}
			header := &l.f.Blocks[l.f.Blocks[l.f.Entry].Term.Goto.Target]
			if header.Term.Kind != TermIf {
				t.Fatal("missing while condition")
			}
			loopBody := &l.f.Blocks[header.Term.If.Then]
			requireLoopDrops(t, l, loopBody, "x")
			exit := header.Term.If.Else
			if loopBody.Term.Kind != TermGoto {
				t.Fatal("body exit is not a jump")
			}
			if name == "break" {
				if loopBody.Term.Goto.Target != exit {
					t.Fatal("break did not exit")
				}
			} else {
				latch := &l.f.Blocks[loopBody.Term.Goto.Target]
				if latch.ID == header.ID || latch.ID == exit || latch.Term.Kind != TermGoto || latch.Term.Goto.Target != header.ID {
					t.Fatal("fallthrough/continue bypassed the numeric latch")
				}
				requireNumericLatchOrder(t, l, latch)
			}
			requireLoopDrops(t, l, &l.f.Blocks[exit], "end", "current")
		})
	}
}

func requireNumericLatchOrder(t *testing.T, l *funcLowerer, b *Block) {
	t.Helper()
	current := l.symToLocal[1]
	compute, drop, store := -1, -1, -1
	for i, ins := range b.Instrs {
		if ins.Kind == InstrAssign && ins.Assign.Src.Kind == RValueBinaryOp {
			compute = i
		}
		if ins.Kind == InstrDrop && ins.Drop.Place.Local == current {
			drop = i
		}
		if ins.Kind == InstrAssign && ins.Assign.Dst.Local == current {
			store = i
		}
	}
	if compute < 0 || drop <= compute || store <= drop {
		t.Fatalf("latch order compute/drop/store = %d/%d/%d", compute, drop, store)
	}
}

func TestNumericLoopPostDropsOnlyOwningConcreteLocals(t *testing.T) {
	in := types.NewInterner()
	for _, tc := range []struct {
		name string
		ty   types.TypeID
	}{
		{"int", in.Builtins().Int},
		{"uint", in.Builtins().Uint},
		{"int32", in.Builtins().Int32},
		{"uint64", in.Builtins().Uint64},
		{"float64", in.Builtins().Float64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := loopOwnershipLowerer(in)
			current := loopOwnershipRef("current", 1, tc.ty)
			literal := &hir.Expr{Kind: hir.ExprLiteral, Type: tc.ty, Data: hir.LiteralData{Kind: hir.LiteralInt, IntValue: 1}}
			if tc.name == "float64" {
				literal = loopOwnershipFloat(tc.ty, 1)
			}
			post := &hir.Expr{Kind: hir.ExprBinaryOp, Type: tc.ty, Data: hir.BinaryOpData{
				Op: ast.ExprBinaryAssign, Left: current, Right: literal, DropOverwritten: true,
			}}
			if err := l.lowerBlock(&hir.Block{Stmts: []hir.Stmt{
				loopOwnershipLet("current", 1, tc.ty, literal),
				{Kind: hir.StmtWhile, Data: hir.WhileData{Cond: boolLiteral(in.Builtins().Bool, true), Body: &hir.Block{}, Post: post}},
			}}); err != nil {
				t.Fatal(err)
			}
			// This invokes the production MIR validity rule, independently of
			// the lowering's predicate, so inline types cannot silently gain
			// a drop when the synthesized HIR carries an ownership candidate.
			if err := validateDrop(l.f, nil); err != nil {
				t.Fatal(err)
			}
			if tc.name == "int32" || tc.name == "uint64" || tc.name == "float64" {
				for i := range l.f.Blocks {
					requireLoopDrops(t, l, &l.f.Blocks[i])
				}
			}
		})
	}
}
