package ast

import "surge/internal/source"

type TypeDeclKind uint8

const (
	TypeDeclAlias TypeDeclKind = iota
	TypeDeclStruct
	TypeDeclUnion
)

type TypeItem struct {
	Name      source.StringID
	Generics  []source.StringID
	AttrStart AttrID
	AttrCount uint32
	Kind      TypeDeclKind
	Payload   PayloadID
	Span      source.Span
}

type TypeAliasDecl struct {
	Target TypeID
}

type TypeStructDecl struct {
	Base        TypeID
	FieldsStart TypeFieldID
	FieldsCount uint32
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
}

type TypeUnionMemberKind uint8

const (
	TypeUnionMemberType TypeUnionMemberKind = iota
	TypeUnionMemberNothing
	TypeUnionMemberTag
)

type TypeUnionMember struct {
	Kind    TypeUnionMemberKind
	Type    TypeID
	TagName source.StringID
	TagArgs []TypeID
	Span    source.Span
}
