package mir

import (
	"errors"
	"fmt"

	"surge/internal/sema"
	"surge/internal/types"
)

// ValidateOptions configures optional MIR validation capabilities.
type ValidateOptions struct {
	CrossingForms map[sema.CrossingLoweringKind]bool
}

func (opts ValidateOptions) crossingEnabled(kind sema.CrossingLoweringKind) bool {
	return opts.CrossingForms != nil && opts.CrossingForms[kind]
}

// Validate checks MIR module invariants.
// Returns error if any invariant is violated.
func Validate(m *Module, typesIn *types.Interner) error {
	return ValidateWithOptions(m, typesIn, ValidateOptions{})
}

// ValidateWithOptions checks MIR module invariants with explicit capabilities.
func ValidateWithOptions(m *Module, typesIn *types.Interner, opts ValidateOptions) error {
	if m == nil {
		return nil
	}
	var errs []error
	if err := validateGlobalTypes(m.Globals, typesIn); err != nil {
		errs = append(errs, err)
	}
	for _, f := range m.Funcs {
		if f == nil {
			continue
		}
		if err := validateFunc(f, typesIn, m.Globals, opts); err != nil {
			errs = append(errs, fmt.Errorf("function %s: %w", f.Name, err))
		}
	}
	return errors.Join(errs...)
}

func validateFunc(f *Func, typesIn *types.Interner, globals []Global, opts ValidateOptions) error {
	if f == nil {
		return nil
	}

	var errs []error

	// 1. Check all blocks terminated
	if err := validateBlocksTerminated(f); err != nil {
		errs = append(errs, err)
	}

	// 2. Check block targets exist
	if err := validateBlockTargets(f); err != nil {
		errs = append(errs, err)
	}

	// 3. Check local IDs exist in instructions
	if err := validateLocalIDs(f, globals); err != nil {
		errs = append(errs, err)
	}

	// 4 & 5. Check types (no TypeParam, no NoTypeID)
	if err := validateTypes(f, typesIn); err != nil {
		errs = append(errs, err)
	}

	// 6. Check return type matching
	if err := validateReturn(f, typesIn); err != nil {
		errs = append(errs, err)
	}

	// 7. Check EndBorrow validity
	if err := validateEndBorrow(f, globals); err != nil {
		errs = append(errs, err)
	}

	// 8. Check Drop validity
	if err := validateDrop(f, globals); err != nil {
		errs = append(errs, err)
	}

	// 9. Crossing instructions are default-closed unless explicitly enabled.
	if err := validateCrossingSupport(f, opts); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// validateBlocksTerminated checks that every block ends with a terminator.
func validateBlocksTerminated(f *Func) error {
	var errs []error
	for i := range f.Blocks {
		if f.Blocks[i].Term.Kind == TermNone {
			if len(f.Blocks[i].Instrs) > 0 {
				last := f.Blocks[i].Instrs[len(f.Blocks[i].Instrs)-1]
				if last.Kind == InstrPoll || last.Kind == InstrJoinAll || last.Kind == InstrChanSend || last.Kind == InstrChanRecv || last.Kind == InstrNetWait || last.Kind == InstrTimeout || last.Kind == InstrSelect || last.Kind == InstrCrossing {
					continue
				}
			}
			errs = append(errs, fmt.Errorf("bb%d: unterminated block", i))
		}
	}
	return errors.Join(errs...)
}

// validateBlockTargets checks that all block target IDs exist.
func validateBlockTargets(f *Func) error {
	var errs []error

	blockExists := func(id BlockID) bool {
		return id >= 0 && int(id) < len(f.Blocks)
	}

	for i := range f.Blocks {
		bb := &f.Blocks[i]
		for j := range bb.Instrs {
			ins := &bb.Instrs[j]
			switch ins.Kind {
			case InstrPoll:
				if !blockExists(ins.Poll.ReadyBB) {
					errs = append(errs, fmt.Errorf("bb%d instr %d: poll ready target bb%d does not exist", i, j, ins.Poll.ReadyBB))
				}
				if !blockExists(ins.Poll.PendBB) {
					errs = append(errs, fmt.Errorf("bb%d instr %d: poll pending target bb%d does not exist", i, j, ins.Poll.PendBB))
				}
			case InstrJoinAll:
				if !blockExists(ins.JoinAll.ReadyBB) {
					errs = append(errs, fmt.Errorf("bb%d instr %d: join_all ready target bb%d does not exist", i, j, ins.JoinAll.ReadyBB))
				}
				if !blockExists(ins.JoinAll.PendBB) {
					errs = append(errs, fmt.Errorf("bb%d instr %d: join_all pending target bb%d does not exist", i, j, ins.JoinAll.PendBB))
				}
			case InstrChanSend:
				if !blockExists(ins.ChanSend.ReadyBB) {
					errs = append(errs, fmt.Errorf("bb%d instr %d: chan_send ready target bb%d does not exist", i, j, ins.ChanSend.ReadyBB))
				}
				if !blockExists(ins.ChanSend.PendBB) {
					errs = append(errs, fmt.Errorf("bb%d instr %d: chan_send pending target bb%d does not exist", i, j, ins.ChanSend.PendBB))
				}
			case InstrChanRecv:
				if ins.ChanRecv.Anchored {
					// Anchored receives park by re-entering the body; they
					// carry no suspend targets.
					continue
				}
				if !blockExists(ins.ChanRecv.ReadyBB) {
					errs = append(errs, fmt.Errorf("bb%d instr %d: chan_recv ready target bb%d does not exist", i, j, ins.ChanRecv.ReadyBB))
				}
				if !blockExists(ins.ChanRecv.PendBB) {
					errs = append(errs, fmt.Errorf("bb%d instr %d: chan_recv pending target bb%d does not exist", i, j, ins.ChanRecv.PendBB))
				}
			case InstrNetWait:
				if !blockExists(ins.NetWait.ReadyBB) {
					errs = append(errs, fmt.Errorf("bb%d instr %d: net_wait ready target bb%d does not exist", i, j, ins.NetWait.ReadyBB))
				}
				if !blockExists(ins.NetWait.PendBB) {
					errs = append(errs, fmt.Errorf("bb%d instr %d: net_wait pending target bb%d does not exist", i, j, ins.NetWait.PendBB))
				}
			case InstrTimeout:
				if !blockExists(ins.Timeout.ReadyBB) {
					errs = append(errs, fmt.Errorf("bb%d instr %d: timeout ready target bb%d does not exist", i, j, ins.Timeout.ReadyBB))
				}
				if !blockExists(ins.Timeout.PendBB) {
					errs = append(errs, fmt.Errorf("bb%d instr %d: timeout pending target bb%d does not exist", i, j, ins.Timeout.PendBB))
				}
			case InstrSelect:
				if !blockExists(ins.Select.ReadyBB) {
					errs = append(errs, fmt.Errorf("bb%d instr %d: select ready target bb%d does not exist", i, j, ins.Select.ReadyBB))
				}
				if !blockExists(ins.Select.PendBB) {
					errs = append(errs, fmt.Errorf("bb%d instr %d: select pending target bb%d does not exist", i, j, ins.Select.PendBB))
				}
			case InstrCrossing:
				if ins.Crossing.ReadyBB != NoBlockID && !blockExists(ins.Crossing.ReadyBB) {
					errs = append(errs, fmt.Errorf("bb%d instr %d: crossing ready target bb%d does not exist", i, j, ins.Crossing.ReadyBB))
				}
				if ins.Crossing.PendBB != NoBlockID && !blockExists(ins.Crossing.PendBB) {
					errs = append(errs, fmt.Errorf("bb%d instr %d: crossing pending target bb%d does not exist", i, j, ins.Crossing.PendBB))
				}
			}
		}
		switch bb.Term.Kind {
		case TermGoto:
			if !blockExists(bb.Term.Goto.Target) {
				errs = append(errs, fmt.Errorf("bb%d: goto target bb%d does not exist", i, bb.Term.Goto.Target))
			}
		case TermIf:
			if !blockExists(bb.Term.If.Then) {
				errs = append(errs, fmt.Errorf("bb%d: if then target bb%d does not exist", i, bb.Term.If.Then))
			}
			if !blockExists(bb.Term.If.Else) {
				errs = append(errs, fmt.Errorf("bb%d: if else target bb%d does not exist", i, bb.Term.If.Else))
			}
		case TermSwitchTag:
			// Check for duplicate tag names
			seenTags := make(map[string]bool)
			for j, c := range bb.Term.SwitchTag.Cases {
				if seenTags[c.TagName] {
					errs = append(errs, fmt.Errorf("bb%d: switch_tag has duplicate case for tag %s", i, c.TagName))
				}
				seenTags[c.TagName] = true

				if !blockExists(c.Target) {
					errs = append(errs, fmt.Errorf("bb%d: switch_tag case %d (%s) target bb%d does not exist",
						i, j, c.TagName, c.Target))
				}
			}
			if !blockExists(bb.Term.SwitchTag.Default) {
				errs = append(errs, fmt.Errorf("bb%d: switch_tag default target bb%d does not exist",
					i, bb.Term.SwitchTag.Default))
			}
		}
	}
	return errors.Join(errs...)
}

// validateLocalIDs checks that all LocalID/global references are valid.
func validateLocalIDs(f *Func, globals []Global) error {
	var errs []error

	localExists := func(id LocalID) bool {
		return id >= 0 && int(id) < len(f.Locals)
	}
	globalExists := func(id GlobalID) bool {
		return id >= 0 && int(id) < len(globals)
	}

	checkPlace := func(p Place, context string) {
		switch p.Kind {
		case PlaceGlobal:
			if p.Global != NoGlobalID && !globalExists(p.Global) {
				errs = append(errs, fmt.Errorf("%s: global G%d does not exist", context, p.Global))
			}
		default:
			if p.Local != NoLocalID && !localExists(p.Local) {
				errs = append(errs, fmt.Errorf("%s: local L%d does not exist", context, p.Local))
			}
		}
		for _, proj := range p.Proj {
			if proj.Kind == PlaceProjIndex && proj.IndexLocal != NoLocalID && !localExists(proj.IndexLocal) {
				errs = append(errs, fmt.Errorf("%s: index local L%d does not exist", context, proj.IndexLocal))
			}
		}
	}

	checkOperand := func(op Operand, context string) {
		switch op.Kind {
		case OperandCopy, OperandMove, OperandAddrOf, OperandAddrOfMut:
			checkPlace(op.Place, context)
		}
	}

	checkRValue := func(rv *RValue, context string) {
		switch rv.Kind {
		case RValueUse:
			checkOperand(rv.Use, context)
		case RValueUnaryOp:
			checkOperand(rv.Unary.Operand, context)
		case RValueBinaryOp:
			checkOperand(rv.Binary.Left, context)
			checkOperand(rv.Binary.Right, context)
		case RValueCast:
			checkOperand(rv.Cast.Value, context)
		case RValueStructLit:
			for i := range rv.StructLit.Fields {
				checkOperand(rv.StructLit.Fields[i].Value, context)
			}
		case RValueArrayLit:
			for i := range rv.ArrayLit.Elems {
				checkOperand(rv.ArrayLit.Elems[i], context)
			}
		case RValueTupleLit:
			for i := range rv.TupleLit.Elems {
				checkOperand(rv.TupleLit.Elems[i], context)
			}
		case RValueField:
			checkOperand(rv.Field.Object, context)
		case RValueIndex:
			checkOperand(rv.Index.Object, context)
			checkOperand(rv.Index.Index, context)
		case RValueTagTest:
			checkOperand(rv.TagTest.Value, context)
		case RValueTagPayload:
			checkOperand(rv.TagPayload.Value, context)
		case RValueIterInit:
			checkOperand(rv.IterInit.Iterable, context)
		case RValueIterNext:
			checkOperand(rv.IterNext.Iter, context)
		case RValueTypeTest:
			checkOperand(rv.TypeTest.Value, context)
		case RValueHeirTest:
			checkOperand(rv.HeirTest.Value, context)
		}
	}

	for i := range f.Blocks {
		bb := &f.Blocks[i]
		for j := range bb.Instrs {
			ins := &bb.Instrs[j]
			ctx := fmt.Sprintf("bb%d instr %d", i, j)

			switch ins.Kind {
			case InstrAssign:
				checkPlace(ins.Assign.Dst, ctx)
				checkRValue(&ins.Assign.Src, ctx)
			case InstrCall:
				if ins.Call.HasDst {
					checkPlace(ins.Call.Dst, ctx)
				}
				if ins.Call.Callee.Kind == CalleeValue {
					checkOperand(ins.Call.Callee.Value, ctx)
				}
				for i := range ins.Call.Args {
					checkOperand(ins.Call.Args[i], ctx)
				}
			case InstrDrop:
				checkPlace(ins.Drop.Place, ctx)
			case InstrEndBorrow:
				checkPlace(ins.EndBorrow.Place, ctx)
			case InstrAwait:
				checkPlace(ins.Await.Dst, ctx)
				checkOperand(ins.Await.Task, ctx)
			case InstrSpawn:
				checkPlace(ins.Spawn.Dst, ctx)
				checkOperand(ins.Spawn.Value, ctx)
			case InstrCrossing:
				checkPlace(ins.Crossing.Dst, ctx)
				checkPlace(ins.Crossing.Pending, ctx)
				if ins.Crossing.Kind == sema.CrossingLoweringSpawnOn ||
					ins.Crossing.Kind == sema.CrossingLoweringChannelCreate ||
					ins.Crossing.Kind == sema.CrossingLoweringChannelShare {
					checkPlace(ins.Crossing.Handle, ctx)
				}
				checkOperand(ins.Crossing.Destination.Value, ctx)
				for i := range ins.Crossing.State.Fields {
					checkOperand(ins.Crossing.State.Fields[i].Value, ctx)
				}
				for i := range ins.Crossing.Captures {
					checkOperand(ins.Crossing.Captures[i].Value, ctx)
				}
				for i := range ins.Crossing.RemoteOps {
					checkOperand(ins.Crossing.RemoteOps[i].Receiver, ctx)
					checkOperand(ins.Crossing.RemoteOps[i].Value, ctx)
				}
				checkOperand(ins.Crossing.Receiver, ctx)
			case InstrBlocking:
				checkPlace(ins.Blocking.Dst, ctx)
				for i := range ins.Blocking.State.Fields {
					checkOperand(ins.Blocking.State.Fields[i].Value, ctx)
				}
			case InstrPoll:
				checkPlace(ins.Poll.Dst, ctx)
				checkOperand(ins.Poll.Task, ctx)
			case InstrJoinAll:
				checkPlace(ins.JoinAll.Dst, ctx)
				checkOperand(ins.JoinAll.Scope, ctx)
			case InstrChanSend:
				checkOperand(ins.ChanSend.Channel, ctx)
				checkOperand(ins.ChanSend.Value, ctx)
			case InstrChanRecv:
				checkPlace(ins.ChanRecv.Dst, ctx)
				checkOperand(ins.ChanRecv.Channel, ctx)
			case InstrNetWait:
				checkOperand(ins.NetWait.Handle, ctx)
			case InstrTimeout:
				checkPlace(ins.Timeout.Dst, ctx)
				checkOperand(ins.Timeout.Task, ctx)
				checkOperand(ins.Timeout.Ms, ctx)
			case InstrSelect:
				checkPlace(ins.Select.Dst, ctx)
				for i := range ins.Select.Arms {
					arm := &ins.Select.Arms[i]
					switch arm.Kind {
					case SelectArmTask:
						checkOperand(arm.Task, ctx)
					case SelectArmChanRecv:
						checkOperand(arm.Channel, ctx)
					case SelectArmChanSend:
						checkOperand(arm.Channel, ctx)
						checkOperand(arm.Value, ctx)
					case SelectArmTimeout:
						checkOperand(arm.Task, ctx)
						checkOperand(arm.Ms, ctx)
					}
				}
			}
		}

		// Check terminator operands
		ctx := fmt.Sprintf("bb%d terminator", i)
		switch bb.Term.Kind {
		case TermReturn:
			if bb.Term.Return.HasValue {
				checkOperand(bb.Term.Return.Value, ctx)
			}
		case TermAsyncYield:
			checkOperand(bb.Term.AsyncYield.State, ctx)
		case TermAsyncReturn:
			checkOperand(bb.Term.AsyncReturn.State, ctx)
			if bb.Term.AsyncReturn.HasValue {
				checkOperand(bb.Term.AsyncReturn.Value, ctx)
			}
		case TermAsyncReturnCancelled:
			checkOperand(bb.Term.AsyncReturnCancelled.State, ctx)
		case TermIf:
			checkOperand(bb.Term.If.Cond, ctx)
		case TermSwitchTag:
			checkOperand(bb.Term.SwitchTag.Value, ctx)
		}
	}

	return errors.Join(errs...)
}

// validateReturn checks that return statements match function signature.
func validateReturn(f *Func, typesIn *types.Interner) error {
	var errs []error

	// If Result is NoTypeID, we can't determine if it's a nothing function or
	// if the return type was simply not resolved (e.g., for generic functions
	// where monomorphization didn't set Result). Skip validation in this case.
	if f.Result == types.NoTypeID {
		return nil
	}

	isNothing := isNothingType(typesIn, f.Result)

	for i := range f.Blocks {
		bb := &f.Blocks[i]
		if bb.Term.Kind != TermReturn {
			continue
		}

		if isNothing && bb.Term.Return.HasValue {
			errs = append(errs, fmt.Errorf("bb%d: return with value in nothing function", i))
		}
		if !isNothing && !bb.Term.Return.HasValue {
			errs = append(errs, fmt.Errorf("bb%d: return without value in non-nothing function", i))
		}
	}

	return errors.Join(errs...)
}

// isNothingType checks if a type is the nothing type.
func isNothingType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindNothing
}

// validateEndBorrow checks that EndBorrow is only used on reference locals.
func validateEndBorrow(f *Func, globals []Global) error {
	var errs []error

	for i := range f.Blocks {
		bb := &f.Blocks[i]
		for j := range bb.Instrs {
			ins := &bb.Instrs[j]
			if ins.Kind != InstrEndBorrow {
				continue
			}

			place := ins.EndBorrow.Place
			if place.Kind == PlaceGlobal {
				if place.Global < 0 || int(place.Global) >= len(globals) {
					continue // Already reported by validateLocalIDs
				}
				continue
			}

			localID := place.Local
			if localID < 0 || int(localID) >= len(f.Locals) {
				continue // Already reported by validateLocalIDs
			}

			loc := f.Locals[localID]
			if loc.Flags&(LocalFlagRef|LocalFlagRefMut) == 0 {
				errs = append(errs, fmt.Errorf("bb%d instr %d: end_borrow on non-reference local L%d (%s)",
					i, j, localID, loc.Name))
			}
		}
	}

	return errors.Join(errs...)
}

// validateDrop checks that Drop is only used on non-copy, non-reference locals.
func validateDrop(f *Func, globals []Global) error {
	var errs []error

	for i := range f.Blocks {
		bb := &f.Blocks[i]
		for j := range bb.Instrs {
			ins := &bb.Instrs[j]
			if ins.Kind != InstrDrop {
				continue
			}

			place := ins.Drop.Place
			if place.Kind == PlaceGlobal {
				if place.Global < 0 || int(place.Global) >= len(globals) {
					continue // Already reported by validateLocalIDs
				}
				continue
			}

			localID := place.Local
			if localID < 0 || int(localID) >= len(f.Locals) {
				continue // Already reported by validateLocalIDs
			}

			loc := f.Locals[localID]
			if loc.Flags&LocalFlagCopy != 0 {
				errs = append(errs, fmt.Errorf("bb%d instr %d: drop on copy local L%d (%s)",
					i, j, localID, loc.Name))
			}
			if loc.Flags&(LocalFlagRef|LocalFlagRefMut) != 0 {
				errs = append(errs, fmt.Errorf("bb%d instr %d: drop on reference local L%d (%s) (use end_borrow)",
					i, j, localID, loc.Name))
			}
		}
	}

	return errors.Join(errs...)
}

func validateCrossingSupport(f *Func, opts ValidateOptions) error {
	if f == nil {
		return nil
	}
	var errs []error
	for bi := range f.Blocks {
		bb := &f.Blocks[bi]
		for ii := range bb.Instrs {
			ins := &bb.Instrs[ii]
			if ins.Kind != InstrCrossing {
				continue
			}
			if opts.crossingEnabled(ins.Crossing.Kind) {
				continue
			}
			errs = append(errs, fmt.Errorf("bb%d instr %d: crossing %s is not enabled", bi, ii, mirCrossingKindName(ins.Crossing.Kind)))
		}
	}
	return errors.Join(errs...)
}
