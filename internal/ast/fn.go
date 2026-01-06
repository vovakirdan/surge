package ast

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/source"
)

// FnModifier represents modifiers for a function.
type FnModifier uint64

const (
	// FnModifierAsync marks an async function.
	FnModifierAsync FnModifier = 1 << iota
	FnModifierPublic
)

// FnParam represents a function parameter.
type FnParam struct {
	Name      source.StringID // может быть source.NoStringID для `_`
	Type      TypeID          // обязательная аннотация
	Default   ExprID          // ast.NoExprID, если нет значения
	Variadic  bool
	AttrStart AttrID
	AttrCount uint32
	Span      source.Span
}

// FnItem represents a function declaration.
type FnItem struct {
	Name                  source.StringID
	NameSpan              source.Span
	Generics              []source.StringID
	GenericCommas         []source.Span
	GenericsTrailingComma bool
	GenericsSpan          source.Span
	TypeParamsStart       TypeParamID
	TypeParamsCount       uint32
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

// Fn returns the FnItem for the given ItemID, or nil/false if invalid.
func (i *Items) Fn(id ItemID) (*FnItem, bool) {
	item := i.Arena.Get(uint32(id))
	if item == nil || item.Kind != ItemFn {
		return nil, false
	}
	return i.Fns.Get(uint32(item.Payload)), true
}

func (i *Items) newFnPayload(
	name source.StringID,
	nameSpan source.Span,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	typeParams []TypeParamSpec,
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
	typeParamsStart, typeParamsCount := i.allocateTypeParams(typeParams)
	payload := i.Fns.Allocate(FnItem{
		Name:                  name,
		NameSpan:              nameSpan,
		Generics:              generics,
		GenericCommas:         append([]source.Span(nil), genericCommas...),
		GenericsTrailingComma: genericsTrailing,
		GenericsSpan:          genericsSpan,
		TypeParamsStart:       typeParamsStart,
		TypeParamsCount:       typeParamsCount,
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

// NewFnParam creates a new function parameter.
func (i *Items) NewFnParam(name source.StringID, typ TypeID, def ExprID, variadic bool) FnParamID {
	return FnParamID(i.FnParams.Allocate(FnParam{
		Name:      name,
		Type:      typ,
		Default:   def,
		Variadic:  variadic,
		AttrStart: NoAttrID,
		AttrCount: 0,
		Span:      source.Span{},
	}))
}

// FnParam returns the FnParam for the given FnParamID.
func (i *Items) FnParam(id FnParamID) *FnParam {
	return i.FnParams.Get(uint32(id))
}

// GetFnParamIDs returns all parameter IDs for the given function.
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

// GetFnTypeParamIDs returns all type parameter IDs for the given function.
func (i *Items) GetFnTypeParamIDs(fn *FnItem) []TypeParamID {
	if fn == nil || fn.TypeParamsCount == 0 || !fn.TypeParamsStart.IsValid() {
		return nil
	}
	params := make([]TypeParamID, fn.TypeParamsCount)
	start := uint32(fn.TypeParamsStart)
	for idx := range fn.TypeParamsCount {
		params[idx] = TypeParamID(start + uint32(idx))
	}
	return params
}

// FnByPayload returns the FnItem for the given PayloadID.
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
	nameSpan source.Span,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	typeParams []TypeParamSpec,
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
	return i.newFnPayload(name, nameSpan, generics, genericCommas, genericsTrailing, genericsSpan, typeParams, paramsStart, paramsCount, paramCommas, paramsTrailing, fnKwSpan, paramsSpan, returnSpan, semicolonSpan, returnType, body, flags, attrStart, attrCount, span)
}

// NewFn creates a new function item.
func (i *Items) NewFn(
	name source.StringID,
	nameSpan source.Span,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	typeParams []TypeParamSpec,
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
	payloadID := i.newFn(name, nameSpan, generics, genericCommas, genericsTrailing, genericsSpan, typeParams, params, paramCommas, paramsTrailing, fnKwSpan, paramsSpan, returnSpan, semicolonSpan, returnType, body, flags, attrs, span)
	return i.New(ItemFn, span, payloadID)
}

// NewExternFn creates a new extern function payload.
func (i *Items) NewExternFn(
	name source.StringID,
	nameSpan source.Span,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	typeParams []TypeParamSpec,
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
	return i.newFn(name, nameSpan, generics, genericCommas, genericsTrailing, genericsSpan, typeParams, params, paramCommas, paramsTrailing, fnKwSpan, paramsSpan, returnSpan, semicolonSpan, returnType, body, flags, attrs, span)
}
