package mir

func formatLocalFlags(f LocalFlags) string {
	if f == 0 {
		return ""
	}
	var parts []string
	if f&LocalFlagCopy != 0 {
		parts = append(parts, "copy")
	}
	if f&LocalFlagOwn != 0 {
		parts = append(parts, "own")
	}
	if f&LocalFlagRef != 0 {
		parts = append(parts, "ref")
	}
	if f&LocalFlagRefMut != 0 {
		parts = append(parts, "refmut")
	}
	if f&LocalFlagPtr != 0 {
		parts = append(parts, "ptr")
	}
	if f&LocalFlagOwnsHeap != 0 {
		parts = append(parts, "owns_heap")
	}
	if len(parts) == 0 {
		return ""
	}
	return "[" + join(parts, ",") + "]"
}

func join(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += sep + parts[i]
	}
	return out
}
