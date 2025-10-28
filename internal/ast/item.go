package ast

import (
	"surge/internal/source"
)

type ItemKind uint8

const (
	ItemFn ItemKind = iota
	ItemLet
	ItemType
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
	Arena            *Arena[Item]
	Imports          *Arena[ImportItem]
	Fns              *Arena[FnItem]
	FnParams         *Arena[FnParam]
	Attrs            *Arena[Attr]
	Lets             *Arena[LetItem]
	Types            *Arena[TypeItem]
	TypeAliases      *Arena[TypeAliasDecl]
	TypeStructs      *Arena[TypeStructDecl]
	TypeFields       *Arena[TypeStructField]
	TypeUnions       *Arena[TypeUnionDecl]
	TypeUnionMembers *Arena[TypeUnionMember]
}

// NewItems creates and returns an *Items with per-kind arenas initialized to capHint.
// If capHint is 0, NewItems uses a default initial capacity of 1<<8.
// The returned Items contains separate arenas for Item, ImportItem, FnItem, FnParam, Attr, and LetItem.
func NewItems(capHint uint) *Items {
	if capHint == 0 {
		capHint = 1 << 8
	}
	return &Items{
		Arena:            NewArena[Item](capHint),
		Imports:          NewArena[ImportItem](capHint),
		Fns:              NewArena[FnItem](capHint),
		FnParams:         NewArena[FnParam](capHint),
		Attrs:            NewArena[Attr](capHint),
		Lets:             NewArena[LetItem](capHint),
		Types:            NewArena[TypeItem](capHint),
		TypeAliases:      NewArena[TypeAliasDecl](capHint),
		TypeStructs:      NewArena[TypeStructDecl](capHint),
		TypeFields:       NewArena[TypeStructField](capHint),
		TypeUnions:       NewArena[TypeUnionDecl](capHint),
		TypeUnionMembers: NewArena[TypeUnionMember](capHint),
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

func (i *Items) Type(itemID ItemID) (*TypeItem, bool) {
	item := i.Get(itemID)
	if item == nil || item.Kind != ItemType || !item.Payload.IsValid() {
		return nil, false
	}
	return i.Types.Get(uint32(item.Payload)), true
}

func (i *Items) TypeAlias(item *TypeItem) *TypeAliasDecl {
	if item == nil || item.Kind != TypeDeclAlias || !item.Payload.IsValid() {
		return nil
	}
	return i.TypeAliases.Get(uint32(item.Payload))
}

func (i *Items) TypeStruct(item *TypeItem) *TypeStructDecl {
	if item == nil || item.Kind != TypeDeclStruct || !item.Payload.IsValid() {
		return nil
	}
	return i.TypeStructs.Get(uint32(item.Payload))
}

func (i *Items) TypeUnion(item *TypeItem) *TypeUnionDecl {
	if item == nil || item.Kind != TypeDeclUnion || !item.Payload.IsValid() {
		return nil
	}
	return i.TypeUnions.Get(uint32(item.Payload))
}

func (i *Items) StructField(id TypeFieldID) *TypeStructField {
	if !id.IsValid() {
		return nil
	}
	return i.TypeFields.Get(uint32(id))
}

func (i *Items) UnionMember(id TypeUnionMemberID) *TypeUnionMember {
	if !id.IsValid() {
		return nil
	}
	return i.TypeUnionMembers.Get(uint32(id))
}
