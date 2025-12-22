package mir

import (
	"errors"
	"fmt"

	"surge/internal/types"
)

// Validate checks MIR module invariants.
// Returns error if any invariant is violated.
func Validate(m *Module, typesIn *types.Interner) error {
	if m == nil {
		return nil
	}
	var errs []error
	for _, f := range m.Funcs {
		if f == nil {
			continue
		}
		if err := validateFunc(f, typesIn); err != nil {
			errs = append(errs, fmt.Errorf("function %s: %w", f.Name, err))
		}
	}
	return errors.Join(errs...)
}

func validateFunc(f *Func, typesIn *types.Interner) error {
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
	if err := validateLocalIDs(f); err != nil {
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
	if err := validateEndBorrow(f); err != nil {
		errs = append(errs, err)
	}

	// 8. Check Drop validity
	if err := validateDrop(f); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// validateBlocksTerminated checks that every block ends with a terminator.
func validateBlocksTerminated(f *Func) error {
	var errs []error
	for i := range f.Blocks {
		if f.Blocks[i].Term.Kind == TermNone {
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

// validateLocalIDs checks that all LocalID references are valid.
func validateLocalIDs(f *Func) error {
	var errs []error

	localExists := func(id LocalID) bool {
		return id >= 0 && int(id) < len(f.Locals)
	}

	checkPlace := func(p Place, context string) {
		if p.Local != NoLocalID && !localExists(p.Local) {
			errs = append(errs, fmt.Errorf("%s: local L%d does not exist", context, p.Local))
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
			for _, elem := range rv.ArrayLit.Elems {
				checkOperand(elem, context)
			}
		case RValueTupleLit:
			for _, elem := range rv.TupleLit.Elems {
				checkOperand(elem, context)
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
				for _, arg := range ins.Call.Args {
					checkOperand(arg, ctx)
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
			}
		}

		// Check terminator operands
		ctx := fmt.Sprintf("bb%d terminator", i)
		switch bb.Term.Kind {
		case TermReturn:
			if bb.Term.Return.HasValue {
				checkOperand(bb.Term.Return.Value, ctx)
			}
		case TermIf:
			checkOperand(bb.Term.If.Cond, ctx)
		case TermSwitchTag:
			checkOperand(bb.Term.SwitchTag.Value, ctx)
		}
	}

	return errors.Join(errs...)
}

// validateTypes checks for TypeParam and unknown types.
func validateTypes(f *Func, typesIn *types.Interner) error {
	var errs []error

	// Check all locals have valid types
	for i, loc := range f.Locals {
		if loc.Type == types.NoTypeID {
			errs = append(errs, fmt.Errorf("local L%d (%s): unknown type", i, loc.Name))
		}
		if typesIn != nil && typeContainsParam(typesIn, loc.Type, nil) {
			errs = append(errs, fmt.Errorf("local L%d (%s): type contains generic parameter", i, loc.Name))
		}
	}

	// Check function result type
	if f.Result != types.NoTypeID && typesIn != nil {
		if typeContainsParam(typesIn, f.Result, nil) {
			errs = append(errs, fmt.Errorf("result type contains generic parameter"))
		}
	}

	return errors.Join(errs...)
}

// typeContainsParam recursively checks if a type contains any generic parameter.
func typeContainsParam(typesIn *types.Interner, id types.TypeID, seen map[types.TypeID]struct{}) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}

	if seen == nil {
		seen = make(map[types.TypeID]struct{})
	}
	if _, ok := seen[id]; ok {
		return false
	}
	seen[id] = struct{}{}

	tt, ok := typesIn.Lookup(id)
	if !ok {
		return false
	}

	switch tt.Kind {
	case types.KindGenericParam:
		return true
	case types.KindPointer, types.KindReference, types.KindOwn, types.KindArray:
		return typeContainsParam(typesIn, tt.Elem, seen)
	case types.KindTuple:
		if info, ok := typesIn.TupleInfo(id); ok {
			for _, elem := range info.Elems {
				if typeContainsParam(typesIn, elem, seen) {
					return true
				}
			}
		}
	case types.KindFn:
		if info, ok := typesIn.FnInfo(id); ok {
			for _, param := range info.Params {
				if typeContainsParam(typesIn, param, seen) {
					return true
				}
			}
			if typeContainsParam(typesIn, info.Result, seen) {
				return true
			}
		}
	case types.KindStruct:
		if info, ok := typesIn.StructInfo(id); ok {
			for _, field := range info.Fields {
				if typeContainsParam(typesIn, field.Type, seen) {
					return true
				}
			}
		}
	case types.KindUnion:
		if info, ok := typesIn.UnionInfo(id); ok {
			for _, member := range info.Members {
				if typeContainsParam(typesIn, member.Type, seen) {
					return true
				}
			}
		}
	case types.KindAlias:
		if target, ok := typesIn.AliasTarget(id); ok {
			return typeContainsParam(typesIn, target, seen)
		}
	}

	return false
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
func validateEndBorrow(f *Func) error {
	var errs []error

	for i := range f.Blocks {
		bb := &f.Blocks[i]
		for j := range bb.Instrs {
			ins := &bb.Instrs[j]
			if ins.Kind != InstrEndBorrow {
				continue
			}

			localID := ins.EndBorrow.Place.Local
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
func validateDrop(f *Func) error {
	var errs []error

	for i := range f.Blocks {
		bb := &f.Blocks[i]
		for j := range bb.Instrs {
			ins := &bb.Instrs[j]
			if ins.Kind != InstrDrop {
				continue
			}

			localID := ins.Drop.Place.Local
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
