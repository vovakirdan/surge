package ast

import (
	"surge/internal/source"
)

type Hints struct{ Files, Items, Stmts, Exprs uint }

type Builder struct {
	Files *Files
	Items *Items
	Stmts *Stmts
	Exprs *Exprs
}

func NewBuilder(hints Hints) *Builder {
	if hints.Files == 0 {
		hints.Files = 1 << 6 // просто понты; 64
	}
	if hints.Items == 0 {
		hints.Items = 1 << 7
	}
	if hints.Stmts == 0 {
		hints.Stmts = 1 << 8
	}
	if hints.Exprs == 0 {
		hints.Exprs = 1 << 8
	}
	return &Builder{
		Files: NewFiles(hints.Files),
		Items: NewItems(hints.Items),
		Stmts: NewStmts(hints.Stmts),
		Exprs: NewExprs(hints.Exprs),
	}
}

func (b *Builder) NewFile(sp source.Span) FileID {
	return b.Files.New(sp)
}

func (b *Builder) NewItem(kind ItemKind, sp source.Span, name string) ItemID {
	return b.Items.New(kind, sp, name)
}

func (b *Builder) NewStmt(kind StmtKind, sp source.Span) StmtID {
	return b.Stmts.New(kind, sp)
}

func (b *Builder) PushItem(file FileID, item ItemID) {
	b.Files.Get(file).Items = append(b.Files.Get(file).Items, item)
}
