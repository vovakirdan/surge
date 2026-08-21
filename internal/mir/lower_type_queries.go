package mir

import (
	"surge/internal/source"
	"surge/internal/types"
)

// What the lowerer needs to know ABOUT a type, as opposed to what it does with
// one.
//
// Every function here answers a yes/no or unwraps one layer: is this copied or
// moved, does it own heap, is it a Task, what does a Task carry. They are
// grouped because they are the lowerer's questions to the type system, and
// keeping them together makes it obvious when two of them disagree.

func (l *funcLowerer) isCopyType(ty types.TypeID) bool {
	if l == nil || ty == types.NoTypeID {
		return false
	}
	if l.sema != nil {
		return l.sema.IsCopyType(ty)
	}
	if l.types == nil {
		return false
	}
	return l.types.IsCopy(resolveAlias(l.types, ty))
}

// ownsHeap is the lowering-side leg of sema's OwnsHeap axis
// (`internal/sema/ownership_axes.go`): does scope exit have to reclaim
// something for this type? It gates every InstrDrop this package emits.
// Without a sema result it falls back to the interner's Copy bit, which is
// what the drop sites read before this axis was named.
func (l *funcLowerer) ownsHeap(ty types.TypeID) bool {
	if l == nil || ty == types.NoTypeID {
		return false
	}
	if l.sema != nil {
		return l.sema.OwnsHeap(ty)
	}
	return !l.isCopyType(ty)
}

func (l *funcLowerer) isNothingType(ty types.TypeID) bool {
	if l == nil || l.types == nil || ty == types.NoTypeID {
		return false
	}
	tt, ok := l.types.Lookup(resolveAlias(l.types, ty))
	return ok && tt.Kind == types.KindNothing
}

func (l *funcLowerer) isTaskType(ty types.TypeID) bool {
	if l == nil || l.types == nil || ty == types.NoTypeID {
		return false
	}
	resolved := resolveAlias(l.types, ty)
	if info, ok := l.types.StructInfo(resolved); ok && info != nil {
		return l.typeNameMatches(info.Name, "Task")
	}
	if info, ok := l.types.AliasInfo(resolved); ok && info != nil {
		return l.typeNameMatches(info.Name, "Task")
	}
	return false
}

func (l *funcLowerer) isChannelType(ty types.TypeID) bool {
	if l == nil || l.types == nil || ty == types.NoTypeID {
		return false
	}
	base := l.unwrapContainerType(ty)
	resolved := resolveAlias(l.types, base)
	if info, ok := l.types.StructInfo(resolved); ok && info != nil {
		return l.typeNameMatches(info.Name, "Channel")
	}
	if info, ok := l.types.AliasInfo(resolved); ok && info != nil {
		return l.typeNameMatches(info.Name, "Channel")
	}
	return false
}

// isFarChannelType reports a `far Channel<T>` handle (the far wrapper is NOT
// unwrapped by unwrapContainerType, deliberately: a far channel is a token,
// not a local channel value).
func (l *funcLowerer) isFarChannelType(ty types.TypeID) bool {
	if l == nil || l.types == nil || ty == types.NoTypeID {
		return false
	}
	tt, ok := l.types.Lookup(resolveAlias(l.types, ty))
	if !ok || tt.Kind != types.KindFar {
		return false
	}
	return l.isChannelType(tt.Elem)
}

func (l *funcLowerer) unwrapContainerType(id types.TypeID) types.TypeID {
	if l == nil || l.types == nil || id == types.NoTypeID {
		return id
	}
	seen := 0
	for id != types.NoTypeID && seen < 16 {
		seen++
		tt, ok := l.types.Lookup(resolveAlias(l.types, id))
		if !ok {
			return id
		}
		switch tt.Kind {
		case types.KindReference, types.KindOwn, types.KindPointer:
			if tt.Elem == types.NoTypeID || tt.Elem == id {
				return id
			}
			id = tt.Elem
		default:
			return id
		}
	}
	return id
}

func (l *funcLowerer) taskPayloadType(task types.TypeID) (types.TypeID, bool) {
	if l == nil || l.types == nil || task == types.NoTypeID {
		return types.NoTypeID, false
	}
	resolved := resolveAlias(l.types, task)
	if info, ok := l.types.StructInfo(resolved); ok && info != nil {
		if !l.typeNameMatches(info.Name, "Task") {
			return types.NoTypeID, false
		}
		if args := l.types.StructArgs(resolved); len(args) == 1 {
			return args[0], true
		}
		return types.NoTypeID, false
	}
	if info, ok := l.types.AliasInfo(resolved); ok && info != nil {
		if !l.typeNameMatches(info.Name, "Task") {
			return types.NoTypeID, false
		}
		if args := l.types.AliasArgs(resolved); len(args) == 1 {
			return args[0], true
		}
	}
	return types.NoTypeID, false
}

func (l *funcLowerer) typeNameMatches(nameID source.StringID, name string) bool { //nolint:unparam // name may vary in future
	if l == nil || l.types == nil || l.types.Strings == nil || nameID == source.NoStringID {
		return false
	}
	got, ok := l.types.Strings.Lookup(nameID)
	return ok && got == name
}

func resolveAlias(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil {
		return id
	}
	seen := 0
	for id != types.NoTypeID && seen < 32 {
		tt, ok := typesIn.Lookup(id)
		if !ok || tt.Kind != types.KindAlias {
			return id
		}
		target, ok := typesIn.AliasTarget(id)
		if !ok || target == types.NoTypeID || target == id {
			return id
		}
		id = target
		seen++
	}
	return id
}
