package sema

import "surge/internal/types"

// A core constructor below returns a fresh opaque handle word: the runtime keeps
// the handle's resources in its own table (rt_net_handles.h:39–46) and never
// stores a caller's borrow in the word. Only the retained core declaration with
// exactly this signature and result structure is believed. A changed signature or
// result structure fails the certificate; re-read the runtime before widening.

type returnOriginFreshParam uint8

const (
	freshRefString returnOriginFreshParam = iota
	freshUint
	freshUint32
	freshRefListener
	freshRefConn
)

type returnOriginFreshHandleRow struct {
	receiver         string
	params           []returnOriginFreshParam
	leaf             string
	wrapped, pointer bool
}

var returnOriginFreshHandleRows = map[string]returnOriginFreshHandleRow{
	"rt_fs_open":     {params: []returnOriginFreshParam{freshRefString, freshUint32}, leaf: "File", wrapped: true},
	"rt_net_listen":  {params: []returnOriginFreshParam{freshRefString, freshUint}, leaf: "TcpListener", wrapped: true},
	"rt_net_connect": {params: []returnOriginFreshParam{freshRefString, freshUint}, leaf: "TcpConn", wrapped: true},
	"rt_net_accept":  {params: []returnOriginFreshParam{freshRefListener}, leaf: "TcpConn", wrapped: true},
	"shard":          {params: []returnOriginFreshParam{freshUint32}, leaf: "Placement"},
	"new":            {receiver: "RwLock", leaf: "RwLock", pointer: true},
}

// A retained body-less core intrinsic whose result may borrow any input.
func returnOriginCoreIntrinsic(fn *returnOriginFunction, templates, receiverArity int) bool {
	return returnOriginCoreIntrinsicDeclaration(fn, templates, receiverArity) && fn.info.ReturnSources().IsAllInputs()
}

// returnOriginFreshHandleResidual answers which parts of a certified constructor's
// result still need the ordinary walk: the error member of a wrapped result.
func returnOriginFreshHandleResidual(fn *returnOriginFunction, result types.TypeID) ([]types.TypeID, bool) {
	if fn == nil {
		return nil, false
	}
	if subjects, fresh := returnOriginFreshContainerResidual(fn, result); fresh {
		return subjects, true
	}
	if subjects, fresh := returnOriginTaskHandleResidual(fn, result); fresh {
		return subjects, true
	}
	if returnOriginFreshRemoteChannel(fn, result) {
		return nil, true
	}
	if returnOriginFreshChannel(fn, result) {
		return nil, true
	}
	row, known := returnOriginFreshHandleRows[fn.name]
	if !known || !returnOriginCoreIntrinsic(fn, 0, 0) || fn.candidate.HasSelf || result != fn.info.Result || !returnOriginFreshParamsMatch(fn, row.params) {
		return nil, false
	}
	if fn.candidate.ReceiverType != types.NoTypeID || row.receiver != "" {
		if row.receiver == "" || !returnOriginFreshHandleLeaf(fn, fn.candidate.ReceiverType, row.receiver, row.pointer) {
			return nil, false
		}
	}
	in := fn.unit.Sema.TypeInterner
	if !row.wrapped {
		return nil, returnOriginFreshHandleLeaf(fn, result, row.leaf, row.pointer)
	}
	info, found := in.UnionInfo(returnOriginResolveAlias(in, result))
	if !found || info == nil || len(info.Members) != 2 {
		return nil, false
	}
	var residual []types.TypeID
	tagged := false
	for _, member := range info.Members {
		switch {
		case !tagged && member.Kind == types.UnionMemberTag && member.Type == types.NoTypeID && len(member.TagArgs) == 1 &&
			returnOriginFreshHandleLeaf(fn, member.TagArgs[0], row.leaf, row.pointer):
			tagged = true
		case residual == nil && member.Kind == types.UnionMemberType && member.Type != types.NoTypeID:
			residual = []types.TypeID{member.Type}
		default:
			return nil, false
		}
	}
	return residual, tagged && residual != nil
}

func returnOriginFreshChannel(fn *returnOriginFunction, result types.TypeID) bool {
	if fn == nil || fn.info == nil || fn.candidate == nil || fn.name != "new" ||
		!returnOriginCoreIntrinsic(fn, 1, 1) || fn.candidate.HasSelf || len(fn.info.Params) != 1 ||
		fn.info.Params[0] != fn.unit.Sema.TypeInterner.Builtins().Uint {
		return false
	}
	in := fn.unit.Sema.TypeInterner
	own, ok := in.Lookup(returnOriginResolveAlias(in, result))
	return ok && own.Kind == types.KindOwn && own.Elem == fn.candidate.ReceiverType &&
		returnOriginChannelLeaf(fn, own.Elem)
}

// channel_on allocates a channel on the destination shard and returns only a
// counted remote lease. The handle word never contains the payload or a caller
// borrow; payload mobility is checked before the CrossingLoweringChannelCreate
// record is published.
func returnOriginFreshRemoteChannel(fn *returnOriginFunction, result types.TypeID) bool {
	if fn == nil || fn.info == nil || fn.candidate == nil || fn.name != "channel_on" ||
		!returnOriginCoreIntrinsic(fn, 1, 0) || fn.candidate.HasSelf ||
		fn.candidate.ReceiverType != types.NoTypeID || len(fn.info.Params) != 2 ||
		fn.info.Params[1] != fn.unit.Sema.TypeInterner.Builtins().Uint || result != fn.info.Result {
		return false
	}
	in := fn.unit.Sema.TypeInterner
	if !returnOriginFreshHandleLeaf(fn, fn.info.Params[0], "Placement", false) {
		return false
	}
	far, ok := in.Lookup(returnOriginResolveAlias(in, result))
	if !ok || far.Kind != types.KindFar {
		return false
	}
	return returnOriginChannelLeaf(fn, far.Elem)
}

func returnOriginChannelLeaf(fn *returnOriginFunction, id types.TypeID) bool {
	in := fn.unit.Sema.TypeInterner
	channel, found := in.StructInfo(id)
	if !found || channel == nil || channel.Decl.File != fn.item.NameSpan.File ||
		len(channel.TypeArgs) != 1 || channel.TypeArgs[0] != fn.candidate.TemplateParams[0] ||
		len(channel.Fields) != 1 || !in.IsRuntimeHandleType(id) || !in.IsRefCountedHandle(id) {
		return false
	}
	name, _ := fn.unit.Builder.StringsInterner.Lookup(channel.Name)
	field, _ := fn.unit.Builder.StringsInterner.Lookup(channel.Fields[0].Name)
	fieldType, typed := in.Lookup(channel.Fields[0].Type)
	return name == "Channel" && field == "__opaque" && typed && fieldType.Kind == types.KindPointer
}

// returnOriginFreshParamsMatch answers whether the formals are exactly these kinds, in order.
func returnOriginFreshParamsMatch(fn *returnOriginFunction, kinds []returnOriginFreshParam) bool {
	if len(fn.info.Params) != len(kinds) {
		return false
	}
	in := fn.unit.Sema.TypeInterner
	for i, kind := range kinds {
		param := fn.info.Params[i]
		typ, typed := in.Lookup(param)
		switch kind {
		case freshRefString, freshRefListener, freshRefConn:
			if !typed || typ.Kind != types.KindReference || typ.Mutable ||
				kind == freshRefString && typ.Elem != in.Builtins().String ||
				kind == freshRefListener && !returnOriginFreshHandleLeaf(fn, typ.Elem, "TcpListener", false) ||
				kind == freshRefConn && !returnOriginFreshHandleLeaf(fn, typ.Elem, "TcpConn", false) {
				return false
			}
		case freshUint:
			if param != in.Builtins().Uint {
				return false
			}
		case freshUint32:
			if returnOriginResolveAlias(in, param) != in.Builtins().Uint32 {
				return false
			}
		}
	}
	return true
}

// The leaf is the core struct of that name holding exactly one opaque word.
func returnOriginFreshHandleLeaf(fn *returnOriginFunction, id types.TypeID, name string, pointer bool) bool {
	in := fn.unit.Sema.TypeInterner
	info, found := in.StructInfo(id)
	if !found || info == nil || info.Decl.File != fn.item.NameSpan.File || len(info.TypeArgs) != 0 || len(info.Fields) != 1 || in.IsRuntimeHandleType(id) {
		return false
	}
	leaf, _ := fn.unit.Builder.StringsInterner.Lookup(info.Name)
	field, _ := fn.unit.Builder.StringsInterner.Lookup(info.Fields[0].Name)
	typ, typed := in.Lookup(info.Fields[0].Type)
	return leaf == name && field == "__opaque" && typed &&
		(pointer && typ.Kind == types.KindPointer || !pointer && info.Fields[0].Type == in.Builtins().Int64)
}
