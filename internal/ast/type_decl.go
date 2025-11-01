package ast

import "surge/internal/source"

type TypeDeclKind uint8

const (
	TypeDeclAlias TypeDeclKind = iota
	TypeDeclStruct
	TypeDeclUnion
)

type TypeItem struct {
	Name                  source.StringID
	Generics              []source.StringID
	GenericCommas         []source.Span
	GenericsTrailingComma bool
	GenericsSpan          source.Span
	TypeKeywordSpan       source.Span
	AssignSpan            source.Span
	SemicolonSpan         source.Span
	AttrStart             AttrID
	AttrCount             uint32
	Kind                  TypeDeclKind
	Payload               PayloadID
	Visibility            Visibility
	Span                  source.Span
}

type TypeAliasDecl struct {
	Target TypeID
}

type TypeStructDecl struct {
	Base        TypeID
	FieldsStart TypeFieldID
	FieldsCount uint32
	FieldCommas []source.Span
	HasTrailing bool
	BodySpan    source.Span
}

type TypeStructField struct {
	Name      source.StringID
	Type      TypeID
	Default   ExprID
	AttrStart AttrID
	AttrCount uint32
	Span      source.Span
}

type TypeUnionDecl struct {
	MembersStart TypeUnionMemberID
	MembersCount uint32
	BodySpan     source.Span
}

type TypeUnionMemberKind uint8

const (
	TypeUnionMemberType TypeUnionMemberKind = iota
	TypeUnionMemberNothing
	TypeUnionMemberTag
)

type TypeUnionMember struct {
	Kind        TypeUnionMemberKind
	Type        TypeID
	TagName     source.StringID
	TagArgs     []TypeID
	ArgCommas   []source.Span
	HasTrailing bool
	ArgsSpan    source.Span
	Span        source.Span
}

type TypeStructFieldSpec struct {
	Name    source.StringID
	Type    TypeID
	Default ExprID
	Attrs   []Attr
	Span    source.Span
}

type TypeUnionMemberSpec struct {
	Kind        TypeUnionMemberKind
	Type        TypeID
	TagName     source.StringID
	TagArgs     []TypeID
	ArgCommas   []source.Span
	HasTrailing bool
	ArgsSpan    source.Span
	Span        source.Span
}
