package ast

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/source"
)

type LetItem struct {
	Name       source.StringID
	Type       TypeID // NoTypeID if type is inferred
	Value      ExprID // NoExprID if no initialization
	IsMut      bool   // mut modifier
	Visibility Visibility
	AttrStart  AttrID
	AttrCount  uint32
	Span       source.Span
}

func (i *Items) Let(id ItemID) (*LetItem, bool) {
	item := i.Arena.Get(uint32(id))
	if item == nil || item.Kind != ItemLet {
		return nil, false
	}
	return i.Lets.Get(uint32(item.Payload)), true
}

func (i *Items) newLetPayload(
	name source.StringID,
	typeID TypeID,
	value ExprID,
	isMut bool,
	visibility Visibility,
	attrStart AttrID,
	attrCount uint32,
	span source.Span,
) PayloadID {
	payload := i.Lets.Allocate(LetItem{
		Name:       name,
		Type:       typeID,
		Value:      value,
		IsMut:      isMut,
		Visibility: visibility,
		AttrStart:  attrStart,
		AttrCount:  attrCount,
		Span:       span,
	})
	return PayloadID(payload)
}

func (i *Items) NewLet(
	name source.StringID,
	typeID TypeID,
	value ExprID,
	isMut bool,
	visibility Visibility,
	attrs []Attr,
	span source.Span,
) ItemID {
	var attrStart AttrID
	var attrCount uint32
	attrCount, err := safecast.Conv[uint32](len(attrs))
	if err != nil {
		panic(fmt.Errorf("let attrs count overflow: %w", err))
	}
	if attrCount > 0 {
		for idx, attr := range attrs {
			id := AttrID(i.Attrs.Allocate(attr))
			if idx == 0 {
				attrStart = id
			}
		}
	}
	payloadID := i.newLetPayload(name, typeID, value, isMut, visibility, attrStart, attrCount, span)
	return i.New(ItemLet, span, payloadID)
}
