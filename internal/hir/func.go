package hir

import (
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// FuncFlags represents function modifiers as a bitmask.
type FuncFlags uint32

const (
	// FuncAsync indicates an async function.
	FuncAsync FuncFlags = 1 << iota
	// FuncFailfast indicates fail-fast structured concurrency.
	FuncFailfast
	// FuncIntrinsic indicates an intrinsic function (compiler-provided).
	FuncIntrinsic
	// FuncEntrypoint indicates the program entry point.
	FuncEntrypoint
	// FuncPublic indicates a public function.
	FuncPublic
	// FuncOverload indicates an overloaded function.
	FuncOverload
	// FuncOverride indicates an overriding function.
	FuncOverride
)

// HasFlag returns true if the given flag is set.
func (f FuncFlags) HasFlag(flag FuncFlags) bool {
	return f&flag != 0
}

// String returns a human-readable representation of flags.
func (f FuncFlags) String() string {
	s := ""
	if f.HasFlag(FuncPublic) {
		s += "pub "
	}
	if f.HasFlag(FuncAsync) {
		s += "async "
	}
	if f.HasFlag(FuncFailfast) {
		s += "@failfast "
	}
	if f.HasFlag(FuncIntrinsic) {
		s += "@intrinsic "
	}
	if f.HasFlag(FuncEntrypoint) {
		s += "@entrypoint "
	}
	if f.HasFlag(FuncOverload) {
		s += "@overload "
	}
	if f.HasFlag(FuncOverride) {
		s += "@override "
	}
	return s
}

// GenericParam represents a generic type parameter.
type GenericParam struct {
	Name   string
	Bounds []types.TypeID // Contract/trait bounds
	Span   source.Span
}

// Param represents a function parameter.
type Param struct {
	Name       string           // Parameter name
	SymbolID   symbols.SymbolID // Symbol for this parameter
	Type       types.TypeID     // Parameter type
	Ownership  Ownership        // Ownership qualifier
	Span       source.Span      // Source location
	HasDefault bool             // true if parameter has default value
	Default    *Expr            // Default value (nil if none)
}

// Func represents an HIR function.
type Func struct {
	ID            FuncID           // HIR function identifier
	Name          string           // Function name
	SymbolID      symbols.SymbolID // Symbol table entry
	Span          source.Span      // Source location
	GenericParams []GenericParam   // Generic type parameters (nil if not generic)
	Params        []Param          // Function parameters
	Result        types.TypeID     // Return type (NoTypeID for void/nothing)
	Flags         FuncFlags        // Function modifiers
	Body          *Block           // Function body (nil for intrinsics/externals)

	// Borrow and MovePlan are derived artefacts produced from sema borrow checker data.
	Borrow   *BorrowGraph
	MovePlan *MovePlan
}

// IsAsync returns true if this is an async function.
func (f *Func) IsAsync() bool {
	return f.Flags.HasFlag(FuncAsync)
}

// IsIntrinsic returns true if this is an intrinsic function.
func (f *Func) IsIntrinsic() bool {
	return f.Flags.HasFlag(FuncIntrinsic)
}

// IsPublic returns true if this is a public function.
func (f *Func) IsPublic() bool {
	return f.Flags.HasFlag(FuncPublic)
}

// IsGeneric returns true if this function has generic parameters.
func (f *Func) IsGeneric() bool {
	return len(f.GenericParams) > 0
}

// HasBody returns true if this function has a body.
func (f *Func) HasBody() bool {
	return f.Body != nil
}
