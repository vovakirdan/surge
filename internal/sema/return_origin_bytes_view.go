package sema

import "surge/internal/types"

// A bytes view borrows the string it reads: rt_string_bytes_view stores a bare pointer to the
// string's bytes and takes no share (runtime/native/rt_string.c, internal/vm/intrinsic_string.go,
// core/intrinsics.sg BytesView). The IsBorrowedView mark only keeps a view's loan; these
// identities are what certify. A function value reaches neither: its callee is nil.

// returnOriginBytesViewConstructor answers whether fn is the core constructor whose result
// borrows what formal 0 points to.
func returnOriginBytesViewConstructor(fn *returnOriginFunction) bool {
	if fn == nil || fn.name != "rt_string_bytes_view" || !returnOriginCoreIntrinsic(fn, 0, 0) {
		return false
	}
	in := fn.unit.Sema.TypeInterner
	if fn.candidate.HasSelf || fn.candidate.ReceiverType != types.NoTypeID || len(fn.info.Params) != 1 {
		return false
	}
	param, ok := in.Lookup(fn.info.Params[0])
	if !ok || param.Kind != types.KindReference || param.Mutable || param.Elem != in.Builtins().String {
		return false
	}
	return returnOriginBytesViewType(fn, fn.info.Result)
}

// returnOriginBytesViewReader answers whether fn is a core view reader (__len, or either
// __index overload). They load ptr and len through a shared self and cannot replace the loan.
func returnOriginBytesViewReader(fn *returnOriginFunction) bool {
	if fn == nil || (fn.name != "__len" && fn.name != "__index") || !returnOriginCoreIntrinsic(fn, 0, 0) ||
		!fn.candidate.HasSelf || len(fn.info.Params) == 0 {
		return false
	}
	in := fn.unit.Sema.TypeInterner
	self, ok := in.Lookup(fn.info.Params[0])
	if !ok || self.Kind != types.KindReference || self.Mutable || self.Elem != fn.candidate.ReceiverType || !returnOriginBytesViewType(fn, self.Elem) {
		return false
	}
	builtins := in.Builtins()
	if fn.name == "__len" {
		return len(fn.info.Params) == 1 && fn.info.Result == builtins.Uint
	}
	return len(fn.info.Params) == 2 && (fn.info.Params[1] == builtins.Int || fn.info.Params[1] == builtins.Int64) && fn.info.Result == builtins.Uint8
}

// returnOriginBytesViewType answers whether id is the marked core view struct declared beside
// fn: non-generic, with fields owner and ptr (pointers) and len (uint).
func returnOriginBytesViewType(fn *returnOriginFunction, id types.TypeID) bool {
	in := fn.unit.Sema.TypeInterner
	info, found := in.StructInfo(id)
	if !found || info == nil || !in.IsBorrowedView(id) || len(info.TypeArgs) != 0 ||
		info.Decl.File != fn.item.NameSpan.File || len(info.Fields) != 3 {
		return false
	}
	for i, want := range [...]string{"owner", "ptr", "len"} {
		name, _ := fn.unit.Builder.StringsInterner.Lookup(info.Fields[i].Name)
		typ, typed := in.Lookup(info.Fields[i].Type)
		if name != want || !typed || (i < 2 && typ.Kind != types.KindPointer) || (i == 2 && info.Fields[i].Type != in.Builtins().Uint) {
			return false
		}
	}
	return true
}
