package ast

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/source"
)

type FnModifier uint64

const (
	FnModifierAsync FnModifier = 1 << iota
	FnModifierPublic
)

type FnParam struct {
	Name     source.StringID // может быть source.NoStringID для `_`
	Type     TypeID          // обязательная аннотация
	Default  ExprID          // ast.NoExprID, если нет значения
	Variadic bool
}

type FnItem struct {
	Name        source.StringID
	Generics    []source.StringID
	ParamsStart FnParamID
	ParamsCount uint32
	ReturnType  TypeID
	Body        StmtID
	Flags       FnModifier
	AttrStart   AttrID
	AttrCount   uint32
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
	generics []source.StringID,
	paramsStart FnParamID,
	paramsCount uint32,
	returnType TypeID,
	body StmtID,
	flags FnModifier,
	attrStart AttrID,
	attrCount uint32,
	span source.Span,
) PayloadID {
	payload := i.Fns.Allocate(FnItem{
		Name:        name,
		Generics:    generics,
		ParamsStart: paramsStart,
		ParamsCount: paramsCount,
		ReturnType:  returnType,
		Body:        body,
		Flags:       flags,
		AttrStart:   attrStart,
		AttrCount:   attrCount,
		Span:        span,
	})
	return PayloadID(payload)
}

func (i *Items) NewFnParam(name source.StringID, typ TypeID, def ExprID, variadic bool) FnParamID {
	return FnParamID(i.FnParams.Allocate(FnParam{
		Name:     name,
		Type:     typ,
		Default:  def,
		Variadic: variadic,
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
	for j := range fn.ParamsCount {
		params[j] = FnParamID(start + j)
	}
	return params
}

func (i *Items) NewFn(
	name source.StringID,
	generics []source.StringID,
	params []FnParam,
	returnType TypeID,
	body StmtID,
	flags FnModifier,
	attrs []Attr,
	span source.Span,
) ItemID {
	var paramsStart FnParamID
	paramsCount, err := safecast.Conv[uint32](len(params))
	if err != nil {
		panic(fmt.Errorf("fn params count overflow: %w", err))
	}
	if paramsCount > 0 {
		for idx, param := range params {
			id := FnParamID(i.FnParams.Allocate(param))
			if idx == 0 {
				paramsStart = id
			}
		}
	}
	var attrStart AttrID
	var attrCount uint32
	attrCount, err = safecast.Conv[uint32](len(attrs))
	if err != nil {
		panic(fmt.Errorf("fn attrs count overflow: %w", err))
	}
	if attrCount > 0 {
		for idx, attr := range attrs {
			id := AttrID(i.Attrs.Allocate(attr))
			if idx == 0 {
				attrStart = id
			}
		}
	}
	payloadID := i.newFnPayload(name, generics, paramsStart, paramsCount, returnType, body, flags, attrStart, attrCount, span)
	return i.New(ItemFn, span, payloadID)
}
