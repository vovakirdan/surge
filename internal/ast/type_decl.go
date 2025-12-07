package ast

import "surge/internal/source"

type TypeDeclKind uint8

const (
	TypeDeclAlias TypeDeclKind = iota
	TypeDeclStruct
	TypeDeclUnion
	TypeDeclEnum
)

type TypeItem struct {
	Name                  source.StringID
	Generics              []source.StringID
	GenericCommas         []source.Span
	GenericsTrailingComma bool
	GenericsSpan          source.Span
	TypeParamsStart       TypeParamID
	TypeParamsCount       uint32
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

type TypeEnumDecl struct {
	BaseType      TypeID
	BaseTypeSpan  source.Span
	ColonSpan     source.Span
	VariantsStart EnumVariantID
	VariantsCount uint32
	VariantCommas []source.Span
	HasTrailing   bool
	BodySpan      source.Span
}

type EnumVariant struct {
	Name       source.StringID
	NameSpan   source.Span
	Value      ExprID
	AssignSpan source.Span
	Span       source.Span
}

type EnumVariantSpec struct {
	Name       source.StringID
	NameSpan   source.Span
	Value      ExprID
	AssignSpan source.Span
	Span       source.Span
}
