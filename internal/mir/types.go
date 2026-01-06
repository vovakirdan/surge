package mir

import (
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// FuncID identifies a function in MIR.
type FuncID int32

// BlockID identifies a basic block in MIR.
type BlockID int32

// LocalID identifies a local variable in MIR.
type LocalID int32

// GlobalID identifies a global variable in MIR.
type GlobalID int32

const (
	// NoFuncID indicates no function.
	NoFuncID FuncID = -1
	// NoBlockID indicates no block.
	NoBlockID  BlockID  = -1
	NoLocalID  LocalID  = -1
	NoGlobalID GlobalID = -1
)

// LocalFlags represents flags for local variables.
type LocalFlags uint8

const (
	// LocalFlagCopy indicates a copy local flag.
	LocalFlagCopy LocalFlags = 1 << iota
	// LocalFlagOwn indicates an own local flag.
	LocalFlagOwn
	LocalFlagRef
	LocalFlagRefMut
	LocalFlagPtr
)

// Local represents a local variable in MIR.
type Local struct {
	Sym   symbols.SymbolID
	Type  types.TypeID
	Flags LocalFlags
	Name  string
	Span  source.Span
}

// PlaceProjKind distinguishes place projection kinds.
type PlaceProjKind uint8

const (
	// PlaceProjDeref represents a dereference projection.
	PlaceProjDeref PlaceProjKind = iota
	// PlaceProjField represents a field projection.
	PlaceProjField
	PlaceProjIndex
)

// PlaceProj represents a place projection.
type PlaceProj struct {
	Kind PlaceProjKind

	FieldName  string
	FieldIdx   int
	IndexLocal LocalID
}

// PlaceKind distinguishes place kinds.
type PlaceKind uint8

const (
	// PlaceLocal represents a local place.
	PlaceLocal PlaceKind = iota
	// PlaceGlobal represents a global place.
	PlaceGlobal
)

// Place represents a memory place in MIR.
type Place struct {
	Kind   PlaceKind
	Local  LocalID
	Global GlobalID
	Proj   []PlaceProj
}

// IsValid reports whether the place is valid.
func (p Place) IsValid() bool {
	switch p.Kind {
	case PlaceGlobal:
		return p.Global != NoGlobalID
	default:
		return p.Local != NoLocalID
	}
}
