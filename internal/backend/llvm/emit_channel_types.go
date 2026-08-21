package llvm

import (
	"surge/internal/mir"
	"surge/internal/types"
)

// isFarChannelType reports whether typeID is `far Channel<T>` (any
// wrapping of &/own/* around the far qualifier is stripped by
// resolveValueType first, matching isChannelType's own unwrap). Used to
// route a far-channel-typed binding's scope-exit drop to the runtime's
// far-handle release instead of the ordinary local-channel leaf path,
// which isFarChannelType-gated callers never reach: resolveValueType
// stops at a KindFar node (it does not unwrap the far qualifier itself,
// only Alias/Own/Reference/Pointer), so a local Channel<T> and a far
// Channel<T> are never confused here.
func isFarChannelType(typesIn *types.Interner, typeID types.TypeID) bool {
	if typesIn == nil || typeID == types.NoTypeID {
		return false
	}
	typeID = resolveValueType(typesIn, typeID)
	tt, ok := typesIn.Lookup(typeID)
	if !ok || tt.Kind != types.KindFar {
		return false
	}
	return isChannelType(typesIn, tt.Elem)
}

// channelElemType extracts T from Channel<T> (any wrapping the resolved
// struct/alias node itself does not already strip — callers pass a
// resolveValueType'd id), mirroring sema's channelPayloadType.
func channelElemType(typesIn *types.Interner, channelType types.TypeID) types.TypeID {
	if typesIn == nil || channelType == types.NoTypeID {
		return types.NoTypeID
	}
	if info, ok := typesIn.StructInfo(channelType); ok && info != nil && typesIn.Strings != nil {
		if name, nameOK := typesIn.Strings.Lookup(info.Name); nameOK && name == "Channel" {
			if args := typesIn.StructArgs(channelType); len(args) == 1 {
				return args[0]
			}
		}
	}
	if info, ok := typesIn.AliasInfo(channelType); ok && info != nil && typesIn.Strings != nil {
		if name, nameOK := typesIn.Strings.Lookup(info.Name); nameOK && name == "Channel" && len(info.TypeArgs) == 1 {
			return info.TypeArgs[0]
		}
	}
	return types.NoTypeID
}

// callDstIsChannel answers whether a call spelled `new` builds a channel.
// The name is shared with RwLock's intrinsic `new` and with the ordinary `new`
// functions on Mutex, Condition, Semaphore and Barrier, so the destination type
// is what tells them apart — the same question the VM asks.
// A constructor whose result is discarded carries no destination to read, so it
// is left to the ordinary call path rather than guessed at.
func (fe *funcEmitter) callDstIsChannel(call *mir.CallInstr) bool {
	if call == nil || fe == nil || !call.HasDst {
		return false
	}
	dstType, err := fe.placeBaseType(call.Dst)
	if err != nil {
		return false
	}
	return isChannelType(fe.emitter.types, dstType)
}
