package symbols

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/source"
)

// Scopes stores all allocated scopes in a compact slice-based arena.
type Scopes struct {
	data []Scope
}

// NewScopes creates an arena with optional capacity hint.
func NewScopes(capacity uint32) *Scopes {
	if capacity == 0 {
		capacity = 32
	}
	s := &Scopes{
		data: make([]Scope, 1, capacity+1), // index 0 reserved for NoScopeID
	}
	return s
}

// New allocates a new scope and returns its ID.

func (s *Scopes) New(kind ScopeKind, parent ScopeID, owner ScopeOwner, span source.Span) ScopeID {
	value, err := safecast.Conv[uint32](len(s.data))
	if err != nil {
		panic(fmt.Errorf("scopes arena overflow: %w", err))
	}
	id := ScopeID(value)
	s.data = append(s.data, Scope{
		Kind:      kind,
		Parent:    parent,
		Owner:     owner,
		Span:      span,
		NameIndex: make(map[source.StringID][]SymbolID),
	})
	if parent.IsValid() {
		if parentScope := s.Get(parent); parentScope != nil {
			parentScope.Children = append(parentScope.Children, id)
		}
	}
	return id
}

// Get returns the scope pointer or nil if ID is invalid.
func (s *Scopes) Get(id ScopeID) *Scope {
	if !id.IsValid() || int(id) >= len(s.data) {
		return nil
	}
	return &s.data[id]
}

// Len reports total number of scopes excluding the sentinel.
func (s *Scopes) Len() int { return len(s.data) - 1 }

// Data exposes the underlying slice without the sentinel.
func (s *Scopes) Data() []Scope {
	if len(s.data) <= 1 {
		return nil
	}
	return s.data[1:]
}

// Symbols stores declared symbols in a compact arena.
type Symbols struct {
	data []Symbol
}

// NewSymbols creates a symbol arena with optional capacity hint.
func NewSymbols(capacity uint32) *Symbols {
	if capacity == 0 {
		capacity = 64
	}
	return &Symbols{
		data: make([]Symbol, 1, capacity+1), // index 0 reserved for NoSymbolID
	}
}

// New allocates a symbol in the arena and returns its ID.
func (s *Symbols) New(sym *Symbol) SymbolID {
	if sym == nil {
		panic("symbols.New: nil symbol")
	}
	value, err := safecast.Conv[uint32](len(s.data))
	if err != nil {
		panic(fmt.Errorf("symbols arena overflow: %w", err))
	}
	id := SymbolID(value)
	s.data = append(s.data, *sym)
	return id
}

// Get returns a symbol pointer or nil for invalid ID.
func (s *Symbols) Get(id SymbolID) *Symbol {
	if !id.IsValid() || int(id) >= len(s.data) {
		return nil
	}
	return &s.data[id]
}

// Len reports number of stored symbols excluding sentinel.
func (s *Symbols) Len() int { return len(s.data) - 1 }

// Data exposes the arena storage without the sentinel.
func (s *Symbols) Data() []Symbol {
	if len(s.data) <= 1 {
		return nil
	}
	return s.data[1:]
}
