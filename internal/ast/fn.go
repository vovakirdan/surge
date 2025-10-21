package ast

import "surge/internal/source"

type FnAttr uint8

const (
	FnAttrExtern FnAttr = 1 << iota
	FnAttrAsync
	FnAttrUnsafe
	FnAttrPure
	FnAttrOverload
	FnAttrOverride
	FnAttrInline
)

type FnParam struct {
	Name    source.StringID // может быть source.NoStringID для `_`
	Type    TypeID          // обязательная аннотация
	Default ExprID          // ast.NoExprID, если нет значения
}

type FnItem struct {
	Name        source.StringID
	ParamsStart FnParamID
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
	paramsStart FnParamID,
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

func (i *Items) GetFnParamIDs(fn *FnItem) []FnParamID {
	if fn == nil || fn.ParamsCount == 0 || !fn.ParamsStart.IsValid() {
		return nil
	}
	params := make([]FnParamID, fn.ParamsCount)
	start := uint32(fn.ParamsStart)
	for j := uint32(0); j < fn.ParamsCount; j++ {
		params[j] = FnParamID(start + j)
	}
	return params
}

func (i *Items) NewFn(
	name source.StringID,
	params []FnParam,
	returnType TypeID,
	body StmtID,
	attr FnAttr,
	span source.Span,
) ItemID {
	var paramsStart FnParamID
	paramsCount := uint32(len(params))
	if paramsCount > 0 {
		for idx, param := range params {
			id := FnParamID(i.FnParams.Allocate(param))
			if idx == 0 {
				paramsStart = id
			}
		}
	}
	payloadID := i.newFnPayload(name, paramsStart, paramsCount, returnType, body, attr, span)
	return i.New(ItemFn, span, payloadID)
}
