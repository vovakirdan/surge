package mir

// cloneSet creates a copy of a localSet.
func cloneSet(s localSet) localSet {
	if len(s) == 0 {
		return nil
	}
	out := make(localSet, len(s))
	for id := range s {
		out.add(id)
	}
	return out
}

// unionSet merges src into dst and returns dst.
func unionSet(dst, src localSet) localSet {
	if dst == nil {
		dst = localSet{}
	}
	for id := range src {
		dst.add(id)
	}
	return dst
}

// subtractSet returns src minus sub.
func subtractSet(src, sub localSet) localSet {
	if len(src) == 0 {
		return nil
	}
	out := localSet{}
	for id := range src {
		if sub.has(id) {
			continue
		}
		out.add(id)
	}
	return out
}

// setEqual checks if two localSets contain the same elements.
func setEqual(a, b localSet) bool {
	if len(a) != len(b) {
		return false
	}
	for id := range a {
		if !b.has(id) {
			return false
		}
	}
	return true
}
