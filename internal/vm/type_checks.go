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

func (vm *VM) evalHeirTest(ht *mir.HeirTest) (Value, *VMError) {
	if ht == nil {
		return Value{}, vm.eb.unimplemented("nil heir_test")
	}
	ok := vm.typeHeir(ht.LeftTy, ht.RightTy)
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
