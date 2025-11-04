package symbols

// ScopeID identifies a scope in the resolver arena.
type ScopeID uint32

const (
	// NoScopeID marks the absence of a scope reference.
	NoScopeID ScopeID = 0
)

// IsValid reports whether the scope ID refers to an allocated scope.
func (id ScopeID) IsValid() bool { return id != NoScopeID }

// SymbolID identifies a symbol inside the resolver arena.
type SymbolID uint32

const (
	// NoSymbolID marks the absence of a symbol reference.
	NoSymbolID SymbolID = 0
)

// IsValid reports whether the symbol ID refers to an allocated symbol.
func (id SymbolID) IsValid() bool { return id != NoSymbolID }
