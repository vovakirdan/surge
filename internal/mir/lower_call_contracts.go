package mir

import (
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

// borrowArgContracts builds an all-BORROW contract slice for a call whose
// every position merely lends its argument for the call's duration.
func borrowArgContracts(n int) []ArgContract {
	if n <= 0 {
		return nil
	}
	return make([]ArgContract, n) // ArgContractBorrow is the zero value.
}

// storeArgContracts builds an all-STORE contract slice for a call whose every
// position is kept by the value it builds — a tag constructor.
func storeArgContracts(n int) []ArgContract {
	if n <= 0 {
		return nil
	}
	out := make([]ArgContract, n)
	for i := range out {
		out[i] = ArgContractStore
	}
	return out
}

// byValueArgContract classifies an ordinary by-value argument position from
// the position's own type, mirroring sema's paramTransfersOwnership: the
// callee owns what it was handed unless the type is not droppable at all or is
// a reference-counted scalar, which the caller keeps owning for the whole
// call.
//
// `stores` short-circuits it, and deliberately without consulting the type:
// storing a value past the call is a sink for reference-counted scalars too,
// which is precisely the case the paramTransfersOwnership predicate excludes.
func byValueArgContract(typesIn *types.Interner, semaRes *sema.Result, ty types.TypeID, stores bool) ArgContract {
	if stores {
		return ArgContractStore
	}
	if !ownsHeapFor(typesIn, semaRes, ty) {
		return ArgContractBorrow
	}
	if typesIn != nil && typesIn.IsRefCountedScalar(resolveAlias(typesIn, ty)) {
		return ArgContractBorrow
	}
	return ArgContractTransferOwned
}

func (l *funcLowerer) byValueArgContract(ty types.TypeID, stores bool) ArgContract {
	if l == nil {
		return ArgContractUnresolved
	}
	return byValueArgContract(l.types, l.sema, ty, stores)
}

// storingIntrinsicArgs maps a runtime intrinsic to the argument positions it
// STORES into a container outliving the call. Nothing general can infer this:
// these are named calls with no MIR-visible parameter contract, and the same
// callee borrows one argument (the receiver) while storing another, so the
// table is per-position and audited by hand.
//
// What is deliberately NOT here, and why:
//
//   - rt_array_append_raw_bytes, rt_byte_array_append_range — these copy raw
//     bytes through a pointer/slice. Nothing owning is retained.
//   - rt_scope_register_child — the scope records the child's numeric task id
//     and the executor's own task table owns the task (runtime/native/
//     rt_async_scope.c: scope_add_child stores a uint64). rt_task does carry a
//     handle_refs count, but registration neither increments nor later
//     releases it, so this borrows.
//   - rt_far_task_begin_transfer / rt_far_task_finish_transfer — bookkeeping
//     around a handoff; they read the handles and store nothing.
//   - timeout, await, rt_fs_close, rt_net_close_* — these consume an owned
//     by-value argument, which the ordinary TRANSFER-OWNED rule already
//     covers; they store nothing past the call.
//
// Positions are indices into the MIR call's own argument list, receiver
// included, which is how these calls are lowered.
var storingIntrinsicArgs = map[string][]int{
	// rt_array_push(a: &mut Array<T>, value: T) — the array keeps the value.
	"rt_array_push": {1},
	// rt_map_insert(m: &mut Map<K,V>, key: K, value: V) — the map keeps both.
	"rt_map_insert": {1, 2},
	// __index_set(self: &mut Array<T>, index: int, value: T), and Map's
	// same-named wrapper (self, key, value) — the element written in is kept
	// by the container either way. Map's key position needs no entry: it is an
	// ordinary droppable by-value parameter of a real Surge function, so
	// TRANSFER-OWNED already covers it.
	"__index_set": {2},
	// rt_anchored_channel_send(value) — the value is queued in the channel.
	"rt_anchored_channel_send": {0},
	// __task_create(poll_fn_id, state) — the task owns the resume state box
	// and frees it later (__async_state_free).
	"__task_create": {1},
}

// calleeIsUserFunc reports whether the call resolves to an ordinary Surge
// function rather than an intrinsic or a runtime helper called by name. Such a
// function is owed whatever its OWN parameters say, even when it is spelled
// like one of the tables below: `__index_set` in particular is a magic method
// any type may implement, and a hand-written one may well only read the value
// it is passed.
func (l *funcLowerer) calleeIsUserFunc(symID symbols.SymbolID) bool {
	fn := l.calleeFunc(symID)
	return fn != nil && !fn.IsIntrinsic()
}

// applyStoringIntrinsicContracts overlays the audited table above onto a
// call's contracts. It runs after the ordinary per-parameter classification
// because a stored position is a sink whatever its type is, so it can only
// upgrade a position, never relax one.
func applyStoringIntrinsicContracts(name string, contracts []ArgContract) {
	for _, pos := range storingIntrinsicArgs[baseSymbolName(name)] {
		if pos >= 0 && pos < len(contracts) {
			contracts[pos] = ArgContractStore
		}
	}
}

// applyChannelSendContracts marks the value handed to Channel.send/try_send as
// STORE. It is kept out of the name table because it needs the extra receiver
// check: the caller has already established this is not a user-defined
// function, and the receiver's type is what makes it the channel operation
// rather than some other intrinsic sharing the name, exactly as the
// async-lowering intercept in lowerCallExpr already decides it.
func (l *funcLowerer) applyChannelSendContracts(name string, args []Operand, contracts []ArgContract) {
	switch baseSymbolName(name) {
	case "send", "try_send":
	default:
		return
	}
	if len(args) != 2 || len(contracts) != 2 || !l.isChannelType(args[0].Type) {
		return
	}
	contracts[1] = ArgContractStore
}

// retainStoredRefCountedArgs gives a stored reference-counted scalar its own
// reference, which is what ArgContractStore already says it is owed.
//
// The contract and the operand answer two different questions and were only
// ever connected in prose. A by-value argument is read with the ordinary
// consuming rule, and for a reference-counted scalar that rule produces
// OperandRetain — but only where the read itself is a consuming one. At a call
// it is not: the callee is usually a borrower, and a `float` handed to an
// arithmetic entry point must NOT be retained. So the operand comes out as a
// bare Copy, and for the positions that are sinks the count is never bumped.
//
// The result is a value the container will later release and the caller also
// releases, on a type that is Copy at the surface and therefore leaves the
// source usable and dropped. Measured on `Channel<float>`: with the sender's
// binding still live the program prints the right answer and then segfaults on
// the second release; with the sender a temporary, the channel keeps a freed
// block and the program prints a WRONG ANSWER and exits 0.
//
// This runs after the contract tables because a sink is a sink whatever the
// callee's own parameters say, and it can only ever upgrade Copy to Retain:
// a moved or borrowed position is left exactly as classified.
func retainStoredRefCountedArgs(l *funcLowerer, args []Operand, contracts []ArgContract) {
	if l == nil {
		return
	}
	for i := range args {
		if i >= len(contracts) || contracts[i] != ArgContractStore {
			continue
		}
		if args[i].Kind != OperandCopy || !l.isRefCountedScalar(args[i].Type) {
			continue
		}
		args[i].Kind = OperandRetain
	}
}
