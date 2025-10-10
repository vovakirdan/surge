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
