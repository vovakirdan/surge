package symbols

// builtinPreludeEntries returns the default set of built-in symbols exposed to every file.
func builtinPreludeEntries() []PreludeEntry {
	return []PreludeEntry{
		{Name: "int", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "uint", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "int8", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "int16", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "int32", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "int64", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "uint8", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "uint16", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "uint32", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "uint64", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "bool", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "float", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "float16", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "float32", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "float64", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "string", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "nothing", Kind: SymbolType, Flags: SymbolFlagBuiltin},
		{Name: "Array", Kind: SymbolType, Flags: SymbolFlagBuiltin, TypeParams: []string{"T"}},
		{Name: "ArrayFixed", Kind: SymbolType, Flags: SymbolFlagBuiltin, TypeParams: []string{"T", "N"}},
		{Name: "Map", Kind: SymbolType, Flags: SymbolFlagBuiltin, TypeParams: []string{"K", "V"}},
	}
}

// mergePrelude combines default builtins with user provided entries.
func mergePrelude(custom []PreludeEntry) []PreludeEntry {
	defaults := builtinPreludeEntries()
	if len(custom) == 0 {
		return defaults
	}
	result := make([]PreludeEntry, 0, len(defaults)+len(custom))
	result = append(result, defaults...)
	result = append(result, custom...)
	return result
}
