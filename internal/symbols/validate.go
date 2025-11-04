package symbols

import (
	"errors"
	"fmt"
)

// Validate walks internal arenas checking structural invariants. Returns nil if
// everything is consistent; otherwise aggregates all detected issues.
func (t *Table) Validate() error {
	var errs []error

	// Check scopes.
	for idx := 1; idx < len(t.Scopes.data); idx++ {
		scopeID := ScopeID(idx)
		scope := t.Scopes.data[idx]
		if scope.Kind == ScopeInvalid {
			errs = append(errs, fmt.Errorf("scope %d has invalid kind", scopeID))
		}
		if scope.Parent.IsValid() {
			if int(scope.Parent) >= len(t.Scopes.data) || scope.Parent == scopeID {
				errs = append(errs, fmt.Errorf("scope %d has invalid parent %d", scopeID, scope.Parent))
				continue
			}
			parent := t.Scopes.data[scope.Parent]
			found := false
			for _, child := range parent.Children {
				if child == scopeID {
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, fmt.Errorf("scope %d parent %d missing backlink", scopeID, scope.Parent))
			}
		}
	}

	// Check child backlink consistency.
	for idx := 1; idx < len(t.Scopes.data); idx++ {
		scopeID := ScopeID(idx)
		scope := t.Scopes.data[idx]
		for _, child := range scope.Children {
			if int(child) >= len(t.Scopes.data) || child == scopeID {
				errs = append(errs, fmt.Errorf("scope %d has invalid child %d", scopeID, child))
				continue
			}
			if t.Scopes.data[child].Parent != scopeID {
				errs = append(errs, fmt.Errorf("scope %d child %d missing parent backlink", scopeID, child))
			}
		}
	}

	// Check symbols.
	for idx := 1; idx < len(t.Symbols.data); idx++ {
		symbolID := SymbolID(idx)
		symbol := t.Symbols.data[idx]
		if !symbol.Scope.IsValid() || int(symbol.Scope) >= len(t.Scopes.data) {
			errs = append(errs, fmt.Errorf("symbol %d has invalid scope %d", symbolID, symbol.Scope))
			continue
		}
		scope := t.Scopes.data[symbol.Scope]
		found := false
		for _, id := range scope.Symbols {
			if id == symbolID {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Errorf("symbol %d is missing from scope %d list", symbolID, symbol.Scope))
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
