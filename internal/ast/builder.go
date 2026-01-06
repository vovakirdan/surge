package ast

import (
	"surge/internal/source"
)

// Hints provides capacity hints for the builder.
type Hints struct{ Files, Items, Stmts, Exprs, Types uint }

// Builder constructs an AST.
type Builder struct {
	Files           *Files
	Items           *Items
	Stmts           *Stmts
	Exprs           *Exprs
	Types           *TypeExprs
	StringsInterner *source.Interner
}

// NewBuilder creates a Builder configured with capacity hints and a shared string interner.
//
// If any hint field is zero, a sensible default capacity is applied (Files=64, Items=128,
// Stmts=256, Exprs=256, Types=128). If stringsInterner is nil, a new interner is created.
// The returned Builder is fully initialized and non-nil.
func NewBuilder(hints Hints, stringsInterner *source.Interner) *Builder {
	if hints.Files == 0 {
		hints.Files = 1 << 6 // просто понты; 64
	}
	if hints.Items == 0 {
		hints.Items = 1 << 7
	}
	if hints.Stmts == 0 {
		hints.Stmts = 1 << 8
	}
	if hints.Exprs == 0 {
		hints.Exprs = 1 << 8
	}
	if hints.Types == 0 {
		hints.Types = 1 << 7
	}
	if stringsInterner == nil {
		stringsInterner = source.NewInterner()
	}
	return &Builder{
		Files:           NewFiles(hints.Files),
		Items:           NewItems(hints.Items),
		Stmts:           NewStmts(hints.Stmts),
		Exprs:           NewExprs(hints.Exprs),
		Types:           NewTypeExprs(hints.Types),
		StringsInterner: stringsInterner,
	}
}

// NewFile creates a new file ID.
func (b *Builder) NewFile(sp source.Span) FileID {
	return b.Files.New(sp)
}

// NewItem creates a new item ID.
func (b *Builder) NewItem(kind ItemKind, sp source.Span, payloadID PayloadID) ItemID {
	return b.Items.New(kind, sp, payloadID)
}

// NewStmt creates a new statement ID.
func (b *Builder) NewStmt(kind StmtKind, sp source.Span, payload PayloadID) StmtID {
	return b.Stmts.New(kind, sp, payload)
}

// PushItem adds an item to a file.
func (b *Builder) PushItem(file FileID, item ItemID) {
	b.Files.Get(file).Items = append(b.Files.Get(file).Items, item)
}

// NewImport creates a new import item.
func (b *Builder) NewImport(
	span source.Span,
	module []source.StringID,
	moduleAlias source.StringID,
	one ImportOne,
	hasOne bool,
	group []ImportPair,
	importAll bool,
) ItemID {
	return b.Items.NewImport(span, module, moduleAlias, one, hasOne, group, importAll)
}

// NewFnParam creates a new function parameter.
func (b *Builder) NewFnParam(name source.StringID, typ TypeID, def ExprID, variadic bool) FnParamID {
	return b.Items.NewFnParam(name, typ, def, variadic)
}

// NewFn creates a new function item.
func (b *Builder) NewFn(
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
	return b.Items.NewFn(name, nameSpan, generics, genericCommas, genericsTrailing, genericsSpan, typeParams, params, paramCommas, paramsTrailing, fnKwSpan, paramsSpan, returnSpan, semicolonSpan, returnType, body, flags, attrs, span)
}

// NewExternFn creates a new extern function payload.
func (b *Builder) NewExternFn(
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
	return b.Items.NewExternFn(name, nameSpan, generics, genericCommas, genericsTrailing, genericsSpan, typeParams, params, paramCommas, paramsTrailing, fnKwSpan, paramsSpan, returnSpan, semicolonSpan, returnType, body, flags, attrs, span)
}

// NewContractField creates a new contract field payload.
func (b *Builder) NewContractField(
	name source.StringID,
	nameSpan source.Span,
	typ TypeID,
	fieldKwSpan source.Span,
	colonSpan source.Span,
	semicolonSpan source.Span,
	attrs []Attr,
	span source.Span,
) PayloadID {
	return b.Items.NewContractField(name, nameSpan, typ, fieldKwSpan, colonSpan, semicolonSpan, attrs, span)
}

// NewContractFn creates a new contract function payload.
func (b *Builder) NewContractFn(
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
	return b.Items.NewContractFn(name, nameSpan, generics, genericCommas, genericsTrailing, genericsSpan, typeParams, params, paramCommas, paramsTrailing, fnKwSpan, paramsSpan, returnSpan, semicolonSpan, returnType, body, flags, attrs, span)
}

// NewContract creates a new contract item.
func (b *Builder) NewContract(
	name source.StringID,
	nameSpan source.Span,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	typeParams []TypeParamSpec,
	contractKwSpan source.Span,
	bodySpan source.Span,
	attrs []Attr,
	items []ContractItemSpec,
	visibility Visibility,
	span source.Span,
) ItemID {
	return b.Items.NewContract(name, nameSpan, generics, genericCommas, genericsTrailing, genericsSpan, typeParams, contractKwSpan, bodySpan, attrs, items, visibility, span)
}

// NewTypeAlias creates a new type alias item.
func (b *Builder) NewTypeAlias(
	name source.StringID,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	typeParams []TypeParamSpec,
	typeKwSpan source.Span,
	assignSpan source.Span,
	semicolonSpan source.Span,
	attrs []Attr,
	visibility Visibility,
	target TypeID,
	span source.Span,
) ItemID {
	return b.Items.NewTypeAlias(name, generics, genericCommas, genericsTrailing, genericsSpan, typeParams, typeKwSpan, assignSpan, semicolonSpan, attrs, visibility, target, span)
}

// NewTypeStruct creates a new struct type item.
func (b *Builder) NewTypeStruct(
	name source.StringID,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	typeParams []TypeParamSpec,
	typeKwSpan source.Span,
	assignSpan source.Span,
	semicolonSpan source.Span,
	attrs []Attr,
	visibility Visibility,
	base TypeID,
	fields []TypeStructFieldSpec,
	fieldCommas []source.Span,
	hasTrailing bool,
	bodySpan source.Span,
	span source.Span,
) ItemID {
	return b.Items.NewTypeStruct(name, generics, genericCommas, genericsTrailing, genericsSpan, typeParams, typeKwSpan, assignSpan, semicolonSpan, attrs, visibility, base, fields, fieldCommas, hasTrailing, bodySpan, span)
}

// NewTypeUnion creates a new union type item.
func (b *Builder) NewTypeUnion(
	name source.StringID,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	typeParams []TypeParamSpec,
	typeKwSpan source.Span,
	assignSpan source.Span,
	semicolonSpan source.Span,
	attrs []Attr,
	visibility Visibility,
	members []TypeUnionMemberSpec,
	bodySpan source.Span,
	span source.Span,
) ItemID {
	return b.Items.NewTypeUnion(name, generics, genericCommas, genericsTrailing, genericsSpan, typeParams, typeKwSpan, assignSpan, semicolonSpan, attrs, visibility, members, bodySpan, span)
}

// NewTypeEnum creates a new enum type item.
func (b *Builder) NewTypeEnum(
	name source.StringID,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	typeParams []TypeParamSpec,
	typeKwSpan source.Span,
	assignSpan source.Span,
	semicolonSpan source.Span,
	attrs []Attr,
	visibility Visibility,
	baseType TypeID,
	baseTypeSpan source.Span,
	colonSpan source.Span,
	variants []EnumVariantSpec,
	variantCommas []source.Span,
	hasTrailing bool,
	bodySpan source.Span,
	span source.Span,
) ItemID {
	return b.Items.NewTypeEnum(name, generics, genericCommas, genericsTrailing, genericsSpan, typeParams, typeKwSpan, assignSpan, semicolonSpan, attrs, visibility, baseType, baseTypeSpan, colonSpan, variants, variantCommas, hasTrailing, bodySpan, span)
}

// NewExtern creates a new extern block item.
func (b *Builder) NewExtern(
	target TypeID,
	attrs []Attr,
	members []ExternMemberSpec,
	span source.Span,
) ItemID {
	return b.Items.NewExtern(target, attrs, members, span)
}

// NewExternField creates a new extern field payload.
func (b *Builder) NewExternField(
	name source.StringID,
	nameSpan source.Span,
	typ TypeID,
	fieldKwSpan source.Span,
	colonSpan source.Span,
	semicolonSpan source.Span,
	attrs []Attr,
	span source.Span,
) ExternFieldID {
	return b.Items.NewExternField(name, nameSpan, typ, fieldKwSpan, colonSpan, semicolonSpan, attrs, span)
}

// NewTag creates a new tag item.
func (b *Builder) NewTag(
	name source.StringID,
	nameSpan source.Span,
	generics []source.StringID,
	genericCommas []source.Span,
	genericsTrailing bool,
	genericsSpan source.Span,
	typeParams []TypeParamSpec,
	tagKwSpan source.Span,
	paramsSpan source.Span,
	semicolonSpan source.Span,
	payload []TypeID,
	payloadCommas []source.Span,
	payloadTrailing bool,
	attrs []Attr,
	visibility Visibility,
	span source.Span,
) ItemID {
	return b.Items.NewTag(name, nameSpan, generics, genericCommas, genericsTrailing, genericsSpan, typeParams, tagKwSpan, paramsSpan, semicolonSpan, payload, payloadCommas, payloadTrailing, attrs, visibility, span)
}
