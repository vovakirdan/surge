package ast

import "surge/internal/source"

type LetItem struct {
	Name       source.StringID
	Type       TypeID // NoTypeID if type is inferred
	Value      ExprID // NoExprID if no initialization
	IsMut      bool   // mut modifier
	Visibility Visibility
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
	span source.Span,
) PayloadID {
	payload := i.Lets.Allocate(LetItem{
		Name:       name,
		Type:       typeID,
		Value:      value,
		IsMut:      isMut,
		Visibility: visibility,
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
	span source.Span,
) ItemID {
	payloadID := i.newLetPayload(name, typeID, value, isMut, visibility, span)
	return i.New(ItemLet, span, payloadID)
}
