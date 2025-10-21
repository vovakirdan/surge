package ast

import "surge/internal/source"

type FnAttr uint8

const (
	FnAttrExtern FnAttr = 1 << iota
	FnAttrAsync
	FnAttrUnsafe
)

type FnParam struct {
	Name    source.StringID // может быть source.NoStringID для `_`
	Type    TypeID          // обязательная аннотация
	Default ExprID          // ast.NoExprID, если нет значения
}

type FnItem struct {
	Name        source.StringID
	ParamsStart uint32
	ParamsCount uint32
	ReturnType  TypeID
	Body        StmtID
	Attr        FnAttr
	Span        source.Span
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
	paramsStart uint32,
	paramsCount uint32,
	returnType TypeID,
	body StmtID,
	attr FnAttr,
	span source.Span,
) PayloadID {
	payload := i.Fns.Allocate(FnItem{
		Name:        name,
		ParamsStart: paramsStart,
		ParamsCount: paramsCount,
		ReturnType:  returnType,
		Body:        body,
		Attr:        attr,
		Span:        span,
	})
	return PayloadID(payload)
}

func (i *Items) NewFnParam(name source.StringID, typ TypeID, def ExprID) FnParamID {
	return FnParamID(i.FnParams.Allocate(FnParam{
		Name:    name,
		Type:    typ,
		Default: def,
	}))
}

func (i *Items) FnParam(id FnParamID) *FnParam {
	return i.FnParams.Get(uint32(id))
}

func (i *Items) GetFnParams(fn *FnItem) []*FnParam {
	if fn.ParamsCount == 0 {
		return nil
	}
	params := make([]*FnParam, fn.ParamsCount)
	for j := uint32(0); j < fn.ParamsCount; j++ {
		params[j] = i.FnParams.Get(fn.ParamsStart + j)
	}
	return params
}

func (i *Items) NewFnWithParams(
	name source.StringID,
	params []FnParam,
	returnType TypeID,
	body StmtID,
	attr FnAttr,
	span source.Span,
) ItemID {
	var paramsStart, paramsCount uint32
	if len(params) > 0 {
		paramsStart = uint32(len(i.FnParams.Slice())) + 1
		paramsCount = uint32(len(params))
		for _, param := range params {
			i.FnParams.Allocate(param)
		}
	}
	payloadID := i.newFnPayload(name, paramsStart, paramsCount, returnType, body, attr, span)
	return i.New(ItemFn, span, payloadID)
}

func (i *Items) NewFn(
	name source.StringID,
	params []FnParamID,
	returnType TypeID,
	body StmtID,
	attr FnAttr,
	span source.Span,
) ItemID {
	var paramsStart, paramsCount uint32
	if len(params) > 0 {
		paramsStart = uint32(params[0])
		paramsCount = uint32(len(params))
	}
	payloadID := i.newFnPayload(name, paramsStart, paramsCount, returnType, body, attr, span)
	return i.New(ItemFn, span, payloadID)
}
