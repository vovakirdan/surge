package mir

import (
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Func represents a function in MIR.
type Func struct {
	ID   FuncID
	Sym  symbols.SymbolID
	Name string
	Span source.Span

	Result         types.TypeID
	IsAsync        bool
	Failfast       bool
	AsyncLoweredV2 bool
	ParamCount     int

	// CapturesArriveOwned says this function's parameters are a frame's
	// captures rather than a caller's arguments, and that the frame took a
	// reference on their behalf before the function was ever entered.
	//
	// It changes what a reference-counted parameter MEANS. For an ordinary
	// call the caller keeps its binding and its reference for the whole call,
	// so such a parameter is an alias and nothing here may release it without
	// retaining first. A frame's capture is the other case: the state literal
	// that built the frame RETAINED the value into it, and the body is what
	// gives that reference back -- so releasing it at every return is not an
	// unbalanced drop but the other half of the literal's retain.
	//
	// Set by the blocking lowering. Without it the ownership verifier reads a
	// blocking body's channel capture as a borrowed argument and reports its
	// release as a finding, which is correct for the shape it thinks it is
	// looking at and wrong for the one that is there.
	CapturesArriveOwned bool

	Locals []Local
	Blocks []Block
	Entry  BlockID

	ScopeLocal LocalID
}
