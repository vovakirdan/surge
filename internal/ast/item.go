package ast

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/source"
)

type ItemKind uint8

const (
	ItemFn ItemKind = iota
	ItemLet
	ItemConst
	ItemType
	ItemTag
	ItemExtern
	ItemPragma
	ItemImport
	ItemMacro
	ItemContract
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
	Consts           *Arena[ConstItem]
	TypeParams       *Arena[TypeParam]
	TypeParamBounds  *Arena[TypeParamBound]
	Contracts        *Arena[ContractDecl]
	ContractItems    *Arena[ContractItem]
	ContractFields   *Arena[ContractFieldReq]
	ContractFns      *Arena[ContractFnReq]
	Types            *Arena[TypeItem]
	TypeAliases      *Arena[TypeAliasDecl]
	TypeStructs      *Arena[TypeStructDecl]
	TypeFields       *Arena[TypeStructField]
	TypeUnions       *Arena[TypeUnionDecl]
	TypeUnionMembers *Arena[TypeUnionMember]
	Externs          *Arena[ExternBlock]
	ExternMembers    *Arena[ExternMember]
	ExternFields     *Arena[ExternField]
	Tags             *Arena[TagItem]
}

// NewItems creates and returns an *Items with per-kind arenas initialized to capHint.
// If capHint is 0, NewItems uses a default initial capacity of 1<<8.
// The returned Items contains separate arenas for Item payloads including imports, fn/contract data,
// attributes, lets/consts, type parameters, types, externs, and tags.
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
		Consts:           NewArena[ConstItem](capHint),
		TypeParams:       NewArena[TypeParam](capHint),
		TypeParamBounds:  NewArena[TypeParamBound](capHint),
		Contracts:        NewArena[ContractDecl](capHint),
		ContractItems:    NewArena[ContractItem](capHint),
		ContractFields:   NewArena[ContractFieldReq](capHint),
		ContractFns:      NewArena[ContractFnReq](capHint),
		Types:            NewArena[TypeItem](capHint),
		TypeAliases:      NewArena[TypeAliasDecl](capHint),
		TypeStructs:      NewArena[TypeStructDecl](capHint),
		TypeFields:       NewArena[TypeStructField](capHint),
		TypeUnions:       NewArena[TypeUnionDecl](capHint),
		TypeUnionMembers: NewArena[TypeUnionMember](capHint),
		Externs:          NewArena[ExternBlock](capHint),
		ExternMembers:    NewArena[ExternMember](capHint),
		ExternFields:     NewArena[ExternField](capHint),
		Tags:             NewArena[TagItem](capHint),
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

func (i *Items) allocateAttrs(attrs []Attr) (attr AttrID, attrCount uint32) {
	if len(attrs) == 0 {
		return NoAttrID, 0
	}
	var start AttrID
	for idx, attr := range attrs {
		id := AttrID(i.Attrs.Allocate(attr))
		if idx == 0 {
			start = id
		}
	}
	count, err := safecast.Conv[uint32](len(attrs))
	if err != nil {
		panic(fmt.Errorf("attrs count overflow: %w", err))
	}
	return start, count
}

func (i *Items) NewTypeAlias(
	name source.StringID,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	typeParams []TypeParamSpec,
	typeKwSpan source.Span,
	assignSpan source.Span,
	semicolonSpan source.Span,
	attrs []Attr,
	visibility Visibility,
	target TypeID,
	span source.Span,
) ItemID {
	attrStart, attrCount := i.allocateAttrs(attrs)
	typeParamsStart, typeParamsCount := i.allocateTypeParams(typeParams)
	payload := i.TypeAliases.Allocate(TypeAliasDecl{Target: target})
	typeItem := TypeItem{
		Name:                  name,
		Generics:              append([]source.StringID(nil), generics...),
		GenericCommas:         append([]source.Span(nil), genericCommas...),
		GenericsTrailingComma: genericsTrailing,
		GenericsSpan:          genericsSpan,
		TypeParamsStart:       typeParamsStart,
		TypeParamsCount:       typeParamsCount,
		TypeKeywordSpan:       typeKwSpan,
		AssignSpan:            assignSpan,
		SemicolonSpan:         semicolonSpan,
		AttrStart:             attrStart,
		AttrCount:             attrCount,
		Kind:                  TypeDeclAlias,
		Payload:               PayloadID(payload),
		Visibility:            visibility,
		Span:                  span,
	}
	payloadID := PayloadID(i.Types.Allocate(typeItem))
	return i.New(ItemType, span, payloadID)
}

func (i *Items) NewTypeStruct(
	name source.StringID,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	typeParams []TypeParamSpec,
	typeKwSpan source.Span,
	assignSpan source.Span,
	semicolonSpan source.Span,
	attrs []Attr,
	visibility Visibility,
	base TypeID,
	fields []TypeStructFieldSpec,
	fieldCommas []source.Span,
	hasTrailing bool,
	bodySpan source.Span,
	span source.Span,
) ItemID {
	attrStart, attrCount := i.allocateAttrs(attrs)
	typeParamsStart, typeParamsCount := i.allocateTypeParams(typeParams)
	var fieldsStart TypeFieldID
	fieldCount, err := safecast.Conv[uint32](len(fields))
	if err != nil {
		panic(fmt.Errorf("fields count overflow: %w", err))
	}
	if fieldCount > 0 {
		for idx, spec := range fields {
			fieldAttrStart, fieldAttrCount := i.allocateAttrs(spec.Attrs)
			fieldID := TypeFieldID(i.TypeFields.Allocate(TypeStructField{
				Name:      spec.Name,
				Type:      spec.Type,
				Default:   spec.Default,
				AttrStart: fieldAttrStart,
				AttrCount: fieldAttrCount,
				Span:      spec.Span,
			}))
			if idx == 0 {
				fieldsStart = fieldID
			}
		}
	}
	structPayload := i.TypeStructs.Allocate(TypeStructDecl{
		Base:        base,
		FieldsStart: fieldsStart,
		FieldsCount: fieldCount,
		FieldCommas: append([]source.Span(nil), fieldCommas...),
		HasTrailing: hasTrailing,
		BodySpan:    bodySpan,
	})
	typeItem := TypeItem{
		Name:                  name,
		Generics:              append([]source.StringID(nil), generics...),
		GenericCommas:         append([]source.Span(nil), genericCommas...),
		GenericsTrailingComma: genericsTrailing,
		GenericsSpan:          genericsSpan,
		TypeParamsStart:       typeParamsStart,
		TypeParamsCount:       typeParamsCount,
		TypeKeywordSpan:       typeKwSpan,
		AssignSpan:            assignSpan,
		SemicolonSpan:         semicolonSpan,
		AttrStart:             attrStart,
		AttrCount:             attrCount,
		Kind:                  TypeDeclStruct,
		Payload:               PayloadID(structPayload),
		Visibility:            visibility,
		Span:                  span,
	}
	payloadID := PayloadID(i.Types.Allocate(typeItem))
	return i.New(ItemType, span, payloadID)
}

func (i *Items) NewTypeUnion(
	name source.StringID,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	typeParams []TypeParamSpec,
	typeKwSpan source.Span,
	assignSpan source.Span,
	semicolonSpan source.Span,
	attrs []Attr,
	visibility Visibility,
	members []TypeUnionMemberSpec,
	bodySpan source.Span,
	span source.Span,
) ItemID {
	attrStart, attrCount := i.allocateAttrs(attrs)
	typeParamsStart, typeParamsCount := i.allocateTypeParams(typeParams)
	var membersStart TypeUnionMemberID
	memberCount, err := safecast.Conv[uint32](len(members))
	if err != nil {
		panic(fmt.Errorf("members count overflow: %w", err))
	}
	if memberCount > 0 {
		for idx, spec := range members {
			memberID := TypeUnionMemberID(i.TypeUnionMembers.Allocate(TypeUnionMember{
				Kind:        spec.Kind,
				Type:        spec.Type,
				TagName:     spec.TagName,
				TagArgs:     append([]TypeID(nil), spec.TagArgs...),
				ArgCommas:   append([]source.Span(nil), spec.ArgCommas...),
				HasTrailing: spec.HasTrailing,
				ArgsSpan:    spec.ArgsSpan,
				Span:        spec.Span,
			}))
			if idx == 0 {
				membersStart = memberID
			}
		}
	}
	unionPayload := i.TypeUnions.Allocate(TypeUnionDecl{
		MembersStart: membersStart,
		MembersCount: memberCount,
		BodySpan:     bodySpan,
	})
	typeItem := TypeItem{
		Name:                  name,
		Generics:              append([]source.StringID(nil), generics...),
		GenericCommas:         append([]source.Span(nil), genericCommas...),
		GenericsTrailingComma: genericsTrailing,
		GenericsSpan:          genericsSpan,
		TypeParamsStart:       typeParamsStart,
		TypeParamsCount:       typeParamsCount,
		TypeKeywordSpan:       typeKwSpan,
		AssignSpan:            assignSpan,
		SemicolonSpan:         semicolonSpan,
		AttrStart:             attrStart,
		AttrCount:             attrCount,
		Kind:                  TypeDeclUnion,
		Payload:               PayloadID(unionPayload),
		Visibility:            visibility,
		Span:                  span,
	}
	payloadID := PayloadID(i.Types.Allocate(typeItem))
	return i.New(ItemType, span, payloadID)
}
