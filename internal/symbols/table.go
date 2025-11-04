package symbols

import "surge/internal/source"

// Hints provide optional capacity suggestions for the symbol table arenas.
type Hints struct{ Scopes, Symbols uint }

// Table aggregates symbol-related arenas and shared resources.
type Table struct {
	Scopes   *Scopes
	Symbols  *Symbols
	Strings  *source.Interner
	fileRoot map[source.FileID]ScopeID
}

// NewTable builds a fresh table with optional capacity hints.
// If strings is nil, a fresh interner is allocated.
func NewTable(h Hints, strings *source.Interner) *Table {
	scopeCap := uint32(h.Scopes)
	symCap := uint32(h.Symbols)
	if strings == nil {
		strings = source.NewInterner()
	}
	return &Table{
		Scopes:   NewScopes(scopeCap),
		Symbols:  NewSymbols(symCap),
		Strings:  strings,
		fileRoot: make(map[source.FileID]ScopeID),
	}
}

// FileRoot returns (and creates if needed) a file-level scope for the given file.
func (t *Table) FileRoot(file source.FileID, span source.Span) ScopeID {
	if scope, ok := t.fileRoot[file]; ok {
		return scope
	}
	scope := t.Scopes.New(ScopeFile, NoScopeID, ScopeOwner{
		Kind:       ScopeOwnerFile,
		SourceFile: file,
	}, span)
	t.fileRoot[file] = scope
	return scope
}
