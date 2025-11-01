package ast

import "surge/internal/source"

type TagItem struct {
	Name                  source.StringID
	Generics              []source.StringID
	GenericCommas         []source.Span
	GenericsTrailingComma bool
	GenericsSpan          source.Span
	Payload               []TypeID
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
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	payload []TypeID,
	attrs []Attr,
	visibility Visibility,
	span source.Span,
) ItemID {
	attrStart, attrCount := i.allocateAttrs(attrs)
	tagPayload := TagItem{
		Name:                  name,
		Generics:              append([]source.StringID(nil), generics...),
		GenericCommas:         append([]source.Span(nil), genericCommas...),
		GenericsTrailingComma: genericsTrailing,
		GenericsSpan:          genericsSpan,
		Payload:               append([]TypeID(nil), payload...),
		AttrStart:             attrStart,
		AttrCount:             attrCount,
		Visibility:            visibility,
		Span:                  span,
	}
	payloadID := i.Tags.Allocate(tagPayload)
	return i.New(ItemTag, span, PayloadID(payloadID))
}
