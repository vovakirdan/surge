package mir

// TermKind enumerates terminator kinds.
type TermKind uint8

const (
	// TermNone indicates no terminator.
	TermNone TermKind = iota
	// TermReturn indicates a return terminator.
	TermReturn
	// TermAsyncYield indicates an async yield terminator.
	TermAsyncYield
	// TermAsyncReturn indicates an async return terminator.
	TermAsyncReturn
	TermAsyncReturnCancelled
	TermGoto
	TermIf
	TermSwitchTag
	TermUnreachable
)

// Terminator represents a block terminator.
type Terminator struct {
	Kind TermKind

	Return               ReturnTerm
	AsyncYield           AsyncYieldTerm
	AsyncReturn          AsyncReturnTerm
	AsyncReturnCancelled AsyncReturnCancelledTerm
	Goto                 GotoTerm
	If                   IfTerm
	SwitchTag            SwitchTagTerm
	Unreachable          struct{}
}

// ReturnTerm represents a return terminator.
type ReturnTerm struct {
	HasValue  bool
	Value     Operand
	Early     bool
	Cancelled bool
}

// AsyncYieldTerm represents an async yield terminator.
type AsyncYieldTerm struct {
	State Operand
}

// AsyncReturnTerm represents an async return terminator.
type AsyncReturnTerm struct {
	State    Operand
	HasValue bool
	Value    Operand
}

// AsyncReturnCancelledTerm represents a cancelled async return terminator.
type AsyncReturnCancelledTerm struct {
	State Operand
}

// GotoTerm represents a goto terminator.
type GotoTerm struct {
	Target BlockID
}

// IfTerm represents an if terminator.
type IfTerm struct {
	Cond Operand
	Then BlockID
	Else BlockID
}

// SwitchTagCase represents a switch tag case.
type SwitchTagCase struct {
	TagName string
	Target  BlockID
}

// SwitchTagTerm represents a switch tag terminator.
type SwitchTagTerm struct {
	Value   Operand
	Cases   []SwitchTagCase
	Default BlockID
}
