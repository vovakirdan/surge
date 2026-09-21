package sema

import "surge/internal/types"

// A core reader below returns a fresh array: native code allocates the header and
// the element storage itself and fills them with bytes it read or strings it just
// built (rt_fs.c:307–434, 654–737; rt_net.c:494–523; rt_io.c:99–138), and the VM
// builds a new heap array the same way. Nothing in the array points into an input.
// Only the retained core declaration with exactly this signature and result shape
// is believed; the element and the error member are still walked.

type returnOriginFreshContainerRow struct {
	params  []returnOriginFreshParam
	element string // "string", "uint8", or the name of a core struct
	wrapped bool
}

var returnOriginFreshContainerRows = map[string]returnOriginFreshContainerRow{
	"rt_fs_read_dir":    {params: []returnOriginFreshParam{freshRefString}, element: "DirEntry", wrapped: true},
	"rt_fs_read_file":   {params: []returnOriginFreshParam{freshRefString}, element: "uint8", wrapped: true},
	"rt_net_read_bytes": {params: []returnOriginFreshParam{freshRefConn, freshUint}, element: "uint8", wrapped: true},
	"rt_argv":           {element: "string"},
}

// returnOriginFreshContainerResidual answers which parts of a certified reader's
// result still need the ordinary walk: the element, and a wrapped result's error member.
func returnOriginFreshContainerResidual(fn *returnOriginFunction, result types.TypeID) ([]types.TypeID, bool) {
	if fn == nil {
		return nil, false
	}
	row, known := returnOriginFreshContainerRows[fn.name]
	if !known || !returnOriginCoreIntrinsic(fn, 0, 0) || fn.candidate.HasSelf || fn.candidate.ReceiverType != types.NoTypeID ||
		result != fn.info.Result || !returnOriginFreshParamsMatch(fn, row.params) {
		return nil, false
	}
	if !row.wrapped {
		elem, fresh := returnOriginFreshArray(fn, result, row.element)
		return []types.TypeID{elem}, fresh
	}
	in := fn.unit.Sema.TypeInterner
	info, found := in.UnionInfo(returnOriginResolveAlias(in, result))
	if !found || info == nil || len(info.Members) != 2 {
		return nil, false
	}
	var elem, failure types.TypeID
	for _, member := range info.Members {
		switch {
		case elem == types.NoTypeID && member.Kind == types.UnionMemberTag && member.Type == types.NoTypeID && len(member.TagArgs) == 1:
			var fresh bool
			if elem, fresh = returnOriginFreshArray(fn, member.TagArgs[0], row.element); !fresh {
				return nil, false
			}
		case failure == types.NoTypeID && member.Kind == types.UnionMemberType && member.Type != types.NoTypeID:
			failure = member.Type
		default:
			return nil, false
		}
	}
	return []types.TypeID{elem, failure}, elem != types.NoTypeID && failure != types.NoTypeID
}

// The canonical dynamic Array of the row's exact element; the element is walked later.
func returnOriginFreshArray(fn *returnOriginFunction, id types.TypeID, element string) (types.TypeID, bool) {
	in := fn.unit.Sema.TypeInterner
	array, canonical := returnOriginIndexContainer(in, id)
	if !canonical || array.reference || array.family != in.ArrayNominalType() {
		return types.NoTypeID, false
	}
	switch element {
	case "string":
		return array.element, array.element == in.Builtins().String
	case "uint8":
		return array.element, returnOriginResolveAlias(in, array.element) == in.Builtins().Uint8
	}
	info, found := in.StructInfo(array.element)
	if !found || info == nil || info.Decl.File != fn.item.NameSpan.File || len(info.TypeArgs) != 0 || in.IsRuntimeHandleType(array.element) {
		return types.NoTypeID, false
	}
	name, _ := fn.unit.Builder.StringsInterner.Lookup(info.Name)
	return array.element, name == element
}

// Container results (P1c): fresh storage; E rows name input contents; borrowing
// rows dispatch to return_origin_cursor.go.

// containerDeclaration answers a certified body-less core container declaration by
// the roots its result may hold and the parts that still need NoBorrowedState.
func (a *returnOriginAnalyzer) containerDeclaration(fn *returnOriginFunction, result types.TypeID) (roots []returnOrigin, subjects []types.TypeID, ok bool) {
	if fn == nil || fn.info == nil || result != fn.info.Result {
		return nil, nil, false
	}
	switch op, certified := returnOriginMapIntrinsic(fn); {
	case certified && op == returnOriginMapNew:
		return nil, nil, true // an empty fresh map (rt_map.c:249–278)
	case certified && op == returnOriginMapKeys:
		return nil, []types.TypeID{fn.candidate.TemplateParams[0]}, true // fresh owning key copies (rt_map.c:414–475)
	}
	return nil, nil, false
}
