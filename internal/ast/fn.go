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
	Span     source.Span
}

type FnItem struct {
	Name                  source.StringID
	Generics              []source.StringID
	GenericCommas         []source.Span
	GenericsTrailingComma bool
	GenericsSpan          source.Span
	ParamsStart           FnParamID
	ParamsCount           uint32
	// Lossless bits for params list:
	// positions of ',' between params and whether there was a trailing comma before ')'
	ParamCommas         []source.Span
	ParamsTrailingComma bool
	FnKeywordSpan       source.Span
	ParamsSpan          source.Span
	ReturnSpan          source.Span
	SemicolonSpan       source.Span
	ReturnType          TypeID
	Body                StmtID
	Flags               FnModifier
	AttrStart           AttrID
	AttrCount           uint32
	Span                source.Span
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
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	paramsStart FnParamID,
	paramsCount uint32,
	paramCommas []source.Span,
	paramsTrailing bool,
	fnKwSpan source.Span,
	paramsSpan source.Span,
	returnSpan source.Span,
	semicolonSpan source.Span,
	returnType TypeID,
	body StmtID,
	flags FnModifier,
	attrStart AttrID,
	attrCount uint32,
	span source.Span,
) PayloadID {
	payload := i.Fns.Allocate(FnItem{
		Name:                  name,
		Generics:              generics,
		GenericCommas:         append([]source.Span(nil), genericCommas...),
		GenericsTrailingComma: genericsTrailing,
		GenericsSpan:          genericsSpan,
		ParamsStart:           paramsStart,
		ParamsCount:           paramsCount,
		ParamCommas:           append([]source.Span(nil), paramCommas...),
		ParamsTrailingComma:   paramsTrailing,
		FnKeywordSpan:         fnKwSpan,
		ParamsSpan:            paramsSpan,
		ReturnSpan:            returnSpan,
		SemicolonSpan:         semicolonSpan,
		ReturnType:            returnType,
		Body:                  body,
		Flags:                 flags,
		AttrStart:             attrStart,
		AttrCount:             attrCount,
		Span:                  span,
	})
	return PayloadID(payload)
}

func (i *Items) NewFnParam(name source.StringID, typ TypeID, def ExprID, variadic bool) FnParamID {
	return FnParamID(i.FnParams.Allocate(FnParam{
		Name:     name,
		Type:     typ,
		Default:  def,
		Variadic: variadic,
		Span:     source.Span{},
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

func (i *Items) FnByPayload(id PayloadID) *FnItem {
	if !id.IsValid() {
		return nil
	}
	return i.Fns.Get(uint32(id))
}

func (i *Items) allocateFnParams(params []FnParam) (startID FnParamID, numberOfParams uint32) {
	if len(params) == 0 {
		return NoFnParamID, 0
	}
	var start FnParamID
	count, err := safecast.Conv[uint32](len(params))
	if err != nil {
		panic(fmt.Errorf("fn params count overflow: %w", err))
	}
	for idx, param := range params {
		id := FnParamID(i.FnParams.Allocate(param))
		if idx == 0 {
			start = id
		}
	}
	return start, count
}

func (i *Items) newFn(
	name source.StringID,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	params []FnParam,
	paramCommas []source.Span,
	paramsTrailing bool,
	fnKwSpan source.Span,
	paramsSpan source.Span,
	returnSpan source.Span,
	semicolonSpan source.Span,
	returnType TypeID,
	body StmtID,
	flags FnModifier,
	attrs []Attr,
	span source.Span,
) PayloadID {
	paramsStart, paramsCount := i.allocateFnParams(params)
	attrStart, attrCount := i.allocateAttrs(attrs)
	return i.newFnPayload(name, generics, genericCommas, genericsTrailing, genericsSpan, paramsStart, paramsCount, paramCommas, paramsTrailing, fnKwSpan, paramsSpan, returnSpan, semicolonSpan, returnType, body, flags, attrStart, attrCount, span)
}

func (i *Items) NewFn(
	name source.StringID,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	params []FnParam,
	paramCommas []source.Span,
	paramsTrailing bool,
	fnKwSpan source.Span,
	paramsSpan source.Span,
	returnSpan source.Span,
	semicolonSpan source.Span,
	returnType TypeID,
	body StmtID,
	flags FnModifier,
	attrs []Attr,
	span source.Span,
) ItemID {
	payloadID := i.newFn(name, generics, genericCommas, genericsTrailing, genericsSpan, params, paramCommas, paramsTrailing, fnKwSpan, paramsSpan, returnSpan, semicolonSpan, returnType, body, flags, attrs, span)
	return i.New(ItemFn, span, payloadID)
}

func (i *Items) NewExternFn(
	name source.StringID,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	params []FnParam,
	paramCommas []source.Span,
	paramsTrailing bool,
	fnKwSpan source.Span,
	paramsSpan source.Span,
	returnSpan source.Span,
	semicolonSpan source.Span,
	returnType TypeID,
	body StmtID,
	flags FnModifier,
	attrs []Attr,
	span source.Span,
) PayloadID {
	return i.newFn(name, generics, genericCommas, genericsTrailing, genericsSpan, params, paramCommas, paramsTrailing, fnKwSpan, paramsSpan, returnSpan, semicolonSpan, returnType, body, flags, attrs, span)
}
