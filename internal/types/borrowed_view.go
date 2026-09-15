package types

// IsBorrowedView reports whether a value of this type borrows the storage of the value it was
// taken from and must not outlive it: the core BytesView, whose runtime keeps a bare pointer
// into its string (rt_string.c rt_string_bytes_view). The mark only makes checks more
// conservative; it certifies nothing by itself.
func (in *Interner) IsBorrowedView(id TypeID) bool {
	if in == nil || id == NoTypeID || in.borrowedViews == nil {
		return false
	}
	_, ok := in.borrowedViews[resolveAliasAndOwn(in, id)]
	return ok
}

// MarkBorrowedViewType records the core view struct from its declaration.
func (in *Interner) MarkBorrowedViewType(id TypeID) {
	if in == nil || id == NoTypeID {
		return
	}
	if info, ok := in.StructInfo(id); !ok || info == nil {
		return
	}
	if in.borrowedViews == nil {
		in.borrowedViews = make(map[TypeID]struct{}, 1)
	}
	in.borrowedViews[id] = struct{}{}
}
