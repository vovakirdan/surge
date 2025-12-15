package mir

type TermKind uint8

const (
	TermNone TermKind = iota
	TermReturn
	TermGoto
	TermIf
	TermSwitchTag
	TermUnreachable
)

type Terminator struct {
	Kind TermKind

	Return      ReturnTerm
	Goto        GotoTerm
	If          IfTerm
	SwitchTag   SwitchTagTerm
	Unreachable struct{}
}

type ReturnTerm struct {
	HasValue bool
	Value    Operand
}

type GotoTerm struct {
	Target BlockID
}

type IfTerm struct {
	Cond Operand
	Then BlockID
	Else BlockID
}

type SwitchTagCase struct {
	TagName string
	Target  BlockID
}

type SwitchTagTerm struct {
	Value   Operand
	Cases   []SwitchTagCase
	Default BlockID
}
