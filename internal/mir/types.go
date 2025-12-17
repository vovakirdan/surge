package mir

import (
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type FuncID int32
type BlockID int32
type LocalID int32

const (
	NoFuncID  FuncID  = -1
	NoBlockID BlockID = -1
	NoLocalID LocalID = -1
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

type Place struct {
	Local LocalID
	Proj  []PlaceProj
}

func (p Place) IsValid() bool { return p.Local != NoLocalID }
