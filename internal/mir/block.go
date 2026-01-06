package mir

// Block represents a basic block in MIR.
type Block struct {
	ID     BlockID
	Instrs []Instr
	Term   Terminator
}

// Terminated reports whether the block has a terminator.
func (b *Block) Terminated() bool {
	if b == nil {
		return true
	}
	return b.Term.Kind != TermNone
}
