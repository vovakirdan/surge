package driver

import (
	"surge/internal/source"
	"surge/internal/symbols"
)

// moduleSymbolOwner answers which of a record's symbols are the record's own
// declarations, by the file a symbol's span lies in.
type moduleSymbolOwner struct {
	path  string
	files map[source.FileID]struct{}
}

func newModuleSymbolOwner(rec *moduleRecord) moduleSymbolOwner {
	owner := moduleSymbolOwner{files: make(map[source.FileID]struct{}, len(rec.Files))}
	if rec.Meta != nil {
		owner.path = normalizeExportsKey(rec.Meta.Path)
	}
	for _, file := range rec.Files {
		if file != nil {
			owner.files[file.ID] = struct{}{}
		}
	}
	return owner
}

// declares answers whether sym is declared in one of the record's files.
func (o moduleSymbolOwner) declares(sym *symbols.Symbol) bool {
	if sym == nil || o.path == "" {
		return false
	}
	_, ok := o.files[sym.Span.File]
	return ok
}

// declarationPath is the module path a symbol of the record's own table is
// keyed under. A declaration of the record is its module's, even when its
// flags read like a prelude copy's: an `extern` method of an `@intrinsic`
// type is declared Imported and Builtin with no module path of its own.
func (o moduleSymbolOwner) declarationPath(sym *symbols.Symbol, rec *moduleRecord) string {
	modulePath := normalizeExportsKey(sym.ModulePath)
	if modulePath == "" && rec.Meta != nil && (!isPreludeSymbol(sym) || o.declares(sym)) {
		modulePath = normalizeExportsKey(rec.Meta.Path)
	}
	return modulePath
}
