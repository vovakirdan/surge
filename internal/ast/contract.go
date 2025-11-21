package ast

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/source"
)

type ContractItemKind uint8

const (
	ContractItemField ContractItemKind = iota
	ContractItemFn
)

type ContractDecl struct {
	Name                  source.StringID
	NameSpan              source.Span
	Generics              []source.StringID
	GenericCommas         []source.Span
	GenericsTrailingComma bool
	GenericsSpan          source.Span
	ContractKeywordSpan   source.Span
	BodySpan              source.Span
	ItemsStart            ContractItemID
	ItemsCount            uint32
	AttrStart             AttrID
	AttrCount             uint32
	Visibility            Visibility
	Span                  source.Span
}

type ContractItem struct {
	Kind    ContractItemKind
	Payload PayloadID
	Span    source.Span
}

type ContractFieldReq struct {
	Name             source.StringID
	NameSpan         source.Span
	Type             TypeID
	FieldKeywordSpan source.Span
	ColonSpan        source.Span
	SemicolonSpan    source.Span
	AttrStart        AttrID
	AttrCount        uint32
	Span             source.Span
}

type ContractFnReq struct {
	Name                  source.StringID
	NameSpan              source.Span
	Generics              []source.StringID
	GenericCommas         []source.Span
	GenericsTrailingComma bool
	GenericsSpan          source.Span
	ParamsStart           FnParamID
	ParamsCount           uint32
	ParamCommas           []source.Span
	ParamsTrailingComma   bool
	FnKeywordSpan         source.Span
	ParamsSpan            source.Span
	ReturnSpan            source.Span
	SemicolonSpan         source.Span
	ReturnType            TypeID
	Flags                 FnModifier
	AttrStart             AttrID
	AttrCount             uint32
	Span                  source.Span
}

type ContractItemSpec struct {
	Kind    ContractItemKind
	Payload PayloadID
	Span    source.Span
}

func (i *Items) Contract(id ItemID) (*ContractDecl, bool) {
	item := i.Arena.Get(uint32(id))
	if item == nil || item.Kind != ItemContract || !item.Payload.IsValid() {
		return nil, false
	}
	return i.Contracts.Get(uint32(item.Payload)), true
}

func (i *Items) ContractItem(id ContractItemID) *ContractItem {
	if !id.IsValid() {
		return nil
	}
	return i.ContractItems.Get(uint32(id))
}

func (i *Items) ContractField(id ContractFieldID) *ContractFieldReq {
	if !id.IsValid() {
		return nil
	}
	return i.ContractFields.Get(uint32(id))
}

func (i *Items) ContractFn(id ContractFnID) *ContractFnReq {
	if !id.IsValid() {
		return nil
	}
	return i.ContractFns.Get(uint32(id))
}

func (i *Items) GetContractItemIDs(contract *ContractDecl) []ContractItemID {
	if contract == nil || contract.ItemsCount == 0 || !contract.ItemsStart.IsValid() {
		return nil
	}
	items := make([]ContractItemID, contract.ItemsCount)
	start := uint32(contract.ItemsStart)
	for idx := range contract.ItemsCount {
		items[idx] = ContractItemID(start + uint32(idx))
	}
	return items
}

func (i *Items) NewContractField(
	name source.StringID,
	nameSpan source.Span,
	typ TypeID,
	fieldKwSpan source.Span,
	colonSpan source.Span,
	semicolonSpan source.Span,
	attrs []Attr,
	span source.Span,
) PayloadID {
	attrStart, attrCount := i.allocateAttrs(attrs)
	payload := ContractFieldReq{
		Name:             name,
		NameSpan:         nameSpan,
		Type:             typ,
		FieldKeywordSpan: fieldKwSpan,
		ColonSpan:        colonSpan,
		SemicolonSpan:    semicolonSpan,
		AttrStart:        attrStart,
		AttrCount:        attrCount,
		Span:             span,
	}
	return PayloadID(i.ContractFields.Allocate(payload))
}

func (i *Items) newContractFnPayload(
	name source.StringID,
	nameSpan source.Span,
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
	flags FnModifier,
	attrStart AttrID,
	attrCount uint32,
	span source.Span,
) PayloadID {
	payload := i.ContractFns.Allocate(ContractFnReq{
		Name:                  name,
		NameSpan:              nameSpan,
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
		Flags:                 flags,
		AttrStart:             attrStart,
		AttrCount:             attrCount,
		Span:                  span,
	})
	return PayloadID(payload)
}

func (i *Items) NewContractFn(
	name source.StringID,
	nameSpan source.Span,
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
	flags FnModifier,
	attrs []Attr,
	span source.Span,
) PayloadID {
	paramsStart, paramsCount := i.allocateFnParams(params)
	attrStart, attrCount := i.allocateAttrs(attrs)
	return i.newContractFnPayload(
		name,
		nameSpan,
		generics,
		genericCommas,
		genericsTrailing,
		genericsSpan,
		paramsStart,
		paramsCount,
		paramCommas,
		paramsTrailing,
		fnKwSpan,
		paramsSpan,
		returnSpan,
		semicolonSpan,
		returnType,
		flags,
		attrStart,
		attrCount,
		span,
	)
}

func (i *Items) NewContract(
	name source.StringID,
	nameSpan source.Span,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	contractKwSpan source.Span,
	bodySpan source.Span,
	attrs []Attr,
	items []ContractItemSpec,
	visibility Visibility,
	span source.Span,
) ItemID {
	attrStart, attrCount := i.allocateAttrs(attrs)

	var itemsStart ContractItemID
	itemCount, err := safecast.Conv[uint32](len(items))
	if err != nil {
		panic(fmt.Errorf("contract items count overflow: %w", err))
	}
	if itemCount > 0 {
		for idx, spec := range items {
			record := ContractItem(spec)
			itemID := ContractItemID(i.ContractItems.Allocate(record))
			if idx == 0 {
				itemsStart = itemID
			}
		}
	}

	payload := ContractDecl{
		Name:                  name,
		NameSpan:              nameSpan,
		Generics:              append([]source.StringID(nil), generics...),
		GenericCommas:         append([]source.Span(nil), genericCommas...),
		GenericsTrailingComma: genericsTrailing,
		GenericsSpan:          genericsSpan,
		ContractKeywordSpan:   contractKwSpan,
		BodySpan:              bodySpan,
		ItemsStart:            itemsStart,
		ItemsCount:            itemCount,
		AttrStart:             attrStart,
		AttrCount:             attrCount,
		Visibility:            visibility,
		Span:                  span,
	}

	payloadID := i.Contracts.Allocate(payload)
	return i.New(ItemContract, span, PayloadID(payloadID))
}
