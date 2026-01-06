package ast

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/source"
)

// ExternMemberKind distinguishes between functions and fields in an extern block.
type ExternMemberKind uint8

const (
	// ExternMemberFn represents a function member in an extern block.
	ExternMemberFn ExternMemberKind = iota
	ExternMemberField
)

// ExternBlock represents an extern block.
type ExternBlock struct {
	Target       TypeID
	AttrStart    AttrID
	AttrCount    uint32
	MembersStart ExternMemberID
	MembersCount uint32
	Span         source.Span
}

// ExternMember represents a member of an extern block.
type ExternMember struct {
	Kind  ExternMemberKind
	Fn    PayloadID
	Field ExternFieldID
	Span  source.Span
}

// ExternMemberSpec specifies a member when creating a new extern block.
type ExternMemberSpec struct {
	Kind  ExternMemberKind
	Fn    PayloadID
	Field ExternFieldID
	Span  source.Span
}

// ExternField represents a field declaration in an extern block.
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

// Extern returns the ExternBlock for the given ItemID, or nil/false if invalid.
func (i *Items) Extern(id ItemID) (*ExternBlock, bool) {
	item := i.Arena.Get(uint32(id))
	if item == nil || item.Kind != ItemExtern || !item.Payload.IsValid() {
		return nil, false
	}
	return i.Externs.Get(uint32(item.Payload)), true
}

// ExternMember returns the ExternMember for the given ExternMemberID.
func (i *Items) ExternMember(id ExternMemberID) *ExternMember {
	if !id.IsValid() {
		return nil
	}
	return i.ExternMembers.Get(uint32(id))
}

// ExternField returns the ExternField for the given ExternFieldID.
func (i *Items) ExternField(id ExternFieldID) *ExternField {
	if !id.IsValid() {
		return nil
	}
	return i.ExternFields.Get(uint32(id))
}

// NewExtern creates a new extern block item.
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

// NewExternField creates a new extern field payload.
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
