package sema

import "surge/internal/types"

// A `&mut X` formal whose referent reads as reference-free can still receive a
// storage loan: X may itself be a canonical array or a cursor (a whole loan sink),
// or store one in a part (a contents loan sink). The shape-only effect rule
// (return_origin_publication.go:104–124) sees neither. These predicates are read
// only where no body answers for the write: a body-less callee, a callback or
// function value, a generic body-less use, a selected operator (by reference or on the
// borrow-free path) or a deferred method.

// loanSink says whether a mutable actual's referent can receive a storage loan. At
// most one outer reference is stripped: a generic callee's effect is the actual's
// own type, which is the owned value when the receiver is borrowed implicitly.
func (b *returnOriginBody) loanSink(effect types.TypeID) bool {
	in := b.function.unit.Sema.TypeInterner
	referent, typ, ok := returnOriginIndexResolve(in, effect)
	if ok && typ.Kind == types.KindReference {
		referent = returnOriginResolveAlias(in, typ.Elem)
	}
	return ok && (b.wholeLoanSink(referent) || b.contentsLoanSink(referent))
}

// wholeLoanSink (E2): the referent itself keeps storage loans.
func (b *returnOriginBody) wholeLoanSink(referent types.TypeID) bool {
	return b.analyzer.loanCarrier(referent)
}

// contentsLoanSink (E1): a part stored in the referent, reached without a reference,
// pointer or function, keeps storage loans. A template parameter part answers
// false; the shape rule already refuses a referent that names one by value.
func (b *returnOriginBody) contentsLoanSink(referent types.TypeID) bool {
	return b.loanWalk(referent, true, func(part types.TypeID) bool { return part != referent && b.analyzer.loanCarrier(part) })
}

// loanSinkEffects says whether some `&mut` formal's actual is a loan sink.
func (b *returnOriginBody) loanSinkEffects(params, effects []types.TypeID) bool {
	in := b.function.unit.Sema.TypeInterner
	for i := range min(len(params), len(effects)) {
		if kind, reference := returnOriginFormalBorrowKind(in, params[i]); reference && kind == BorrowMut && b.loanSink(effects[i]) {
			return true
		}
	}
	return false
}

// operationLoanSink is site 5 on the borrow-free operator path (return_origin_expr.go:
// 186–195, :247–261), where no certificate is computed and SEMA borrows a by-value
// operand into a reference formal (magic_names.go:498–535). Read on the selection's own
// signature: a `&mut` formal that is a loan sink, or whose referent's shape is not proven
// (a generic formal), can receive a loan, and a result that stores one in a part cannot
// announce it, since `@return_source` needs a reference-bearing result.
func (b *returnOriginBody) operationLoanSink(info *types.FnInfo, result types.TypeID) bool {
	if info == nil {
		return true
	}
	in := b.function.unit.Sema.TypeInterner
	for _, param := range info.Params {
		if kind, reference := returnOriginFormalBorrowKind(in, param); reference && kind == BorrowMut &&
			(b.loanSink(param) || returnOriginCallHasUnprovedEffects(in, []types.TypeID{param})) {
			return true
		}
	}
	return b.loanWalk(result, true, b.analyzer.loanCarrier)
}

// Three core byte sinks write only bytes into their destination's own buffer,
// growing or moving it inside its own header, and never store a view header or a
// pointer into a source: native rt_array.c:336–410 (a view destination panics at
// :360), :412–438, :440–474 (a view panics at :454); VM array_utils.go:262–285,
// array_storage.go:468–541, intrinsic_array.go:262–331; LLVM builtins.go:48–50,
// emit_intrinsics_bytes.go. Only the retained core declaration with exactly these
// formals is believed; re-read the runtime before widening.
func returnOriginCertifiedByteSink(fn *returnOriginFunction) bool {
	if !returnOriginCoreIntrinsic(fn, 0, 0) || fn.candidate.HasSelf || fn.candidate.ReceiverType != types.NoTypeID {
		return false
	}
	in := fn.unit.Sema.TypeInterner
	bytes := func(id types.TypeID, mutable bool) bool {
		c, canonical := returnOriginContainer(in, id)
		_, outer, _ := returnOriginIndexResolve(in, id)
		return canonical && c.reference && outer.Mutable == mutable && c.family == in.ArrayNominalType() &&
			returnOriginResolveAlias(in, c.element) == in.Builtins().Uint8
	}
	pointer := func(id types.TypeID) bool {
		_, typ, ok := returnOriginIndexResolve(in, id)
		return ok && typ.Kind == types.KindPointer && returnOriginResolveAlias(in, typ.Elem) == in.Builtins().Uint8
	}
	p, u64 := fn.info.Params, in.Builtins().Uint64
	if fn.info.Result != in.Builtins().Nothing || len(p) == 0 || !bytes(p[0], true) {
		return false
	}
	switch fn.name {
	case "rt_array_append_raw_bytes":
		return len(p) == 3 && pointer(p[1]) && p[2] == u64
	case "rt_byte_array_append_range":
		return len(p) == 4 && bytes(p[1], false) && p[2] == u64 && p[3] == u64
	case "rt_byte_array_drop_prefix":
		return len(p) == 2 && p[1] == u64
	}
	return false
}
