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
	Payload PayloadID
}

type Items struct {
	Arena *Arena[Item]
	Imports *Arena[ImportItem]
}

func NewItems(capHint uint) *Items {
	return &Items{
		Arena: NewArena[Item](capHint),
		Imports: NewArena[ImportItem](capHint),
	}
}

func (i *Items) New(kind ItemKind, span source.Span, payloadID PayloadID) ItemID {
	return ItemID(i.Arena.Allocate(Item{
		Kind: kind,
		Span: span,
		Payload: payloadID,
	}))
}

func (i *Items) NewImport(span source.Span, module []string, alias string, one *ImportOne, group []ImportPair) ItemID {
	payload := i.Imports.Allocate(ImportItem{
		Module: append([]string(nil), module...), // копия, чтобы не держать чужой слайс
		Alias:  alias,
		One:    one,
		Group:  append([]ImportPair(nil), group...),
	})
	return i.New(ItemImport, span, PayloadID(payload))
}

func (i *Items) Import(id ItemID) (*ImportItem, bool) {
	item := i.Arena.Get(uint32(id))
	if item == nil || item.Kind != ItemImport {
		return nil, false
	}
	return i.Imports.Get(uint32(item.Payload)), true
}

func (i *Items) Get(id ItemID) *Item {
	return i.Arena.Get(uint32(id))
}
