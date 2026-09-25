package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// returnOriginStdlibTimeDuration recognizes the declaration, not merely the
// spelling or @intrinsic attribute. Its completed field roster is part of the
// certificate: the sole stored value is int64, which cannot contain a
// reference, a storage loan, or a task.
func returnOriginStdlibTimeDuration(fn *returnOriginFunction, id types.TypeID) bool {
	if fn == nil || fn.unit == nil {
		return false
	}
	in := fn.unit.Sema.TypeInterner
	info, found := in.StructInfo(id)
	if !found || info == nil || len(info.TypeArgs) != 0 || len(info.TypeParams) != 0 || len(info.Fields) != 1 {
		return false
	}
	name, _ := in.Strings.Lookup(info.Name)
	fieldName, _ := in.Strings.Lookup(info.Fields[0].Name)
	if name != "Duration" || fieldName != "__opaque" || info.Fields[0].Type != in.Builtins().Int64 {
		return false
	}

	u := returnOriginDeclaringUnit(fn.unit, info)
	if u == nil {
		return false
	}
	file := u.Builder.Files.Get(u.FileID)
	for _, itemID := range file.Items {
		item, ok := u.Builder.Items.Type(itemID)
		if !ok || item.Span != info.Decl {
			continue
		}
		decl := u.Builder.Items.TypeStruct(item)
		ids := u.Symbols.ItemSymbols[itemID]
		if decl == nil || decl.Base.IsValid() || decl.FieldsCount != 1 || len(ids) != 1 || !durationIntrinsicAttribute(u, item) {
			return false
		}
		sym := u.Symbols.Table.Symbols.Get(ids[0])
		return sym != nil && sym.Kind == symbols.SymbolType && u.ModulePath == "stdlib/time" &&
			sym.Decl.Item == itemID && sym.Decl.ASTFile == u.FileID && sym.Decl.SourceFile == info.Decl.File && sym.Type == id
	}
	return false
}

func returnOriginDeclaringUnit(start *returnOriginUnitIndex, info *types.StructInfo) *returnOriginUnitIndex {
	var match *returnOriginUnitIndex
	for _, u := range append([]*returnOriginUnitIndex{start}, start.peers...) {
		file := u.Builder.Files.Get(u.FileID)
		if file == nil || file.Span.File != info.Decl.File {
			continue
		}
		if match != nil && match != u {
			return nil
		}
		match = u
	}
	return match
}

func durationIntrinsicAttribute(u *returnOriginUnitIndex, item *ast.TypeItem) bool {
	attrs := u.Builder.Items.CollectAttrs(item.AttrStart, item.AttrCount)
	if uint64(len(attrs)) != uint64(item.AttrCount) {
		return false
	}
	hasCopy, hasIntrinsic := false, false
	for _, attr := range attrs {
		spec, known := ast.LookupAttrID(u.Builder.StringsInterner, attr.Name)
		if !known || len(attr.Args) != 0 || (spec.Name != "copy" && spec.Name != "intrinsic") {
			return false
		}
		hasIntrinsic = hasIntrinsic || spec.Name == "intrinsic"
		hasCopy = hasCopy || spec.Name == "copy"
	}
	return hasCopy && hasIntrinsic
}
