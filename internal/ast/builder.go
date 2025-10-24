package ast

import (
	"surge/internal/source"
)

type Hints struct{ Files, Items, Stmts, Exprs, Types uint }

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

func (b *Builder) NewFile(sp source.Span) FileID {
	return b.Files.New(sp)
}

func (b *Builder) NewItem(kind ItemKind, sp source.Span, payloadID PayloadID) ItemID {
	return b.Items.New(kind, sp, payloadID)
}

func (b *Builder) NewStmt(kind StmtKind, sp source.Span, payload PayloadID) StmtID {
	return b.Stmts.New(kind, sp, payload)
}

func (b *Builder) PushItem(file FileID, item ItemID) {
	b.Files.Get(file).Items = append(b.Files.Get(file).Items, item)
}

func (b *Builder) NewImport(
	span source.Span,
	module []source.StringID,
	moduleAlias source.StringID,
	one ImportOne,
	hasOne bool,
	group []ImportPair,
) ItemID {
	return b.Items.NewImport(span, module, moduleAlias, one, hasOne, group)
}

func (b *Builder) NewFnParam(name source.StringID, typ TypeID, def ExprID, variadic bool) FnParamID {
	return b.Items.NewFnParam(name, typ, def, variadic)
}

func (b *Builder) NewFn(
	name source.StringID,
	generics []source.StringID,
	params []FnParam,
	returnType TypeID,
	body StmtID,
	flags FnAttr,
	attrs []Attr,
	span source.Span,
) ItemID {
	return b.Items.NewFn(name, generics, params, returnType, body, flags, attrs, span)
}