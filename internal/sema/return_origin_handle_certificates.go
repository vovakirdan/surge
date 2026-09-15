package sema

import (
	"slices"

	"surge/internal/types"
)

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

// A retained body-less core intrinsic, by its original declaration and publication.
// A name alone selects nothing.
func returnOriginCoreIntrinsic(fn *returnOriginFunction, templates, receiverArity int) bool {
	if fn == nil || fn.info == nil || fn.item == nil || fn.candidate == nil {
		return false
	}
	c, u := fn.candidate, fn.unit
	if !c.Builtin || !c.Intrinsic || c.HasBody || c.Async || fn.item.Body.IsValid() || c.Name != fn.name ||
		c.SourceKey != "builtin" || c.ModulePath != "core/intrinsics" || u.SourceKey != "core/intrinsics.sg" ||
		len(c.TemplateParams) != templates || c.ReceiverTemplateArity != receiverArity ||
		len(c.Defaults) != len(fn.info.Params) || len(c.Variadic) != len(fn.info.Params) ||
		slices.Contains(c.Defaults, true) || slices.Contains(c.Variadic, true) || !fn.info.ReturnSources().IsAllInputs() {
		return false
	}
	identity, err := u.owningCallableIdentity(fn.item, fn.symbol, fn.info)
	return err == nil && identity.BodyKey == fn.key && identity.SourceKey == fn.canonicalSourceKey
}

// returnOriginFreshHandleResidual answers which parts of a certified constructor's
// result still need the ordinary walk: the error member of a wrapped result.
func returnOriginFreshHandleResidual(fn *returnOriginFunction, result types.TypeID) ([]types.TypeID, bool) {
	if fn == nil {
		return nil, false
	}
	row, known := returnOriginFreshHandleRows[fn.name]
	if !known || !returnOriginCoreIntrinsic(fn, 0, 0) || fn.candidate.HasSelf || result != fn.info.Result || len(fn.info.Params) != len(row.params) {
		return nil, false
	}
	if fn.candidate.ReceiverType != types.NoTypeID || row.receiver != "" {
		if row.receiver == "" || !returnOriginFreshHandleLeaf(fn, fn.candidate.ReceiverType, row.receiver, row.pointer) {
			return nil, false
		}
	}
	in := fn.unit.Sema.TypeInterner
	for i, kind := range row.params {
		param := fn.info.Params[i]
		typ, typed := in.Lookup(param)
		switch kind {
		case freshRefString, freshRefListener:
			if !typed || typ.Kind != types.KindReference || typ.Mutable ||
				kind == freshRefString && typ.Elem != in.Builtins().String ||
				kind == freshRefListener && !returnOriginFreshHandleLeaf(fn, typ.Elem, "TcpListener", false) {
				return nil, false
			}
		case freshUint:
			if param != in.Builtins().Uint {
				return nil, false
			}
		case freshUint32:
			if returnOriginResolveAlias(in, param) != in.Builtins().Uint32 {
				return nil, false
			}
		}
	}
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
