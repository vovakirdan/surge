package ast

import "surge/internal/source"

type FnItem struct {
	Name       source.StringID
	ReturnType TypeID
	Body       StmtID
	Span       source.Span
}

func (i *Items) Fn(id ItemID) (*FnItem, bool) {
	item := i.Arena.Get(uint32(id))
	if item == nil || item.Kind != ItemFn {
		return nil, false
	}
	return i.Fns.Get(uint32(item.Payload)), true
}

func (i *Items) newFnPayload(
	name source.StringID,
	returnType TypeID,
	body StmtID,
	span source.Span,
) PayloadID {
	payload := i.Fns.Allocate(FnItem{
		Name:       name,
		ReturnType: returnType,
		Body:       body,
		Span:       span,
	})
	return PayloadID(payload)
}

func (i *Items) NewFn(
	name source.StringID,
	returnType TypeID,
	body StmtID,
	span source.Span,
) ItemID {
	payloadID := i.newFnPayload(name, returnType, body, span)
	return i.New(ItemFn, span, payloadID)
}
