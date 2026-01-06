package types //nolint:revive

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
	param = in.RegisterTypeParam(paramName, owner, 0, false, NoTypeID)
	base = in.RegisterStruct(arrayName, decl)
	in.SetStructTypeParams(base, []TypeID{param})
	in.arrayType = base
	in.arrayParam = param
	return base, param
}

// EnsureArrayFixedNominal registers the built-in ArrayFixed<T, N> nominal type if needed.
func (in *Interner) EnsureArrayFixedNominal(name, elemParam, lenParam source.StringID, decl source.Span, owner uint32, constType TypeID) (base TypeID, params [2]TypeID) {
	if name == source.NoStringID || elemParam == source.NoStringID || lenParam == source.NoStringID {
		return NoTypeID, params
	}
	if in.arrayFixedType != NoTypeID {
		if info, ok := in.StructInfo(in.arrayFixedType); ok && info != nil && len(info.TypeParams) == 0 && in.arrayFixedParams[0] != NoTypeID && in.arrayFixedParams[1] != NoTypeID {
			in.SetStructTypeParams(in.arrayFixedType, []TypeID{in.arrayFixedParams[0], in.arrayFixedParams[1]})
		}
		return in.arrayFixedType, in.arrayFixedParams
	}
	params[0] = in.RegisterTypeParam(elemParam, owner, 0, false, NoTypeID)
	params[1] = in.RegisterTypeParam(lenParam, owner, 1, true, constType)
	base = in.RegisterStruct(name, decl)
	in.SetStructTypeParams(base, []TypeID{params[0], params[1]})
	in.arrayFixedType = base
	in.arrayFixedParams = params
	return base, params
}
