package mono

import (
	"surge/internal/hir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Subst manages type parameter substitution during monomorphization.
type Subst struct {
	Types     *types.Interner
	OwnerSym  symbols.SymbolID
	OwnerSyms []symbols.SymbolID
	TypeArgs  []types.TypeID
	NameArgs  map[source.StringID]types.TypeID

	cache map[types.TypeID]types.TypeID
}

// ApplyFunc performs type substitution on a function.
func (s *Subst) ApplyFunc(fn *hir.Func) error {
	if s == nil || fn == nil {
		return nil
	}
	for i := range fn.Params {
		fn.Params[i].Type = s.Type(fn.Params[i].Type)
		fn.Params[i].Ownership = inferOwnership(s.Types, fn.Params[i].Type)
		if fn.Params[i].Default != nil {
			if err := s.ApplyExpr(fn.Params[i].Default); err != nil {
				return err
			}
		}
	}
	fn.Result = s.Type(fn.Result)
	if fn.Body != nil {
		if err := s.ApplyBlock(fn.Body); err != nil {
			return err
		}
	}
	return nil
}

// ApplyBlock performs type substitution on a block of statements.
func (s *Subst) ApplyBlock(b *hir.Block) error {
	if s == nil || b == nil {
		return nil
	}
	for i := range b.Stmts {
		if err := s.ApplyStmt(&b.Stmts[i]); err != nil {
			return err
		}
	}
	return nil
}
