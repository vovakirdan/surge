package ast

import "surge/internal/source"

type TagItem struct {
	Name                  source.StringID
	NameSpan              source.Span
	Generics              []source.StringID
	GenericCommas         []source.Span
	GenericsTrailingComma bool
	GenericsSpan          source.Span
	TypeParamsStart       TypeParamID
	TypeParamsCount       uint32
	TagKeywordSpan        source.Span
	ParamsSpan            source.Span
	SemicolonSpan         source.Span
	Payload               []TypeID
	PayloadCommas         []source.Span
	PayloadTrailingComma  bool
	AttrStart             AttrID
	AttrCount             uint32
	Visibility            Visibility
	Span                  source.Span
}

func (i *Items) Tag(id ItemID) (*TagItem, bool) {
	item := i.Arena.Get(uint32(id))
	if item == nil || item.Kind != ItemTag || !item.Payload.IsValid() {
		return nil, false
	}
	return i.Tags.Get(uint32(item.Payload)), true
}

func (i *Items) NewTag(
	name source.StringID,
	nameSpan source.Span,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	typeParams []TypeParamSpec,
	tagKwSpan source.Span,
	paramsSpan source.Span,
	semicolonSpan source.Span,
	payload []TypeID,
	payloadCommas []source.Span,
	payloadTrailing bool,
	attrs []Attr,
	visibility Visibility,
	span source.Span,
) ItemID {
	attrStart, attrCount := i.allocateAttrs(attrs)
	typeParamsStart, typeParamsCount := i.allocateTypeParams(typeParams)
	tagPayload := TagItem{
		Name:                  name,
		NameSpan:              nameSpan,
		Generics:              append([]source.StringID(nil), generics...),
		GenericCommas:         append([]source.Span(nil), genericCommas...),
		GenericsTrailingComma: genericsTrailing,
		GenericsSpan:          genericsSpan,
		TypeParamsStart:       typeParamsStart,
		TypeParamsCount:       typeParamsCount,
		TagKeywordSpan:        tagKwSpan,
		ParamsSpan:            paramsSpan,
		SemicolonSpan:         semicolonSpan,
		Payload:               append([]TypeID(nil), payload...),
		PayloadCommas:         append([]source.Span(nil), payloadCommas...),
		PayloadTrailingComma:  payloadTrailing,
		AttrStart:             attrStart,
		AttrCount:             attrCount,
		Visibility:            visibility,
		Span:                  span,
	}
	payloadID := i.Tags.Allocate(tagPayload)
	return i.New(ItemTag, span, PayloadID(payloadID))
}
