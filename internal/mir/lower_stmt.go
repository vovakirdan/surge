package mir

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/sema"
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
		l.pushTempDropFrame()
		err := l.lowerStmt(&b.Stmts[i])
		l.flushTempDropFrame()
		if err != nil {
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
		if len(l.returnStack) > 0 && data.IsImplicit {
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

			// Same contract the explicit-return path honours: these free AFTER
			// the value evaluated (it may read them) and before the terminator.
			// This path carried them unemitted, so a binding a compare arm
			// introduced was never released.
			l.emitExitDrops(data.DropsAfterValue)
			l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: ctx.exit}})
			return nil
		}

		if l.f != nil && l.isNothingType(l.f.Result) {
			if data.Value != nil {
				if err := l.lowerExprForSideEffects(data.Value); err != nil {
					return err
				}
			}
			l.flushTempDropsForExit()
			l.emitExitDrops(data.DropsAfterValue)
			l.setTerm(&Terminator{Kind: TermReturn, Return: ReturnTerm{Early: early}})
			return nil
		}

		if data.Value == nil {
			l.flushTempDropsForExit()
			l.emitExitDrops(data.DropsAfterValue)
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
		op = l.detachFromExitDrops(&op, data.DropsAfterValue, st.Span)
		l.flushTempDropsForExit()
		l.emitExitDrops(data.DropsAfterValue)
		l.setTerm(&Terminator{Kind: TermReturn, Return: ReturnTerm{HasValue: true, Value: op, Early: early}})
		return nil

	case hir.StmtRet:
		data, ok := st.Data.(hir.RetData)
		if !ok {
			return fmt.Errorf("mir: ret: unexpected payload %T", st.Data)
		}
		if len(l.returnStack) == 0 {
			return fmt.Errorf("mir: ret outside of a block-return context")
		}
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
			// A ret that hands ON one of the bindings this exit would drop must
			// not also free it: the result slot is the new owner.
			//
			// Guarded on there BEING exit drops. detachFromExitDrops also fires
			// on pending temps alone, and every block-expression `ret` — compare
			// arms, value blocks — reaches here with temps pending and no exit
			// drops. Those already hand their result over safely (the flush
			// below excludes the result local), so materializing a transfer temp
			// for them would be an unrequested change to a working path.
			if len(data.DropsAfterValue) > 0 {
				op = l.detachFromExitDrops(&op, data.DropsAfterValue, st.Span)
			}
			l.emit(&Instr{
				Kind: InstrAssign,
				Assign: AssignInstr{
					Dst: ctx.result,
					Src: RValue{Kind: RValueUse, Use: op},
				},
			})
		} else if data.Value != nil {
			if err := l.lowerExprForSideEffects(data.Value); err != nil {
				return err
			}
		}
		// The regions between here and the block's exit are skipped by the
		// goto below and never run their own flush, so their temps are freed
		// on this edge.
		l.flushTempDropsForRet(ctx.tempFrameDepth, ctx.result.Local)
		// Same contract the explicit-return path honours: what this exit still
		// owns is released here, after the value has been read into the result
		// slot. The goto skips the block's normal end, so these obligations
		// have nowhere else to run — a crossing body reaches its exit through
		// `ret` and nothing else.
		l.emitExitDrops(data.DropsAfterValue)
		l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: ctx.exit}})
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
		l.pushTempDropFrame()
		condOp, err := l.lowerValueExpr(cond, false)
		if err != nil {
			return err
		}
		l.flushTempDropFrame()

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
		l.pushTempDropFrame()
		condOp, err := l.lowerValueExpr(data.Cond, false)
		if err != nil {
			return err
		}
		l.flushTempDropFrame()
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
		if len(data.Steps) > 0 {
			// A RESIDUAL drop: reclaim the places this binding still holds.
			place, err := l.lowerPlace(data.Value)
			if err != nil {
				return err
			}
			l.emitResidualDropAt(place, data.Steps)
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
		// - owns heap → Drop
		// - anything else → nothing
		isRef := false
		if l.types != nil && ty != types.NoTypeID {
			resolved := resolveAlias(l.types, ty)
			if tt, ok := l.types.Lookup(resolved); ok {
				isRef = (tt.Kind == types.KindReference)
			}
		}

		if isRef {
			l.emit(&Instr{Kind: InstrEndBorrow, EndBorrow: EndBorrowInstr{Place: place}})
		} else if l.ownsHeap(ty) {
			l.emit(&Instr{Kind: InstrDrop, Drop: DropInstr{Place: place}})
		}
		// else: nothing to reclaim → emit nothing
		return nil

	case hir.StmtEnvelopeRelease:
		data, ok := st.Data.(hir.EnvelopeReleaseData)
		if !ok {
			return fmt.Errorf("mir: envelope_release: unexpected payload %T", st.Data)
		}
		if data.Value == nil {
			return nil
		}
		place, err := l.lowerPlace(data.Value)
		if err != nil {
			return err
		}
		// Unconditional: the envelope is always a heap box regardless of
		// whether the declared element type is Copy (unlike InstrDrop,
		// which a Copy type would suppress above).
		l.emit(&Instr{Kind: InstrEnvelopeRelease, EnvelopeRelease: EnvelopeReleaseInstr{Place: place, Cursor: data.Cursor}})
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

// detachFromExitDrops materializes a place-shaped return operand into a
// fresh temp before the exit drops run: terminators may read place
// operands lazily (the VM does), and the operand can project into a
// dropped local (`return self.code` with self dropping).
func (l *funcLowerer) detachFromExitDrops(op *Operand, drops []hir.DropLocal, span source.Span) Operand {
	if len(drops) == 0 && !l.hasPendingTempDrops() {
		return *op
	}
	if op.Kind != OperandCopy && op.Kind != OperandCopyValue && op.Kind != OperandMove && op.Kind != OperandRetain {
		return *op
	}
	// The temp is a TRANSFER: its reference leaves with the return value, so
	// it is kept out of the temp-drop frames the flush below walks. The
	// materialization has to happen HERE, before the exit drops, or a returned
	// binding would be read after its own release.
	// Marked as OWNING its value, which is the same statement the comment
	// above makes: consuming this temp transfers what it holds to the caller
	// rather than duplicating it. Without the mark a composite return would
	// clone here and leave the temp's own box with no one to reclaim it — the
	// temp is deliberately outside the drop frames, so nothing sweeps it up.
	tmp := l.markOwningTemp(l.newTransferTemp(op.Type, "retval", span))
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueUse, Use: *op},
		},
	})
	// Reading the transfer temp must not retain again: the reference it holds
	// is the one the caller receives.
	if l.isRefCountedScalar(op.Type) {
		return l.placeOperand(Place{Local: tmp}, op.Type, false)
	}
	return l.placeOperand(Place{Local: tmp}, op.Type, true)
}

// emitExitDrops frees the carried scope-exit obligations of a return:
// after the value evaluated, before the terminator. The synthesized
// drops match explicit `@drop` lowering (InstrDrop; copies never carry
// obligations, references never appear — sema's droppable predicate).
func (l *funcLowerer) emitExitDrops(drops []hir.DropLocal) {
	for i := range drops {
		local, ok := l.symToLocal[drops[i].SymbolID]
		if !ok {
			continue
		}
		// Generic bodies carry obligations for T-typed params; an
		// instantiation that owns no heap has nothing to free — mirror
		// StmtDrop lowering.
		ty := drops[i].Type
		if int(local) < len(l.f.Locals) {
			ty = l.f.Locals[local].Type
		}
		if len(drops[i].Steps) > 0 {
			// A RESIDUAL drop: this binding is only partly moved, so it
			// reclaims the places it still holds instead of all of itself.
			// The plan already carries the order — contents before the
			// container that holds them — so it is emitted as it stands.
			l.emitResidualDrop(local, drops[i].Steps)
			continue
		}
		if !l.ownsHeap(ty) {
			continue
		}
		l.emit(&Instr{Kind: InstrDrop, Drop: DropInstr{Place: Place{Local: local}}})
	}
}

// emitResidualDrop lowers a residual plan into the drops the backends perform:
// a deep drop for each live place and a shallow free for each container that
// survives only in part.
func (l *funcLowerer) emitResidualDrop(local LocalID, steps []sema.DropStep) {
	l.emitResidualDropAt(Place{Local: local}, steps)
}

// emitResidualDropAt emits a residual plan against an already-resolved base
// place, so both the obligation path and an explicit drop statement lower the
// same plan the same way.
func (l *funcLowerer) emitResidualDropAt(base Place, steps []sema.DropStep) {
	for _, step := range steps {
		place := base
		place.Proj = append(append([]PlaceProj(nil), base.Proj...), l.placeProjFromSteps(step.Path)...)
		l.emit(&Instr{Kind: InstrDrop, Drop: DropInstr{Place: place, Shallow: step.Shallow}})
	}
}

// placeProjFromSteps converts sema's projection path into MIR's. Field indices
// are left unresolved (-1) exactly as every other projected place does it — the
// emitters resolve a field by name against the container's type.
func (l *funcLowerer) placeProjFromSteps(path []sema.PlaceSegment) []PlaceProj {
	if len(path) == 0 {
		return nil
	}
	out := make([]PlaceProj, 0, len(path))
	for _, seg := range path {
		switch seg.Kind {
		case sema.PlaceSegmentField:
			out = append(out, PlaceProj{Kind: PlaceProjField, FieldName: l.lookupFieldName(seg.Name), FieldIdx: -1})
		case sema.PlaceSegmentIndex:
			out = append(out, PlaceProj{Kind: PlaceProjIndex})
		case sema.PlaceSegmentDeref:
			out = append(out, PlaceProj{Kind: PlaceProjDeref})
		}
	}
	return out
}

// lookupFieldName resolves an interned field name for a projection. The
// emitters match a field by name, so the plan's StringID has to become text
// here, where the interner is still in reach.
func (l *funcLowerer) lookupFieldName(id source.StringID) string {
	if l == nil || l.symbols == nil || l.symbols.Table == nil || l.symbols.Table.Strings == nil {
		return ""
	}
	name, _ := l.symbols.Table.Strings.Lookup(id)
	return name
}
