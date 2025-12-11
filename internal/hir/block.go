package hir

import (
	"surge/internal/source"
)

// Block represents a sequence of statements in HIR.
type Block struct {
	Stmts []Stmt
	Span  source.Span
}

// IsEmpty returns true if the block has no statements.
func (b *Block) IsEmpty() bool {
	return len(b.Stmts) == 0
}

// LastStmt returns the last statement in the block, or nil if empty.
func (b *Block) LastStmt() *Stmt {
	if len(b.Stmts) == 0 {
		return nil
	}
	return &b.Stmts[len(b.Stmts)-1]
}
