package ast

import (
	"surge/internal/source"
)

type ItemKind uint8

const (
	ItemFn ItemKind = iota
	ItemLet
	ItemType
	ItemNewtype
	ItemAlias
	ItemLiteral
	ItemTag
	ItemExtern
	ItemPragma
	ItemImport
	ItemMacro
)

type Item struct {
	Kind ItemKind
	Span source.Span
	Name string // временно, позже interned id
	Body StmtID
}

type Items struct {
	Arena *Arena[Item]
}

func NewItems(capHint uint) *Items {
	return &Items{
		Arena: NewArena[Item](capHint),
	}
}

func (i *Items) New(kind ItemKind, span source.Span, name string) ItemID {
	return ItemID(i.Arena.Allocate(Item{
		Kind: kind,
		Span: span,
		Name: name,
	}))
}

func (i *Items) Get(id ItemID) *Item {
	return i.Arena.Get(uint32(id))
}
