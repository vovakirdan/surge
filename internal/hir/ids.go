// Package hir provides the High-level Intermediate Representation for Surge.
//
// HIR sits between the AST and MIR layers. It is a typed representation
// where every expression has an associated TypeID from the semantic analysis.
// HIR preserves high-level language constructs (compare, for, async/spawn)
// with minimal desugaring - only explicit return insertion.
//
// The HIR layer is designed to be the input for:
// - Monomorphization of generic functions
// - Further lowering to MIR/CFG
// - Analysis passes that need type information
package hir

// FuncID identifies a function within an HIR module.
type FuncID uint32

// LocalID identifies a local variable or parameter within a function.
type LocalID uint32

// BlockID identifies a basic block (for future CFG support).
type BlockID uint32

// NodeID is a generic HIR node identifier.
type NodeID uint32

// Invalid ID constants (zero is sentinel).
const (
	NoFuncID  FuncID  = 0
	NoLocalID LocalID = 0
	NoBlockID BlockID = 0
	NoNodeID  NodeID  = 0
)

// IsValid returns true if the ID is valid (non-zero).
func (id FuncID) IsValid() bool  { return id != NoFuncID }
func (id LocalID) IsValid() bool { return id != NoLocalID }
func (id BlockID) IsValid() bool { return id != NoBlockID }
func (id NodeID) IsValid() bool  { return id != NoNodeID }
