package sema

import (
	"surge/internal/source"
	"surge/internal/types"
)

// IsCopyType reports whether values of the given type can be implicitly copied,
// matching the semantics used by the type checker (builtin Copy + @copy types).
func (r *Result) IsCopyType(id types.TypeID) bool {
	if r == nil || r.TypeInterner == nil || id == types.NoTypeID {
		return false
	}
	resolved := resolveAlias(r.TypeInterner, id)
	if r.TypeInterner.IsCopy(resolved) {
		return true
	}
	if r.CopyTypes != nil {
		if _, ok := r.CopyTypes[resolved]; ok {
			return true
		}
	}
	return false
}

func resolveAlias(in *types.Interner, id types.TypeID) types.TypeID {
	if in == nil {
		return id
	}
	seen := 0
	for id != types.NoTypeID && seen < 32 {
		tt, ok := in.Lookup(id)
		if !ok || tt.Kind != types.KindAlias {
			return id
		}
		target, ok := in.AliasTarget(id)
		if !ok || target == types.NoTypeID || target == id {
			return id
		}
		id = target
		seen++
	}
	return id
}

// NonCopyCulpritPath names the first non-copy component inside a struct
// payload for diagnostics: a dotted field path like "meta.name", or "" when
// the type itself (or an unnamed component) is the culprit. The strings
// interner renders field names; passing nil degrades to "".
func (r *Result) NonCopyCulpritPath(strings *source.Interner, id types.TypeID) string {
	if r == nil || r.TypeInterner == nil || strings == nil {
		return ""
	}
	var walk func(t types.TypeID, depth int) string
	walk = func(t types.TypeID, depth int) string {
		if depth > 8 || t == types.NoTypeID || r.IsCopyType(t) {
			return ""
		}
		resolved := resolveAlias(r.TypeInterner, t)
		fields := r.TypeInterner.StructFields(resolved)
		for i := range fields {
			if r.IsCopyType(fields[i].Type) {
				continue
			}
			name := strings.MustLookup(fields[i].Name)
			if name == "" {
				return ""
			}
			if inner := walk(fields[i].Type, depth+1); inner != "" {
				return name + "." + inner
			}
			return name
		}
		return ""
	}
	return walk(id, 0)
}
