package ast

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/source"
)

// TypeParam represents a generic type parameter.
type TypeParam struct {
	Name       source.StringID
	NameSpan   source.Span
	ColonSpan  source.Span
	IsConst    bool
	ConstType  TypeID
	BoundsSpan source.Span
	Bounds     TypeParamBoundID
	BoundsNum  uint32
	PlusSpans  []source.Span
	Span       source.Span
}

// TypeParamBound represents a bound on a type parameter.
type TypeParamBound struct {
	Name      source.StringID
	NameSpan  source.Span
	Type      TypeID
	TypeArgs  []TypeID
	ArgCommas []source.Span
	ArgsSpan  source.Span
	AttrStart AttrID
	AttrCount uint32
	Span      source.Span
}

// TypeParamSpec specifies a type parameter during creation.
type TypeParamSpec struct {
	Name       source.StringID
	NameSpan   source.Span
	ColonSpan  source.Span
	IsConst    bool
	ConstType  TypeID
	Bounds     []TypeParamBoundSpec
	PlusSpans  []source.Span
	BoundsSpan source.Span
	Span       source.Span
}

// TypeParamBoundSpec specifies a type parameter bound during creation.
type TypeParamBoundSpec struct {
	Name      source.StringID
	NameSpan  source.Span
	Type      TypeID
	TypeArgs  []TypeID
	ArgCommas []source.Span
	ArgsSpan  source.Span
	Attrs     []Attr
	Span      source.Span
}

// TypeParam returns the TypeParam for the given TypeParamID.
func (i *Items) TypeParam(id TypeParamID) *TypeParam {
	if !id.IsValid() {
		return nil
	}
	return i.TypeParams.Get(uint32(id))
}

// TypeParamBound returns the TypeParamBound for the given TypeParamBoundID.
func (i *Items) TypeParamBound(id TypeParamBoundID) *TypeParamBound {
	if !id.IsValid() {
		return nil
	}
	return i.TypeParamBounds.Get(uint32(id))
}

// GetTypeParamIDs returns a slice of type parameter IDs starting from the given ID.
func (i *Items) GetTypeParamIDs(start TypeParamID, count uint32) []TypeParamID {
	if !start.IsValid() || count == 0 {
		return nil
	}
	result := make([]TypeParamID, count)
	base := uint32(start)
	for idx := range count {
		result[idx] = TypeParamID(base + uint32(idx))
	}
	return result
}

func (i *Items) allocateTypeParamBounds(bounds []TypeParamBoundSpec) (start TypeParamBoundID, count uint32) {
	if len(bounds) == 0 {
		return NoTypeParamBoundID, 0
	}
	for idx := range bounds {
		b := &bounds[idx]
		attrStart, attrCount := i.allocateAttrs(b.Attrs)
		record := TypeParamBound{
			Name:      b.Name,
			NameSpan:  b.NameSpan,
			Type:      b.Type,
			TypeArgs:  append([]TypeID(nil), b.TypeArgs...),
			ArgCommas: append([]source.Span(nil), b.ArgCommas...),
			ArgsSpan:  b.ArgsSpan,
			AttrStart: attrStart,
			AttrCount: attrCount,
			Span:      b.Span,
		}
		id := TypeParamBoundID(i.TypeParamBounds.Allocate(record))
		if idx == 0 {
			start = id
		}
	}
	var err error
	count, err = safecast.Conv[uint32](len(bounds))
	if err != nil {
		panic(fmt.Errorf("type param bounds overflow: %w", err))
	}
	return start, count
}

func (i *Items) allocateTypeParams(params []TypeParamSpec) (start TypeParamID, count uint32) {
	if len(params) == 0 {
		return NoTypeParamID, 0
	}
	for idx, p := range params {
		boundStart, boundCount := i.allocateTypeParamBounds(p.Bounds)
		record := TypeParam{
			Name:       p.Name,
			NameSpan:   p.NameSpan,
			ColonSpan:  p.ColonSpan,
			IsConst:    p.IsConst,
			ConstType:  p.ConstType,
			BoundsSpan: p.BoundsSpan,
			Bounds:     boundStart,
			BoundsNum:  boundCount,
			PlusSpans:  append([]source.Span(nil), p.PlusSpans...),
			Span:       p.Span,
		}
		id := TypeParamID(i.TypeParams.Allocate(record))
		if idx == 0 {
			start = id
		}
	}
	var err error
	count, err = safecast.Conv[uint32](len(params))
	if err != nil {
		panic(fmt.Errorf("type params overflow: %w", err))
	}
	return start, count
}
