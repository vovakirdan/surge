package mir

type Block struct {
	ID     BlockID
	Instrs []Instr
	Term   Terminator
}

func (b *Block) Terminated() bool {
	if b == nil {
		return true
	}
	return b.Term.Kind != TermNone
}
