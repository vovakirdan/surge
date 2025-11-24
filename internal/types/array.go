package types

import "surge/internal/source"

// EnsureArrayNominal registers the built-in Array<T> nominal type if needed and
// returns its base type along with the single generic parameter.
func (in *Interner) EnsureArrayNominal(arrayName, paramName source.StringID, decl source.Span, owner uint32) (base, param TypeID) {
	if arrayName == source.NoStringID || paramName == source.NoStringID {
		return NoTypeID, NoTypeID
	}
	if in.arrayType != NoTypeID {
		if info, ok := in.StructInfo(in.arrayType); ok && info != nil && len(info.TypeParams) == 0 && in.arrayParam != NoTypeID {
			in.SetStructTypeParams(in.arrayType, []TypeID{in.arrayParam})
		}
		return in.arrayType, in.arrayParam
	}
	param = in.RegisterTypeParam(paramName, owner, 0)
	base = in.RegisterStruct(arrayName, decl)
	in.SetStructTypeParams(base, []TypeID{param})
	in.arrayType = base
	in.arrayParam = param
	return base, param
}
