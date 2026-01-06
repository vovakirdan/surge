package types //nolint:revive

import "surge/internal/source"

// EnsureMapNominal registers the built-in Map<K, V> nominal type if needed and
// returns its base type along with the generic parameters.
func (in *Interner) EnsureMapNominal(name, keyParam, valueParam source.StringID, decl source.Span, owner uint32) (base TypeID, params [2]TypeID) {
	if name == source.NoStringID || keyParam == source.NoStringID || valueParam == source.NoStringID {
		return NoTypeID, params
	}
	if in.mapType != NoTypeID {
		if info, ok := in.StructInfo(in.mapType); ok && info != nil && len(info.TypeParams) == 0 && in.mapParams[0] != NoTypeID && in.mapParams[1] != NoTypeID {
			in.SetStructTypeParams(in.mapType, []TypeID{in.mapParams[0], in.mapParams[1]})
		}
		return in.mapType, in.mapParams
	}
	params[0] = in.RegisterTypeParam(keyParam, owner, 0, false, NoTypeID)
	params[1] = in.RegisterTypeParam(valueParam, owner, 1, false, NoTypeID)
	base = in.RegisterStruct(name, decl)
	in.SetStructTypeParams(base, []TypeID{params[0], params[1]})
	in.mapType = base
	in.mapParams = params
	return base, params
}
