package llvm

import (
	"fmt"
	"strings"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

const (
	mapKeyString = iota + 1
	mapKeyInt
	mapKeyUint
	mapKeyBigInt
	mapKeyBigUint
)

func (fe *funcEmitter) emitMapIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if call.Callee.Sym.IsValid() && fe.emitter != nil && fe.emitter.mod != nil {
		if _, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
			return false, nil
		}
	}
	switch name {
	case "rt_map_new":
		return true, fe.emitMapNew(call)
	case "rt_map_len":
		return true, fe.emitMapLen(call)
	case "rt_map_contains":
		return true, fe.emitMapContains(call)
	case "rt_map_get_ref":
		return true, fe.emitMapGet(call, "rt_map_get_ref")
	case "rt_map_get_mut":
		return true, fe.emitMapGet(call, "rt_map_get_mut")
	case "rt_map_insert":
		return true, fe.emitMapInsert(call)
	case "rt_map_remove":
		return true, fe.emitMapRemove(call)
	case "rt_map_keys":
		return true, fe.emitMapKeys(call)
	default:
		return false, nil
	}
}

func (fe *funcEmitter) emitMapNew(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 0 {
		return fmt.Errorf("rt_map_new requires 0 arguments")
	}
	if !call.HasDst {
		return nil
	}
	dstType, err := fe.placeBaseType(call.Dst)
	if err != nil {
		return err
	}
	keyType, err := fe.mapKeyTypeFromType(dstType)
	if err != nil {
		return err
	}
	keyKind, err := fe.mapKeyKindForType(keyType)
	if err != nil {
		return err
	}
	// The two descriptors are what make the entry storage typed: they carry the
	// stride an entry occupies and the move that relocates one. They are given
	// once, here, because a map's key and value types never change afterwards.
	keyOps, valueOps, err := fe.mapEntryValueOps(dstType)
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_map_new(i64 %d, ptr %s, ptr %s)\n",
		tmp, keyKind, keyOps, valueOps)
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
	if err != nil {
		return err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, tmp, ptr, dstAlign)
	return nil
}

func (fe *funcEmitter) emitMapLen(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 1 {
		return fmt.Errorf("rt_map_len requires 1 argument")
	}
	if _, err := fe.mapCallKeyType(&call.Args[0]); err != nil {
		return err
	}
	handle, err := fe.emitMapHandle(&call.Args[0])
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i64 @rt_map_len(ptr %s)\n", tmp, handle)
	if call.HasDst {
		dstType := types.NoTypeID
		if call.Dst.Kind == mir.PlaceLocal && int(call.Dst.Local) < len(fe.f.Locals) {
			dstType = fe.f.Locals[call.Dst.Local].Type
		}
		if err := fe.emitLenStore(call.Dst, dstType, tmp); err != nil {
			return err
		}
	}
	return nil
}

func (fe *funcEmitter) emitMapContains(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 2 {
		return fmt.Errorf("rt_map_contains requires 2 arguments")
	}
	if _, err := fe.mapCallKeyType(&call.Args[0]); err != nil {
		return err
	}
	handle, err := fe.emitMapHandle(&call.Args[0])
	if err != nil {
		return err
	}
	key, err := fe.emitMapStorageOperand(&call.Args[1])
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_map_contains(ptr %s, ptr %s)\n", tmp, handle, key)
	if call.HasDst {
		ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != "i1" {
			dstTy = "i1"
		}
		fe.emitValueStore(dstTy, tmp, ptr, dstAlign)
	}
	return nil
}

func (fe *funcEmitter) emitMapGet(call *mir.CallInstr, name string) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 2 {
		return fmt.Errorf("%s requires 2 arguments", name)
	}
	if _, err := fe.mapCallKeyType(&call.Args[0]); err != nil {
		return err
	}
	handle, err := fe.emitMapHandle(&call.Args[0])
	if err != nil {
		return err
	}
	key, err := fe.emitMapStorageOperand(&call.Args[1])
	if err != nil {
		return err
	}
	return fe.emitMapOptionCall(call, name, []string{"ptr " + handle, "ptr " + key})
}

func (fe *funcEmitter) emitMapInsert(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 3 {
		return fmt.Errorf("rt_map_insert requires 3 arguments")
	}
	if _, err := fe.mapCallKeyType(&call.Args[0]); err != nil {
		return err
	}
	handle, err := fe.emitMapHandle(&call.Args[0])
	if err != nil {
		return err
	}
	key, err := fe.emitMapStorageOperand(&call.Args[1])
	if err != nil {
		return err
	}
	value, err := fe.emitMapStorageOperand(&call.Args[2])
	if err != nil {
		return err
	}
	// Both addresses name storage the caller is giving up: the map moves out of
	// them, so nothing here copies the value into an allocation of its own
	// first, and no destination means the displaced value is destroyed rather
	// than written to a slot nobody reads.
	return fe.emitMapOptionCall(call, "rt_map_insert",
		[]string{"ptr " + handle, "ptr " + key, "ptr " + value})
}

func (fe *funcEmitter) emitMapRemove(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 2 {
		return fmt.Errorf("rt_map_remove requires 2 arguments")
	}
	if _, err := fe.mapCallKeyType(&call.Args[0]); err != nil {
		return err
	}
	handle, err := fe.emitMapHandle(&call.Args[0])
	if err != nil {
		return err
	}
	key, err := fe.emitMapStorageOperand(&call.Args[1])
	if err != nil {
		return err
	}
	return fe.emitMapOptionCall(call, "rt_map_remove", []string{"ptr " + handle, "ptr " + key})
}

func (fe *funcEmitter) emitMapKeys(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 1 {
		return fmt.Errorf("rt_map_keys requires 1 argument")
	}
	keyType, err := fe.mapCallKeyType(&call.Args[0])
	if err != nil {
		return err
	}
	keyType = resolveMapKeyType(fe.emitter.types, keyType)
	keyLLVM, err := fe.emitter.llvmValueType(keyType)
	if err != nil {
		return err
	}
	elemSize, elemAlign, err := llvmTypeSizeAlign(keyLLVM)
	if err != nil {
		return err
	}
	if elemSize <= 0 {
		elemSize = 1
	}
	if elemAlign <= 0 {
		elemAlign = 1
	}
	handle, err := fe.emitMapHandle(&call.Args[0])
	if err != nil {
		return err
	}
	duplicate, err := fe.mapKeysDuplication(keyType)
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_map_keys(ptr %s, i64 %d, i64 %d, ptr %s)\n",
		tmp, handle, elemSize, elemAlign, duplicate)
	if call.HasDst {
		ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
		if err != nil {
			return err
		}
		if !isStorageRun(dstTy) {
			dstTy = handleType
		}
		fe.emitValueStore(dstTy, tmp, ptr, dstAlign)
	}
	return nil
}

// mapCallKeyType is what a map intrinsic must establish before it emits
// anything: the receiver really is a `Map`, and its key is one the runtime's
// scan knows how to compare. Both questions are asked here so that six call
// emitters cannot answer them six slightly different ways.
func (fe *funcEmitter) mapCallKeyType(op *mir.Operand) (types.TypeID, error) {
	mapType := operandValueType(fe.emitter.types, op)
	if mapType == types.NoTypeID && op.Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(op.Place); err == nil {
			mapType = baseType
		}
	}
	keyType, err := fe.mapKeyTypeFromType(mapType)
	if err != nil {
		return types.NoTypeID, err
	}
	if _, keyErr := fe.mapKeyKindForType(keyType); keyErr != nil {
		return types.NoTypeID, keyErr
	}
	return keyType, nil
}

// emitMapOptionCall emits one map entry point whose last argument is the
// destination for what it hands back, and tags the `Option` it answers with.
//
// The runtime writes STRAIGHT INTO the destination union's payload: there is no
// intermediate word, no allocation to adopt out of, and no pair of branches to
// build the Some arm in. The tag is the only thing decided here, and a `select`
// is the whole decision.
//
// A call with no destination hands the runtime a null instead, which is not a
// refusal but an instruction: destroy what you would otherwise have handed
// back. That is the case a map literal takes, where the displaced value used to
// be written into an alloca nobody read.
func (fe *funcEmitter) emitMapOptionCall(call *mir.CallInstr, name string, args []string) error {
	leading := strings.Join(args, ", ")
	if !call.HasDst {
		okVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @%s(%s, ptr null)\n", okVal, name, leading)
		return nil
	}
	dstType, err := fe.placeBaseType(call.Dst)
	if err != nil {
		return err
	}
	someIdx, someMeta, err := fe.emitter.tagCaseMeta(dstType, "Some", symbols.NoSymbolID)
	if err != nil {
		return err
	}
	if len(someMeta.PayloadTypes) != 1 {
		return fmt.Errorf("Option::Some expects single payload")
	}
	noneIdx, noneMeta, err := fe.emitter.tagCaseMeta(dstType, "nothing", symbols.NoSymbolID)
	if err != nil {
		return err
	}
	if len(noneMeta.PayloadTypes) != 0 {
		return fmt.Errorf("the nothing case of type#%d expects no payload", dstType)
	}
	layoutInfo, err := fe.emitter.layoutOf(dstType)
	if err != nil {
		return err
	}
	if layoutInfo.TagSize != 4 {
		return fmt.Errorf("unsupported tag size %d for type#%d", layoutInfo.TagSize, dstType)
	}
	align := layoutInfo.Align
	if align == 0 {
		align = 1
	}
	unionCase, ok := layoutInfo.UnionCase(someIdx)
	if !ok {
		return fmt.Errorf("missing finalized union case %d for type#%d", someIdx, dstType)
	}
	payloadOffset, ok := unionCase.FieldOffset(0)
	if !ok {
		return fmt.Errorf("missing finalized payload offset for type#%d case %d", dstType, someIdx)
	}
	mem, err := fe.emitStorageAlloca(dstType)
	if err != nil {
		return err
	}
	payloadPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n",
		payloadPtr, mem, unionCase.PayloadOffset+payloadOffset)
	okVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @%s(%s, ptr %s)\n", okVal, name, leading, payloadPtr)
	// The payload slot is initialized exactly when the tag says Some, which is
	// the same fact the runtime reported, so one value decides both.
	tag := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = select i1 %s, i32 %d, i32 %d\n", tag, okVal, someIdx, noneIdx)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %s, ptr %s, align %d\n", tag, mem, align)
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
	if err != nil {
		return err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, mem, ptr, dstAlign)
	return nil
}

// mapEntryValueOps names the two descriptors a map addresses its entries
// through.
//
// A map's key and value are recorded as operation roots by the layout census
// (`internal/mir/layout_finalize.go`), so a map that reached emission has both.
// A type that arrived here without one would leave the runtime unable to say
// how wide an entry is or how to move one, and there is no degraded mode worth
// having: the module is refused where the reason is still legible rather than
// linked against a descriptor that is not there.
func (fe *funcEmitter) mapEntryValueOps(mapType types.TypeID) (keyOps, valueOps string, err error) {
	key, value, ok := fe.emitter.types.MapInfo(resolveValueType(fe.emitter.types, mapType))
	if !ok {
		return "", "", fmt.Errorf("map construction requires a Map destination, got type#%d", mapType)
	}
	keyOps, err = fe.emitter.mapEntrySideOps(key)
	if err != nil {
		return "", "", err
	}
	valueOps, err = fe.emitter.mapEntrySideOps(value)
	if err != nil {
		return "", "", err
	}
	return keyOps, valueOps, nil
}

// mapKeysDuplication names the body that gives the keys array its OWN copy of
// one key, or "null" when the key's bytes are the whole value.
//
// keys() answers with an INDEPENDENT owning array, which is what makes walking
// it while removing from the map safe -- the shape stdlib/json/stringify.sg
// takes. Copying the bytes of a heap-owning key instead would put the array and
// the map on one block, and the map's teardown would free what the array still
// held.
//
// The recipe is the call site's, not the key descriptor's clone_init, for the
// reason rt_value_duplicate_detached exists: this duplication is an obligation
// the OPERATION takes on, the same way cloning a task handle takes on serving a
// second asker.
func (fe *funcEmitter) mapKeysDuplication(keyType types.TypeID) (string, error) {
	if !fe.emitter.typeOwnsHeap(keyType) {
		return "null", nil
	}
	if !fe.emitter.canDuplicateValue(keyType) {
		// Unreachable while a key may only be a string or an integer, and
		// stated rather than assumed: a key type that owns heap and cannot be
		// duplicated has no honest answer for keys(), and a byte copy is not
		// one.
		return "", fmt.Errorf(
			"map key type#%d owns heap and cannot be duplicated, so keys() cannot answer with an independent array",
			keyType)
	}
	return "@" + fe.emitter.requireCloneGlue(keyType), nil
}

func (e *Emitter) mapEntrySideOps(id types.TypeID) (string, error) {
	if e.valueOpsRegistryHas(id) {
		return "@" + valueOpsSymbol(id), nil
	}
	if resolved := resolveValueType(e.types, id); resolved != id && e.valueOpsRegistryHas(resolved) {
		return "@" + valueOpsSymbol(resolved), nil
	}
	return "", fmt.Errorf("map entry type#%d has no value operations descriptor", id)
}

// emitMapStorageOperand addresses the storage an operand names, which is what
// every map entry point takes now: a key or a value is handed over by address,
// at its own type.
//
// The three shapes an operand arrives in part company here. A BORROW's value IS
// the address, so the slot holding the borrow is one level too far out. A place
// is addressed where it lives, which is also what lets the map move out of it
// instead of copying. A constant has no place at all and gets a slot of its own
// to be handed over from.
func (fe *funcEmitter) emitMapStorageOperand(op *mir.Operand) (string, error) {
	if op == nil {
		return "", fmt.Errorf("missing map entry operand")
	}
	switch {
	case op.Kind == mir.OperandAddrOf || op.Kind == mir.OperandAddrOfMut:
		ptr, _, err := fe.emitPlacePtr(op.Place)
		return ptr, err
	case isRefType(fe.emitter.types, op.Type):
		val, valTy, err := fe.emitOperand(op)
		if err != nil {
			return "", err
		}
		if valTy != handleType {
			return "", fmt.Errorf("map entry borrow is %s, expected a pointer", valTy)
		}
		return val, nil
	case op.Kind == mir.OperandConst:
		val, valTy, err := fe.emitOperand(op)
		if err != nil {
			return "", err
		}
		slot := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca %s, align %d\n", slot, valTy, alignWord)
		fe.emitValueStore(valTy, val, slot, alignWord)
		return slot, nil
	default:
		ptr, _, err := fe.emitOperandStorage(op)
		return ptr, err
	}
}

func (fe *funcEmitter) emitMapHandle(op *mir.Operand) (string, error) {
	if op == nil {
		return "", fmt.Errorf("missing map operand")
	}
	handlePtr, err := fe.emitHandleOperandPtr(op)
	if err != nil {
		return "", err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", tmp, handlePtr)
	return tmp, nil
}

func (fe *funcEmitter) mapKeyTypeFromType(mapType types.TypeID) (types.TypeID, error) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return types.NoTypeID, fmt.Errorf("missing type interner")
	}
	mapType = resolveValueType(fe.emitter.types, mapType)
	key, _, ok := fe.emitter.types.MapInfo(mapType)
	if !ok {
		return types.NoTypeID, fmt.Errorf("map intrinsic requires Map type")
	}
	return key, nil
}

func resolveMapKeyType(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil {
		return id
	}
	for i := 0; i < 32 && id != types.NoTypeID; i++ {
		tt, ok := typesIn.Lookup(id)
		if !ok {
			return id
		}
		switch tt.Kind {
		case types.KindAlias:
			target, ok := typesIn.AliasTarget(id)
			if !ok || target == types.NoTypeID {
				return id
			}
			id = target
		case types.KindOwn, types.KindReference:
			id = tt.Elem
		default:
			return id
		}
	}
	return id
}

func (fe *funcEmitter) mapKeyKindForType(typeID types.TypeID) (int, error) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return 0, fmt.Errorf("missing type interner")
	}
	typeID = resolveMapKeyType(fe.emitter.types, typeID)
	tt, ok := fe.emitter.types.Lookup(typeID)
	if !ok {
		return 0, fmt.Errorf("missing key type")
	}
	switch tt.Kind {
	case types.KindString:
		return mapKeyString, nil
	case types.KindInt:
		if tt.Width == types.WidthAny {
			return mapKeyBigInt, nil
		}
		return mapKeyInt, nil
	case types.KindUint:
		if tt.Width == types.WidthAny {
			return mapKeyBigUint, nil
		}
		return mapKeyUint, nil
	default:
		return 0, fmt.Errorf("unsupported map key type %s", tt.Kind.String())
	}
}
