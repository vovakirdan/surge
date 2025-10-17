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
	Kind    ItemKind
	Span    source.Span
	Payload PayloadID
}

type Items struct {
	Arena   *Arena[Item]
	Imports *Arena[ImportItem]
	Fns     *Arena[FnItem]
}

func NewItems(capHint uint) *Items {
	if capHint == 0 {
		capHint = 1 << 8
	}
	return &Items{
		Arena:   NewArena[Item](capHint),
		Imports: NewArena[ImportItem](capHint),
		Fns:     NewArena[FnItem](capHint),
	}
}

func (i *Items) New(kind ItemKind, span source.Span, payloadID PayloadID) ItemID {
	return ItemID(i.Arena.Allocate(Item{
		Kind:    kind,
		Span:    span,
		Payload: payloadID,
	}))
}

func (i *Items) Get(id ItemID) *Item {
	return i.Arena.Get(uint32(id))
}
