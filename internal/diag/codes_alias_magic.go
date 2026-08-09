package diag

const (
	// SemaAliasMagicRedeclared marks a magic method declared on an alias whose
	// target declares the same hook for the same operands. An alias names the
	// same type as its target, but the two spellings would bind different
	// bodies, and the compiler — not the author — picks the spelling a magic
	// method is invoked through.
	SemaAliasMagicRedeclared Code = 3196
)
