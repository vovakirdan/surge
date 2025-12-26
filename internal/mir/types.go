package mir

import (
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type FuncID int32
type BlockID int32
type LocalID int32
type GlobalID int32

const (
	NoFuncID   FuncID   = -1
	NoBlockID  BlockID  = -1
	NoLocalID  LocalID  = -1
	NoGlobalID GlobalID = -1
)

type LocalFlags uint8

const (
	LocalFlagCopy LocalFlags = 1 << iota
	LocalFlagOwn
	LocalFlagRef
	LocalFlagRefMut
	LocalFlagPtr
)

type Local struct {
	Sym   symbols.SymbolID
	Type  types.TypeID
	Flags LocalFlags
	Name  string
	Span  source.Span
}

type PlaceProjKind uint8

const (
	PlaceProjDeref PlaceProjKind = iota
	PlaceProjField
	PlaceProjIndex
)

type PlaceProj struct {
	Kind PlaceProjKind

	FieldName  string
	FieldIdx   int
	IndexLocal LocalID
}

type PlaceKind uint8

const (
	PlaceLocal PlaceKind = iota
	PlaceGlobal
)

type Place struct {
	Kind   PlaceKind
	Local  LocalID
	Global GlobalID
	Proj   []PlaceProj
}

func (p Place) IsValid() bool {
	switch p.Kind {
	case PlaceGlobal:
		return p.Global != NoGlobalID
	default:
		return p.Local != NoLocalID
	}
}
