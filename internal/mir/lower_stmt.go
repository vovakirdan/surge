package mir

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/source"
	"surge/internal/types"
)

func (l *funcLowerer) lowerBlock(b *hir.Block) error {
	if l == nil || b == nil {
		return nil
	}
	for i := range b.Stmts {
		if l.curBlock().Terminated() {
			return nil
		}
		if err := l.lowerStmt(&b.Stmts[i]); err != nil {
			return err
		}
	}
	return nil
}

func (l *funcLowerer) lowerStmt(st *hir.Stmt) error {
	if l == nil || st == nil {
		return nil
	}
	if l.curBlock().Terminated() {
		return nil
	}

	switch st.Kind {
	case hir.StmtLet:
		data, ok := st.Data.(hir.LetData)
		if !ok {
			return fmt.Errorf("mir: let: unexpected payload %T", st.Data)
		}

		// Tuple destructuring `let (a,b) = value`.
		if data.Pattern != nil && data.Value != nil {
			return l.lowerLetPattern(st.Span, data)
		}

		if data.SymbolID.IsValid() {
			localID := l.ensureLocal(data.SymbolID, data.Name, data.Type, st.Span)
			if data.Value != nil {
				expected := data.Type
				op, err := l.lowerExprForType(data.Value, expected)
				if err != nil {
					return err
				}
				l.emit(&Instr{
					Kind: InstrAssign,
					Assign: AssignInstr{
						Dst: Place{Local: localID},
						Src: RValue{Kind: RValueUse, Use: op},
					},
				})
			}
			return nil
		}

		// No symbol (e.g. `let _ = expr;`): evaluate for side effects.
		if data.Value != nil {
			return l.lowerExprForSideEffects(data.Value)
		}
		return nil

	case hir.StmtExpr:
		data, ok := st.Data.(hir.ExprStmtData)
		if !ok {
			return fmt.Errorf("mir: expr stmt: unexpected payload %T", st.Data)
		}
		if data.Expr == nil {
			return nil
		}
		return l.lowerExprForSideEffects(data.Expr)

	case hir.StmtAssign:
		data, ok := st.Data.(hir.AssignData)
		if !ok {
			return fmt.Errorf("mir: assign: unexpected payload %T", st.Data)
		}
		dst, err := l.lowerPlace(data.Target)
		if err != nil {
			return err
		}
		if data.Value == nil {
			return nil
		}
		expected := l.exprType(data.Target)
		if data.Target != nil && data.Target.Kind == hir.ExprIndex {
			expected = l.unwrapReferenceType(expected)
		}
		op, err := l.lowerExprForType(data.Value, expected)
		if err != nil {
			return err
		}
		l.emit(&Instr{
			Kind: InstrAssign,
			Assign: AssignInstr{
				Dst: dst,
				Src: RValue{Kind: RValueUse, Use: op},
			},
		})
		return nil

	case hir.StmtReturn:
		data, ok := st.Data.(hir.ReturnData)
		if !ok {
			return fmt.Errorf("mir: return: unexpected payload %T", st.Data)
		}
		early := !data.IsTail
		if len(l.returnStack) > 0 {
			ctx := l.returnStack[len(l.returnStack)-1]
			if ctx.hasResult && data.Value != nil {
				expected := types.NoTypeID
				if l.f != nil && ctx.result.Local != NoLocalID {
					idx := int(ctx.result.Local)
					if idx >= 0 && idx < len(l.f.Locals) {
						expected = l.f.Locals[idx].Type
					}
				}
				op, err := l.lowerExprForType(data.Value, expected)
				if err != nil {
					return err
				}
				l.emit(&Instr{
					Kind: InstrAssign,
					Assign: AssignInstr{
						Dst: ctx.result,
						Src: RValue{Kind: RValueUse, Use: op},
					},
				})
			} else if data.Value != nil {
				// Still lower for side effects.
				if err := l.lowerExprForSideEffects(data.Value); err != nil {
					return err
				}
			}

			l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: ctx.exit}})
			return nil
		}

		if l.f != nil && l.isNothingType(l.f.Result) {
			if data.Value != nil {
				if err := l.lowerExprForSideEffects(data.Value); err != nil {
					return err
				}
			}
			l.setTerm(&Terminator{Kind: TermReturn, Return: ReturnTerm{Early: early}})
			return nil
		}

		if data.Value == nil {
			l.setTerm(&Terminator{Kind: TermReturn, Return: ReturnTerm{Early: early}})
			return nil
		}
		expected := types.NoTypeID
		if l.f != nil {
			expected = l.f.Result
		}
		op, err := l.lowerExprForType(data.Value, expected)
		if err != nil {
			return err
		}
		l.setTerm(&Terminator{Kind: TermReturn, Return: ReturnTerm{HasValue: true, Value: op, Early: early}})
		return nil

	case hir.StmtBreak:
		if len(l.loopStack) == 0 {
			return fmt.Errorf("mir: break outside of a loop")
		}
		ctx := l.loopStack[len(l.loopStack)-1]
		l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: ctx.breakTarget}})
		return nil

	case hir.StmtContinue:
		if len(l.loopStack) == 0 {
			return fmt.Errorf("mir: continue outside of a loop")
		}
		ctx := l.loopStack[len(l.loopStack)-1]
		l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: ctx.continueTarget}})
		return nil

	case hir.StmtIf:
		data, ok := st.Data.(hir.IfStmtData)
		if !ok {
			return fmt.Errorf("mir: if: unexpected payload %T", st.Data)
		}
		cond := data.Cond
		condOp, err := l.lowerValueExpr(cond, false)
		if err != nil {
			return err
		}

		thenBB := l.newBlock()
		elseBB := l.newBlock()
		joinBB := l.newBlock()

		l.setTerm(&Terminator{
			Kind: TermIf,
			If: IfTerm{
				Cond: condOp,
				Then: thenBB,
				Else: elseBB,
			},
		})

		l.startBlock(thenBB)
		if err := l.lowerBlock(data.Then); err != nil {
			return err
		}
		if !l.curBlock().Terminated() {
			l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: joinBB}})
		}

		l.startBlock(elseBB)
		if err := l.lowerBlock(data.Else); err != nil {
			return err
		}
		if !l.curBlock().Terminated() {
			l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: joinBB}})
		}

		l.startBlock(joinBB)
		return nil

	case hir.StmtWhile:
		data, ok := st.Data.(hir.WhileData)
		if !ok {
			return fmt.Errorf("mir: while: unexpected payload %T", st.Data)
		}

		headerBB := l.newBlock()
		bodyBB := l.newBlock()
		exitBB := l.newBlock()

		l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: headerBB}})

		l.startBlock(headerBB)
		condOp, err := l.lowerValueExpr(data.Cond, false)
		if err != nil {
			return err
		}
		l.setTerm(&Terminator{
			Kind: TermIf,
			If: IfTerm{
				Cond: condOp,
				Then: bodyBB,
				Else: exitBB,
			},
		})

		l.startBlock(bodyBB)
		l.loopStack = append(l.loopStack, loopCtx{breakTarget: exitBB, continueTarget: headerBB})
		if err := l.lowerBlock(data.Body); err != nil {
			return err
		}
		l.loopStack = l.loopStack[:len(l.loopStack)-1]
		if !l.curBlock().Terminated() {
			l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: headerBB}})
		}

		l.startBlock(exitBB)
		return nil

	case hir.StmtFor:
		return fmt.Errorf("mir: unexpected for-loop after HIR normalization")

	case hir.StmtBlock:
		data, ok := st.Data.(hir.BlockStmtData)
		if !ok {
			return fmt.Errorf("mir: block: unexpected payload %T", st.Data)
		}
		return l.lowerBlock(data.Block)

	case hir.StmtDrop:
		data, ok := st.Data.(hir.DropData)
		if !ok {
			return fmt.Errorf("mir: drop: unexpected payload %T", st.Data)
		}
		if data.Value == nil {
			return nil
		}

		// Get the type of the value being dropped
		ty := data.Value.Type

		place, err := l.lowerPlace(data.Value)
		if err != nil {
			// Not a place: lower into a temp and drop it.
			tmpTy := types.NoTypeID
			if data.Value != nil {
				tmpTy = data.Value.Type
			}
			tmp := l.newTemp(tmpTy, "drop", st.Span)
			op, err2 := l.lowerExpr(data.Value, true)
			if err2 != nil {
				return err2
			}
			l.emit(&Instr{
				Kind: InstrAssign,
				Assign: AssignInstr{
					Dst: Place{Local: tmp},
					Src: RValue{Kind: RValueUse, Use: op},
				},
			})
			place = Place{Local: tmp}
		}

		// Determine instruction based on type:
		// - &T or &mut T → EndBorrow
		// - non-copy → Drop
		// - copy → nothing
		isRef := false
		if l.types != nil && ty != types.NoTypeID {
			resolved := resolveAlias(l.types, ty)
			if tt, ok := l.types.Lookup(resolved); ok {
				isRef = (tt.Kind == types.KindReference)
			}
		}

		if isRef {
			l.emit(&Instr{Kind: InstrEndBorrow, EndBorrow: EndBorrowInstr{Place: place}})
		} else if !l.isCopyType(ty) {
			l.emit(&Instr{Kind: InstrDrop, Drop: DropInstr{Place: place}})
		}
		// else: copy type → emit nothing
		return nil

	default:
		return nil
	}
}

func (l *funcLowerer) lowerLetPattern(span source.Span, data hir.LetData) error {
	if l == nil {
		return nil
	}
	// Currently only supports tuple patterns: let (a,b,...) = value.
	if data.Value == nil || data.Pattern == nil || data.Pattern.Kind != hir.ExprTupleLit {
		if data.Value != nil {
			_, err := l.lowerExpr(data.Value, false)
			return err
		}
		return nil
	}
	pat, ok := data.Pattern.Data.(hir.TupleLitData)
	if !ok {
		return fmt.Errorf("mir: let pattern: unexpected payload %T", data.Pattern.Data)
	}

	tupleTy := data.Value.Type
	tupleTmp := l.newTemp(tupleTy, "tuple", span)
	valOp, err := l.lowerExpr(data.Value, true)
	if err != nil {
		return err
	}
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tupleTmp},
			Src: RValue{Kind: RValueUse, Use: valOp},
		},
	})
	tupleOperand := Operand{Kind: OperandCopy, Type: tupleTy, Place: Place{Local: tupleTmp}}

	for i, el := range pat.Elements {
		if el == nil {
			continue
		}
		if el.Kind != hir.ExprVarRef {
			continue
		}
		vr, ok := el.Data.(hir.VarRefData)
		if !ok || !vr.SymbolID.IsValid() || vr.Name == "" || vr.Name == "_" {
			continue
		}
		localID := l.ensureLocal(vr.SymbolID, vr.Name, el.Type, el.Span)
		l.emit(&Instr{
			Kind: InstrAssign,
			Assign: AssignInstr{
				Dst: Place{Local: localID},
				Src: RValue{
					Kind: RValueField,
					Field: FieldAccess{
						Object:   tupleOperand,
						FieldIdx: i,
					},
				},
			},
		})
	}

	return nil
}
