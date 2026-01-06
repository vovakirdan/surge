package types //nolint:revive

// LayoutAttrs describes layout-affecting attributes applied to a type declaration.
//
// These attributes must be validated by sema; layout computation must not emit diagnostics.
type LayoutAttrs struct {
	Packed        bool
	AlignOverride *int // nil when no @align(N) is present
}

// FieldLayoutAttrs describes layout-affecting attributes applied to a struct field.
type FieldLayoutAttrs struct {
	AlignOverride *int // nil when no @align(N) is present
}

func cloneIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// TypeLayoutAttrs returns the validated layout-affecting attributes recorded for the type.
func (in *Interner) TypeLayoutAttrs(id TypeID) (LayoutAttrs, bool) {
	if in == nil || id == NoTypeID || in.typeLayoutAttrs == nil {
		return LayoutAttrs{}, false
	}
	attrs, ok := in.typeLayoutAttrs[id]
	return attrs, ok
}

// SetTypeLayoutAttrs stores validated layout-affecting attributes for the type.
func (in *Interner) SetTypeLayoutAttrs(id TypeID, attrs LayoutAttrs) {
	if in == nil || id == NoTypeID {
		return
	}
	if !attrs.Packed && attrs.AlignOverride == nil {
		if in.typeLayoutAttrs != nil {
			delete(in.typeLayoutAttrs, id)
		}
		return
	}
	if in.typeLayoutAttrs == nil {
		in.typeLayoutAttrs = make(map[TypeID]LayoutAttrs, 64)
	}
	attrs.AlignOverride = cloneIntPtr(attrs.AlignOverride)
	in.typeLayoutAttrs[id] = attrs
}
