package sema

import (
	"fmt"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type returnOriginShape uint8

const (
	returnOriginRefFree returnOriginShape = iota
	returnOriginCarriesRef
	returnOriginShapeUnknown
)

// This asks about borrowed content, not reference-counted ownership. In
// particular, a string parameter owns its storage but carries no input borrow.
func returnOriginTypeShape(interner *types.Interner, id types.TypeID, seen map[types.TypeID]bool) returnOriginShape {
	if id == types.NoTypeID || interner == nil {
		return returnOriginShapeUnknown
	}
	if seen[id] {
		return returnOriginRefFree
	}
	typ, ok := interner.Lookup(id)
	if !ok {
		return returnOriginShapeUnknown
	}
	if seen == nil {
		seen = make(map[types.TypeID]bool)
	}
	seen[id] = true
	switch typ.Kind {
	case types.KindReference:
		return returnOriginCarriesRef
	case types.KindUnit, types.KindNothing, types.KindBool, types.KindConst,
		types.KindString, types.KindInt, types.KindUint, types.KindFloat, types.KindEnum, types.KindPointer:
		return returnOriginRefFree
	case types.KindOwn, types.KindFar, types.KindArray:
		return returnOriginTypeShape(interner, typ.Elem, seen)
	case types.KindAlias:
		if target, found := interner.AliasTarget(id); found {
			return returnOriginTypeShape(interner, target, seen)
		}
	case types.KindUnion:
		if info, found := interner.UnionInfo(id); found && info != nil {
			shape := returnOriginRefFree
			for _, member := range info.Members {
				if member.Type != types.NoTypeID {
					shape = max(shape, returnOriginTypeShape(interner, member.Type, seen))
				}
				for _, arg := range member.TagArgs {
					shape = max(shape, returnOriginTypeShape(interner, arg, seen))
				}
			}
			return shape
		}
	case types.KindStruct:
		if info, found := interner.StructInfo(id); found && info != nil {
			if shape, logical := returnOriginNominalShape(interner, id, info, seen); logical {
				return shape
			}
			shape := returnOriginRefFree
			for _, field := range info.Fields {
				shape = max(shape, returnOriginTypeShape(interner, field.Type, seen))
			}
			return shape
		}
	case types.KindTuple:
		if info, found := interner.TupleInfo(id); found && info != nil {
			shape := returnOriginRefFree
			for _, elem := range info.Elems {
				shape = max(shape, returnOriginTypeShape(interner, elem, seen))
			}
			return shape
		}
	}
	return returnOriginShapeUnknown
}

func returnOriginNominalShape(interner *types.Interner, id types.TypeID, info *types.StructInfo, seen map[types.TypeID]bool) (returnOriginShape, bool) {
	runtimeHandle := interner.IsRuntimeHandleType(id)
	for _, base := range [...]types.TypeID{interner.ArrayNominalType(), interner.ArrayFixedNominalType(), interner.MapNominalType()} {
		original, ok := interner.StructInfo(base)
		if !ok || original == nil || info.Name != original.Name || info.Decl != original.Decl {
			continue
		}
		if len(info.TypeArgs) == 0 {
			return returnOriginShapeUnknown, true
		}
		if base == interner.ArrayFixedNominalType() {
			// The element exists even when the length is still a generic N.
			return returnOriginTypeShape(interner, info.TypeArgs[0], seen), true
		}
		arity := 1
		if base == interner.MapNominalType() {
			arity = 2
		}
		if len(info.TypeArgs) != arity {
			return returnOriginShapeUnknown, true
		}
		runtimeHandle = true
		break
	}
	if !runtimeHandle {
		return returnOriginRefFree, false
	}
	payloads, ok := interner.RuntimeHandlePayloads(id)
	if !ok || len(payloads) == 0 && len(info.TypeParams) != 0 {
		return returnOriginShapeUnknown, true
	}
	shape := returnOriginRefFree
	for _, payload := range payloads {
		shape = max(shape, returnOriginTypeShape(interner, payload, seen))
	}
	return shape, true
}

func (b *returnOriginBody) pending(span source.Span, reason string) {
	if !b.analyzer.collect {
		return
	}
	pending := ReturnOriginPending{SourceKey: b.function.unit.SourceKey, Span: span, Reason: reason}
	for _, old := range b.analyzer.report.Pending {
		if old == pending {
			return
		}
	}
	b.analyzer.report.Pending = append(b.analyzer.report.Pending, pending)
}

func (b *returnOriginBody) checkExpired(value returnOriginValue, span source.Span) {
	if !b.analyzer.collect || !value.normal {
		return
	}
	for _, root := range value.roots {
		if root.kind == returnOriginUnknown || root.kind == returnOriginCapture {
			b.pending(span, "outgoing reference has unresolved or captured provenance")
			continue
		}
		if root.kind != returnOriginLocal || !root.expired {
			continue
		}
		owner := b.function.unit.Symbols.Table.Symbols.Get(root.binding)
		if owner == nil {
			b.pending(span, "expired reference has no owning source declaration")
			continue
		}
		name, _ := b.function.unit.Symbols.Table.Strings.Lookup(owner.Name)
		message := fmt.Sprintf("borrow of '%s' outlives its owner when this scope exits", name)
		duplicate := false
		for _, old := range b.analyzer.report.Diagnostics {
			if old.Primary == span && old.Message == message {
				duplicate = true
				break
			}
		}
		if !duplicate {
			b.analyzer.report.Diagnostics = append(b.analyzer.report.Diagnostics, diag.Diagnostic{
				Severity: diag.SevError, Code: diag.SemaBorrowEscapesReturn, Primary: span, Message: message,
				Notes: []diag.Note{{Span: owner.Span, Msg: fmt.Sprintf("'%s' owns storage that ends in this scope", name)}},
				Help:  []diag.Note{{Span: span, Msg: "keep the owner outside this scope, or return an owned value instead"}},
			})
		}
	}
}

func (b *returnOriginBody) closeOutcome(outcome returnOriginOutcome, scope symbols.ScopeID, span source.Span) returnOriginOutcome {
	if !outcome.env.reachable {
		return outcome
	}
	closed := outcome.env.leaveScope(scope, outcome.value, b.within)
	b.checkExpired(closed.value, span)
	for _, binding := range closed.env.bindings {
		b.checkExpired(binding.value, span)
	}
	// Cells and container backings are the caller's storage, so a local loan
	// stored in one is an escape even when this scope's result is `nothing`.
	for _, cell := range closed.env.cells {
		b.checkExpired(cell, span)
	}
	for _, backing := range closed.env.backings {
		b.checkExpired(backing, span)
	}
	return returnOriginOutcome{env: closed.env, value: closed.value}
}

func (b *returnOriginBody) closeFlow(flow returnOriginFlow, scope symbols.ScopeID, span source.Span) returnOriginFlow {
	closed := b.closeOutcome(returnOriginOutcome{env: flow.normal, value: returnOriginValueOf()}, scope, span)
	out := returnOriginFlow{normal: closed.env, exits: make(map[returnOriginExit]returnOriginOutcome, len(flow.exits))}
	for key, outcome := range flow.exits {
		out.exits[key] = b.closeOutcome(outcome, scope, key.site)
	}
	return out
}
