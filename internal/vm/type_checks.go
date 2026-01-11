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

func (vm *VM) retagUnionValue(val Value, expected types.TypeID) (Value, bool) {
	if vm == nil || vm.Types == nil || expected == types.NoTypeID {
		return val, false
	}
	if val.Kind != VKHandleTag {
		return val, false
	}
	targetVal := vm.valueType(expected)
	valType := vm.valueType(val.TypeID)
	if targetVal == types.NoTypeID || valType == types.NoTypeID || targetVal == valType {
		return val, false
	}
	if vm.tagLayouts != nil {
		if layout, ok := vm.tagLayouts.Layout(targetVal); ok && layout != nil {
			if val.H != 0 {
				if obj := vm.Heap.Get(val.H); obj != nil && obj.Kind == OKTag {
					if _, ok := layout.CaseBySym(obj.Tag.TagSym); ok {
						val.TypeID = expected
						obj.TypeID = expected
						return val, true
					}
				}
			}
		}
	}
	if !vm.isUnionType(targetVal) || !vm.unionContains(targetVal, valType) {
		return val, false
	}
	val.TypeID = expected
	if val.H != 0 {
		if obj := vm.Heap.Get(val.H); obj != nil && obj.Kind == OKTag {
			obj.TypeID = expected
		}
	}
	return val, true
}

func (vm *VM) compatiblePayloadTypes(expected, got types.TypeID) bool {
	if expected == got {
		return true
	}
	if expected == types.NoTypeID || got == types.NoTypeID || vm.Types == nil {
		return false
	}
	expVal := vm.valueType(expected)
	gotVal := vm.valueType(got)
	if expVal == gotVal {
		return true
	}
	if expElem, ok := vm.Types.ArrayInfo(expVal); ok {
		if gotElem, ok := vm.Types.ArrayInfo(gotVal); ok {
			return vm.compatiblePayloadTypes(expElem, gotElem)
		}
	}
	if expElem, expLen, ok := vm.Types.ArrayFixedInfo(expVal); ok {
		if gotElem, gotLen, ok := vm.Types.ArrayFixedInfo(gotVal); ok && expLen == gotLen {
			return vm.compatiblePayloadTypes(expElem, gotElem)
		}
	}
	if expTuple, ok := vm.Types.TupleInfo(expVal); ok && expTuple != nil {
		if gotTuple, ok := vm.Types.TupleInfo(gotVal); ok && gotTuple != nil {
			if len(expTuple.Elems) != len(gotTuple.Elems) {
				return false
			}
			for i := range expTuple.Elems {
				if !vm.compatiblePayloadTypes(expTuple.Elems[i], gotTuple.Elems[i]) {
					return false
				}
			}
			return true
		}
	}
	if expInfo, ok := vm.Types.UnionInfo(expVal); ok && expInfo != nil {
		if gotInfo, ok := vm.Types.UnionInfo(gotVal); ok && gotInfo != nil {
			if expInfo.Name != source.NoStringID && expInfo.Name == gotInfo.Name {
				if len(expInfo.TypeArgs) != len(gotInfo.TypeArgs) {
					return false
				}
				for i := range expInfo.TypeArgs {
					if !vm.compatiblePayloadTypes(expInfo.TypeArgs[i], gotInfo.TypeArgs[i]) {
						return false
					}
				}
				return true
			}
			if len(expInfo.Members) != len(gotInfo.Members) {
				return false
			}
			for i := range expInfo.Members {
				expMember := expInfo.Members[i]
				gotMember := gotInfo.Members[i]
				if expMember.Kind != gotMember.Kind {
					return false
				}
				switch expMember.Kind {
				case types.UnionMemberNothing:
					continue
				case types.UnionMemberType:
					if !vm.compatiblePayloadTypes(expMember.Type, gotMember.Type) {
						return false
					}
				case types.UnionMemberTag:
					if expMember.TagName != gotMember.TagName {
						return false
					}
					if len(expMember.TagArgs) != len(gotMember.TagArgs) {
						return false
					}
					for j := range expMember.TagArgs {
						if !vm.compatiblePayloadTypes(expMember.TagArgs[j], gotMember.TagArgs[j]) {
							return false
						}
					}
				}
			}
			return true
		}
	}
	if vm.tagLayouts != nil {
		if expLayout, ok := vm.tagLayouts.Layout(expVal); ok && expLayout != nil {
			if gotLayout, ok := vm.tagLayouts.Layout(gotVal); ok && gotLayout != nil {
				if len(expLayout.Cases) != len(gotLayout.Cases) {
					return false
				}
				for _, expCase := range expLayout.Cases {
					gotCase, ok := gotLayout.CaseByName(expCase.TagName)
					if !ok {
						return false
					}
					if len(expCase.PayloadTypes) != len(gotCase.PayloadTypes) {
						return false
					}
					for i := range expCase.PayloadTypes {
						if !vm.compatiblePayloadTypes(expCase.PayloadTypes[i], gotCase.PayloadTypes[i]) {
							return false
						}
					}
				}
				return true
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
