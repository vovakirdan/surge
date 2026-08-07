package vm

import (
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/types"
)

func (vm *VM) evalTypeTest(frame *Frame, tt *mir.TypeTest) (Value, *VMError) {
	if tt == nil {
		return Value{}, vm.eb.unimplemented("nil type_test")
	}
	val, vmErr := vm.evalOperand(frame, &tt.Value)
	if vmErr != nil {
		return Value{}, vmErr
	}
	defer vm.dropValue(val)
	leftType := vm.valueTypeForTest(val)
	ok := vm.sameTypeIgnoringOwn(leftType, tt.TargetTy)
	return MakeBool(ok, types.NoTypeID), nil
}

func (vm *VM) evalHeirTest(frame *Frame, ht *mir.HeirTest) (Value, *VMError) {
	if ht == nil {
		return Value{}, vm.eb.unimplemented("nil heir_test")
	}
	val, vmErr := vm.evalOperand(frame, &ht.Value)
	if vmErr != nil {
		return Value{}, vmErr
	}
	defer vm.dropValue(val)
	leftType := vm.valueTypeForTest(val)
	ok := vm.typeHeir(leftType, ht.TargetTy)
	return MakeBool(ok, types.NoTypeID), nil
}

func (vm *VM) valueTypeForTest(val Value) types.TypeID {
	if val.TypeID != types.NoTypeID {
		return val.TypeID
	}
	if vm.Types == nil {
		return types.NoTypeID
	}
	if val.Kind == VKNothing {
		return vm.Types.Builtins().Nothing
	}
	return types.NoTypeID
}

func (vm *VM) sameTypeIgnoringOwn(left, right types.TypeID) bool {
	left = vm.stripOwnType(left)
	right = vm.stripOwnType(right)
	if left == types.NoTypeID || right == types.NoTypeID {
		return false
	}
	return left == right
}

func (vm *VM) stripOwnType(id types.TypeID) types.TypeID {
	if id == types.NoTypeID || vm.Types == nil {
		return id
	}
	for range 32 {
		tt, ok := vm.Types.Lookup(id)
		if !ok || tt.Kind != types.KindOwn {
			return id
		}
		id = tt.Elem
	}
	return id
}

func (vm *VM) typeHeir(left, right types.TypeID) bool {
	if vm.Types == nil {
		return false
	}
	left = vm.stripOwnType(left)
	right = vm.stripOwnType(right)
	if left == types.NoTypeID || right == types.NoTypeID {
		return false
	}
	if left == right {
		return true
	}
	rightIsUnion := vm.isUnionType(right)
	seen := map[types.TypeID]struct{}{left: {}}
	queue := []types.TypeID{left}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == right {
			return true
		}
		if rightIsUnion && vm.unionContains(right, cur) {
			return true
		}
		tt, ok := vm.Types.Lookup(cur)
		if !ok {
			continue
		}
		if tt.Kind == types.KindAlias {
			if target, ok := vm.Types.AliasTarget(cur); ok {
				target = vm.stripOwnType(target)
				if _, exists := seen[target]; !exists {
					seen[target] = struct{}{}
					queue = append(queue, target)
				}
			}
		}
		if base, ok := vm.Types.StructBase(cur); ok {
			base = vm.stripOwnType(base)
			if _, exists := seen[base]; !exists {
				seen[base] = struct{}{}
				queue = append(queue, base)
			}
		}
	}
	return false
}

// retagUnionValue adapts a value to the union type its destination declares.
//
// Under a box this was a RELABEL — write a different TypeID onto the object and
// change nothing else — and it could afford to be, because the object carried
// its members as values and no layout depended on which union named it. Storage
// removed that freedom: two unions order their arms and place their payloads
// independently, so the same arm sits somewhere different in each. The work is
// therefore a conversion, and retagUnion does it.
//
// There are three answers here, not two, and collapsing them is how a real
// failure became a misleading one.
//
// A conversion that DOES NOT APPLY leaves the value alone and reports false
// without raising: the value is not a union of a convertible shape, the store
// this feeds is about to refuse it anyway, and that refusal names both types
// where a refusal here would name only the moment somebody asked.
//
// A conversion that applies and FAILS is a different thing entirely — the arms
// matched but the payload could not be moved, the destination could not be
// built, the discriminant could not be written. That was reported as "does not
// apply" too, which handed the caller back the unconverted value and let the
// store fail later for a reason that had nothing to do with the actual fault.
// It is how a precise storage error once surfaced as `unimplemented: __to
// conversion`. The error is returned now, and every caller propagates it.
func (vm *VM) retagUnionValue(val Value, expected types.TypeID) (Value, bool, *VMError) {
	if vm == nil || vm.Types == nil || expected == types.NoTypeID {
		return val, false, nil
	}
	ref, isTag := vm.isTagStorage(val)
	if !isTag {
		return val, false, nil
	}
	converted, changed, vmErr := vm.retagUnion(vm.currentFrame(), ref, expected)
	if vmErr != nil {
		return val, false, vmErr
	}
	if !changed {
		return val, false, nil
	}
	return converted, true, nil
}

func (vm *VM) isUnionType(id types.TypeID) bool {
	if id == types.NoTypeID || vm.Types == nil {
		return false
	}
	tt, ok := vm.Types.Lookup(id)
	return ok && tt.Kind == types.KindUnion
}

func (vm *VM) unionContains(unionType, candidate types.TypeID) bool {
	if vm.Types == nil || unionType == types.NoTypeID || candidate == types.NoTypeID {
		return false
	}
	tt, ok := vm.Types.Lookup(unionType)
	if !ok || tt.Kind != types.KindUnion {
		return false
	}
	info, ok := vm.Types.UnionInfo(unionType)
	if !ok || info == nil {
		return false
	}
	candidate = vm.stripOwnType(candidate)
	for _, member := range info.Members {
		switch member.Kind {
		case types.UnionMemberNothing:
			if candidate == vm.Types.Builtins().Nothing {
				return true
			}
		case types.UnionMemberType:
			if vm.stripOwnType(member.Type) == candidate {
				return true
			}
		case types.UnionMemberTag:
			if vm.tagTypeMatches(candidate, member.TagName, member.TagArgs) {
				return true
			}
		}
	}
	return false
}

func (vm *VM) tagTypeMatches(candidate types.TypeID, tagName source.StringID, tagArgs []types.TypeID) bool {
	if vm.Types == nil || candidate == types.NoTypeID || tagName == source.NoStringID {
		return false
	}
	info, ok := vm.Types.UnionInfo(candidate)
	if !ok || info == nil || info.Name != tagName {
		return false
	}
	if len(info.TypeArgs) != len(tagArgs) {
		return false
	}
	for i := range info.TypeArgs {
		if !vm.sameTypeIgnoringOwn(info.TypeArgs[i], tagArgs[i]) {
			return false
		}
	}
	return true
}
