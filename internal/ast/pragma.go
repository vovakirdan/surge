package ast

import "surge/internal/source"

// PragmaFlags enumerates recognized pragma switches.
type PragmaFlags uint32

const (
	PragmaFlagNone      PragmaFlags = 0
	PragmaFlagDirective PragmaFlags = 1 << iota
	PragmaFlagNoStd
	PragmaFlagStrict
	PragmaFlagUnsafe
)

// Pragma represents the module-level pragma metadata captured by the parser.
// It records recognized flags as well as the raw pragma entries for future phases.
type Pragma struct {
	Span    source.Span
	Flags   PragmaFlags
	Entries []PragmaEntry
}

// IsEmpty reports whether pragma information was present.
func (p Pragma) IsEmpty() bool {
	return p.Span == source.Span{} && len(p.Entries) == 0 && p.Flags == PragmaFlagNone
}

// PragmaEntry stores the raw textual data of a single pragma flag as written in the source file.
// Raw holds the exact substring (including optional arguments) while Name is the interned identifier.
type PragmaEntry struct {
	Name source.StringID
	Raw  source.StringID
	Span source.Span
}
