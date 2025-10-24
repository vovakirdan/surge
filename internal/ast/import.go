package ast

import "surge/internal/source"

type ImportItem struct {
	Module      []source.StringID
	ModuleAlias source.StringID
	One         ImportOne
	HasOne      bool
	Group       []ImportPair
}

type ImportOne struct {
	Name  source.StringID
	Alias source.StringID
}

type ImportPair struct {
	Name  source.StringID
	Alias source.StringID
}

func (i *Items) Import(id ItemID) (*ImportItem, bool) {
	item := i.Arena.Get(uint32(id))
	if item == nil || item.Kind != ItemImport {
		return nil, false
	}
	return i.Imports.Get(uint32(item.Payload)), true
}

func (i *Items) newImportPayload(
	module []source.StringID,
	moduleAlias source.StringID,
	one ImportOne,
	hasOne bool,
	group []ImportPair,
) PayloadID {
	payload := i.Imports.Allocate(ImportItem{
		Module:      append([]source.StringID(nil), module...),
		ModuleAlias: moduleAlias,
		One:         one,
		HasOne:      hasOne,
		Group:       append([]ImportPair(nil), group...),
	})
	return PayloadID(payload)
}

func (i *Items) NewImport(
	span source.Span,
	module []source.StringID,
	moduleAlias source.StringID,
	one ImportOne,
	hasOne bool,
	group []ImportPair,
) ItemID {
	payloadID := i.newImportPayload(module, moduleAlias, one, hasOne, group)
	return i.New(ItemImport, span, payloadID)
}
