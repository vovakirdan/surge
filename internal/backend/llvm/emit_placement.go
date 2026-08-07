package llvm

import (
	"fmt"
	"strings"

	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

const (
	placementKindBits        = 8
	placementKindPool        = uint64(1)
	placementKindDistributed = uint64(2)
	placementKindShard       = uint64(3)
)

func placementEncode(kind, payload uint64) uint64 {
	return (payload << placementKindBits) | kind
}

func (fe *funcEmitter) emitPlacementIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	if call.Callee.Sym.IsValid() && fe.emitter != nil && fe.emitter.mod != nil {
		if _, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
			return false, nil
		}
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "shard" {
		return false, nil
	}
	if !fe.isRuntimePlacementFunction(call.Callee.Sym, name) &&
		!fe.isUnresolvedRuntimePlacementFunction(call, name) {
		return false, nil
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("shard placement intrinsic requires 1 argument")
	}
	if !call.HasDst {
		return true, nil
	}
	dstType := fe.placementCallDstType(call)
	if !isPlacementType(fe.emitter.types, dstType) {
		return true, fmt.Errorf("shard placement intrinsic destination must be Placement")
	}

	shardID, err := fe.emitUintOperandToI64(&call.Args[0], "placement shard id out of range")
	if err != nil {
		return true, err
	}
	shifted := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = shl i64 %s, %d\n", shifted, shardID, placementKindBits)
	encoded := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = or i64 %s, %d\n", encoded, shifted, placementKindShard)
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
	if err != nil {
		return true, err
	}
	if dstTy != "i64" {
		return true, fmt.Errorf("placement destination must lower as i64, got %s", dstTy)
	}
	fe.emitValueStore("i64", encoded, ptr, dstAlign)
	return true, nil
}

func (fe *funcEmitter) isUnresolvedRuntimePlacementFunction(call *mir.CallInstr, name string) bool {
	if fe == nil || call == nil || name != "shard" || !call.HasDst || !call.Callee.Sym.IsValid() {
		return false
	}
	if fe.hasSymbol(call.Callee.Sym) {
		return false
	}
	return isPlacementType(fe.emitter.types, fe.placementCallDstType(call))
}

func (fe *funcEmitter) hasSymbol(symID symbols.SymbolID) bool {
	return fe != nil && fe.emitter != nil && fe.emitter.syms != nil &&
		fe.emitter.syms.Symbols != nil && symID.IsValid() &&
		fe.emitter.syms.Symbols.Get(symID) != nil
}

func (fe *funcEmitter) placementCallDstType(call *mir.CallInstr) types.TypeID {
	if fe == nil || fe.f == nil || call == nil || !call.HasDst {
		return types.NoTypeID
	}
	if call.Dst.Kind == mir.PlaceLocal && int(call.Dst.Local) < len(fe.f.Locals) {
		return fe.f.Locals[call.Dst.Local].Type
	}
	return types.NoTypeID
}

func (fe *funcEmitter) isRuntimePlacementFunction(symID symbols.SymbolID, wantName string) bool {
	if fe == nil || fe.emitter == nil || fe.emitter.syms == nil ||
		fe.emitter.syms.Symbols == nil || fe.emitter.syms.Strings == nil || !symID.IsValid() {
		return false
	}
	sym := fe.emitter.syms.Symbols.Get(symID)
	if sym == nil || sym.Kind != symbols.SymbolFunction || sym.Name == source.NoStringID {
		return false
	}
	if !llvmIsCoreRuntimeSymbol(sym) {
		return false
	}
	return fe.emitter.syms.Strings.MustLookup(sym.Name) == wantName
}

func llvmIsCoreRuntimeSymbol(sym *symbols.Symbol) bool {
	if sym == nil || sym.Flags&symbols.SymbolFlagBuiltin == 0 {
		return false
	}
	if sym.ModulePath != "" {
		trimmed := strings.Trim(sym.ModulePath, "/")
		return trimmed == "core" || strings.HasPrefix(trimmed, "core/")
	}
	return sym.Flags&symbols.SymbolFlagImported != 0
}
