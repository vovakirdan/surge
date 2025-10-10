package ast

type ImportItem struct {
	Module []string // todo []stingID
	Alias  string
	One    *ImportOne
	Group  []ImportPair
}

type ImportOne struct {
	Name  string
	Alias string
}

type ImportPair struct {
	Name  string
	Alias string
}
