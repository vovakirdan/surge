package ast

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/source"
)

type ConstItem struct {
	Name          source.StringID
	Type          TypeID
	Value         ExprID
	Visibility    Visibility
	AttrStart     AttrID
	AttrCount     uint32
	ConstSpan     source.Span
	NameSpan      source.Span
	ColonSpan     source.Span
	EqualsSpan    source.Span
	SemicolonSpan source.Span
	Span          source.Span
}

func (i *Items) Const(id ItemID) (*ConstItem, bool) {
	item := i.Arena.Get(uint32(id))
	if item == nil || item.Kind != ItemConst {
		return nil, false
	}
	return i.Consts.Get(uint32(item.Payload)), true
}

func (i *Items) newConstPayload(
	name source.StringID,
	typeID TypeID,
	value ExprID,
	visibility Visibility,
	attrStart AttrID,
	attrCount uint32,
	constSpan source.Span,
	nameSpan source.Span,
	colonSpan source.Span,
	equalsSpan source.Span,
	semicolonSpan source.Span,
	span source.Span,
) PayloadID {
	payload := i.Consts.Allocate(ConstItem{
		Name:          name,
		Type:          typeID,
		Value:         value,
		Visibility:    visibility,
		AttrStart:     attrStart,
		AttrCount:     attrCount,
		ConstSpan:     constSpan,
		NameSpan:      nameSpan,
		ColonSpan:     colonSpan,
		EqualsSpan:    equalsSpan,
		SemicolonSpan: semicolonSpan,
		Span:          span,
	})
	return PayloadID(payload)
}

func (i *Items) NewConst(
	name source.StringID,
	typeID TypeID,
	value ExprID,
	visibility Visibility,
	attrs []Attr,
	constSpan source.Span,
	nameSpan source.Span,
	colonSpan source.Span,
	equalsSpan source.Span,
	semicolonSpan source.Span,
	span source.Span,
) ItemID {
	var attrStart AttrID
	var attrCount uint32
	attrCount, err := safecast.Conv[uint32](len(attrs))
	if err != nil {
		panic(fmt.Errorf("const attrs count overflow: %w", err))
	}
	if attrCount > 0 {
		for idx, attr := range attrs {
			id := AttrID(i.Attrs.Allocate(attr))
			if idx == 0 {
				attrStart = id
			}
		}
	}
	payloadID := i.newConstPayload(name, typeID, value, visibility, attrStart, attrCount, constSpan, nameSpan, colonSpan, equalsSpan, semicolonSpan, span)
	return i.New(ItemConst, span, payloadID)
}
