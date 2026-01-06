package ast

import "surge/internal/source"

// TypeDeclKind enumerates kinds of type declarations.
type TypeDeclKind uint8

const (
	// TypeDeclAlias represents a type alias.
	TypeDeclAlias TypeDeclKind = iota
	// TypeDeclStruct represents a struct type declaration.
	TypeDeclStruct
	TypeDeclUnion
	TypeDeclEnum
)

// TypeItem represents a type declaration item.
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

// TypeAliasDecl represents a type alias declaration.
type TypeAliasDecl struct {
	Target TypeID
}

// TypeStructDecl represents a struct type declaration.
type TypeStructDecl struct {
	Base        TypeID
	FieldsStart TypeFieldID
	FieldsCount uint32
	FieldCommas []source.Span
	HasTrailing bool
	BodySpan    source.Span
}

// TypeStructField represents a field in a struct type.
type TypeStructField struct {
	Name      source.StringID
	Type      TypeID
	Default   ExprID
	AttrStart AttrID
	AttrCount uint32
	Span      source.Span
}

// TypeUnionDecl represents a union type declaration.
type TypeUnionDecl struct {
	MembersStart TypeUnionMemberID
	MembersCount uint32
	BodySpan     source.Span
}

// TypeUnionMemberKind distinguishes kinds of union members.
type TypeUnionMemberKind uint8

const (
	// TypeUnionMemberType represents a type member in a union.
	TypeUnionMemberType TypeUnionMemberKind = iota
	// TypeUnionMemberNothing represents a nothing member in a union.
	TypeUnionMemberNothing
	TypeUnionMemberTag
)

// TypeUnionMember represents a member of a union type.
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

// TypeStructFieldSpec specifies a field when creating a struct type.
type TypeStructFieldSpec struct {
	Name    source.StringID
	Type    TypeID
	Default ExprID
	Attrs   []Attr
	Span    source.Span
}

// TypeUnionMemberSpec specifies a member when creating a union type.
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

// TypeEnumDecl represents an enum type declaration.
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

// EnumVariant represents a variant of an enum type.
type EnumVariant struct {
	Name       source.StringID
	NameSpan   source.Span
	Value      ExprID
	AssignSpan source.Span
	Span       source.Span
}

// EnumVariantSpec specifies a variant when creating an enum type.
type EnumVariantSpec struct {
	Name       source.StringID
	NameSpan   source.Span
	Value      ExprID
	AssignSpan source.Span
	Span       source.Span
}
