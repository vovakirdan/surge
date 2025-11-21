package ast

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/source"
)

type ExternMemberKind uint8

const (
	ExternMemberFn ExternMemberKind = iota
	ExternMemberField
)

type ExternBlock struct {
	Target       TypeID
	AttrStart    AttrID
	AttrCount    uint32
	MembersStart ExternMemberID
	MembersCount uint32
	Span         source.Span
}

type ExternMember struct {
	Kind  ExternMemberKind
	Fn    PayloadID
	Field ExternFieldID
	Span  source.Span
}

type ExternMemberSpec struct {
	Kind  ExternMemberKind
	Fn    PayloadID
	Field ExternFieldID
	Span  source.Span
}

type ExternField struct {
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

func (i *Items) Extern(id ItemID) (*ExternBlock, bool) {
	item := i.Arena.Get(uint32(id))
	if item == nil || item.Kind != ItemExtern || !item.Payload.IsValid() {
		return nil, false
	}
	return i.Externs.Get(uint32(item.Payload)), true
}

func (i *Items) ExternMember(id ExternMemberID) *ExternMember {
	if !id.IsValid() {
		return nil
	}
	return i.ExternMembers.Get(uint32(id))
}

func (i *Items) ExternField(id ExternFieldID) *ExternField {
	if !id.IsValid() {
		return nil
	}
	return i.ExternFields.Get(uint32(id))
}

func (i *Items) NewExtern(
	target TypeID,
	attrs []Attr,
	members []ExternMemberSpec,
	span source.Span,
) ItemID {
	attrStart, attrCount := i.allocateAttrs(attrs)

	var membersStart ExternMemberID
	memberCount, err := safecast.Conv[uint32](len(members))
	if err != nil {
		panic(fmt.Errorf("extern members count overflow: %w", err))
	}
	if memberCount > 0 {
		for idx, spec := range members {
			record := ExternMember(spec)
			memberID := ExternMemberID(i.ExternMembers.Allocate(record))
			if idx == 0 {
				membersStart = memberID
			}
		}
	}

	externPayload := i.Externs.Allocate(ExternBlock{
		Target:       target,
		AttrStart:    attrStart,
		AttrCount:    attrCount,
		MembersStart: membersStart,
		MembersCount: memberCount,
		Span:         span,
	})

	return i.New(ItemExtern, span, PayloadID(externPayload))
}

func (i *Items) NewExternField(
	name source.StringID,
	nameSpan source.Span,
	typ TypeID,
	fieldKwSpan source.Span,
	colonSpan source.Span,
	semicolonSpan source.Span,
	attrs []Attr,
	span source.Span,
) ExternFieldID {
	attrStart, attrCount := i.allocateAttrs(attrs)
	field := ExternField{
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
	return ExternFieldID(i.ExternFields.Allocate(field))
}
