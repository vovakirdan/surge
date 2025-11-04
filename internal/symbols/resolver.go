package symbols

import (
	"surge/internal/diag"
	"surge/internal/source"
)

// ResolverOptions configures resolver construction.
type ResolverOptions struct {
	Reporter diag.Reporter
	Prelude  []PreludeEntry
}

// PreludeEntry describes a symbol injected before source traversal.
type PreludeEntry struct {
	Name  string
	Kind  SymbolKind
	Flags SymbolFlags
}

// Resolver drives scope management and declaration/lookup routines.
type Resolver struct {
	table    *Table
	reporter diag.Reporter
	stack    []ScopeID
}

// NewResolver wires a resolver to an existing scope stack. If root is valid it
// becomes the current scope; otherwise scope-sensitive operations are no-ops.
func NewResolver(table *Table, root ScopeID, opts ResolverOptions) *Resolver {
	r := &Resolver{
		table:    table,
		reporter: opts.Reporter,
		stack:    make([]ScopeID, 0, 8),
	}
	if root.IsValid() {
		r.stack = append(r.stack, root)
	}
	if len(opts.Prelude) > 0 && root.IsValid() {
		r.installPrelude(root, opts.Prelude)
	}
	return r
}

// CurrentScope returns the scope at the top of the stack.
func (r *Resolver) CurrentScope() ScopeID {
	if len(r.stack) == 0 {
		return NoScopeID
	}
	return r.stack[len(r.stack)-1]
}

// Enter creates a child scope, pushes it onto the stack, and returns its ID.
func (r *Resolver) Enter(kind ScopeKind, owner ScopeOwner, span source.Span) ScopeID {
	parent := r.CurrentScope()
	scope := r.table.Scopes.New(kind, parent, owner, span)
	r.stack = append(r.stack, scope)
	return scope
}

// Leave pops the current scope if it matches expected. Mismatches are ignored
// in release builds; future debug builds may assert.
func (r *Resolver) Leave(expected ScopeID) {
	if len(r.stack) == 0 {
		return
	}
	top := r.stack[len(r.stack)-1]
	if expected.IsValid() && top != expected {
		return
	}
	r.stack = r.stack[:len(r.stack)-1]
}

// Declare installs a symbol into the current scope. Returns false if there is
// no active scope.
func (r *Resolver) Declare(name source.StringID, span source.Span, kind SymbolKind, flags SymbolFlags, decl SymbolDecl) (SymbolID, bool) {
	scopeID := r.CurrentScope()
	if !scopeID.IsValid() {
		return NoSymbolID, false
	}
	sym := Symbol{
		Name:  name,
		Kind:  kind,
		Scope: scopeID,
		Span:  span,
		Flags: flags,
		Decl:  decl,
	}
	symbolID := r.table.Symbols.New(sym)
	if scope := r.table.Scopes.Get(scopeID); scope != nil {
		scope.Symbols = append(scope.Symbols, symbolID)
	}
	return symbolID, true
}

// Lookup walks the scope chain searching for a symbol with the given name.
func (r *Resolver) Lookup(name source.StringID) (SymbolID, bool) {
	scopeID := r.CurrentScope()
	for scopeID.IsValid() {
		scope := r.table.Scopes.Get(scopeID)
		if scope == nil {
			break
		}
		for i := len(scope.Symbols) - 1; i >= 0; i-- {
			symbolID := scope.Symbols[i]
			if sym := r.table.Symbols.Get(symbolID); sym != nil && sym.Name == name {
				return symbolID, true
			}
		}
		scopeID = scope.Parent
	}
	return NoSymbolID, false
}

// installPrelude declares prelude entries into scope.
func (r *Resolver) installPrelude(scope ScopeID, entries []PreludeEntry) {
	for _, entry := range entries {
		nameID := r.table.Strings.Intern(entry.Name)
		flags := entry.Flags | SymbolFlagImported
		sym := Symbol{
			Name:  nameID,
			Kind:  entry.Kind,
			Scope: scope,
			Flags: flags,
		}
		id := r.table.Symbols.New(sym)
		if sc := r.table.Scopes.Get(scope); sc != nil {
			sc.Symbols = append(sc.Symbols, id)
		}
	}
}
