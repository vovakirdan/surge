package diag

const (
	// SemaAliasMagicRedeclared marks a magic method declared on an alias whose
	// target declares the same hook for the same operands. An alias names the
	// same type as its target, but the two spellings would bind different
	// bodies, and the compiler — not the author — picks the spelling a magic
	// method is invoked through.
	SemaAliasMagicRedeclared Code = 3187
)

// aliasMagicCodeDescriptions holds the human-readable titles for alias magic
// method diagnostics. Merged into codeDescription by init() so Title()/String()
// resolve them like any other code.
var aliasMagicCodeDescriptions = map[Code]string{
	SemaAliasMagicRedeclared: "an alias cannot declare a magic method its target declares",
}

func init() {
	for c, d := range aliasMagicCodeDescriptions {
		codeDescription[c] = d
	}
}
