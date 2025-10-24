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
	Arena    *Arena[Item]
	Imports  *Arena[ImportItem]
	Fns      *Arena[FnItem]
	FnParams *Arena[FnParam]
	Attrs    *Arena[Attr]
	Lets     *Arena[LetItem]
}

// NewItems creates and returns an *Items with per-kind arenas initialized to capHint.
// If capHint is 0, NewItems uses a default initial capacity of 1<<8.
// The returned Items contains separate arenas for Item, ImportItem, FnItem, FnParam, Attr, and LetItem.
func NewItems(capHint uint) *Items {
	if capHint == 0 {
		capHint = 1 << 8
	}
	return &Items{
		Arena:    NewArena[Item](capHint),
		Imports:  NewArena[ImportItem](capHint),
		Fns:      NewArena[FnItem](capHint),
		FnParams: NewArena[FnParam](capHint),
		Attrs:    NewArena[Attr](capHint),
		Lets:     NewArena[LetItem](capHint),
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

// CollectAttrs returns a copy of attributes starting at attrStart with count attrCount.
func (i *Items) CollectAttrs(attrStart AttrID, attrCount uint32) []Attr {
	if attrCount == 0 || !attrStart.IsValid() {
		return nil
	}
	result := make([]Attr, 0, attrCount)

	base := uint32(attrStart)
	for offset := range attrCount {
		attr := i.Attrs.Get(base + offset)
		if attr == nil {
			continue
		}
		result = append(result, *attr)
	}
	return result
}
