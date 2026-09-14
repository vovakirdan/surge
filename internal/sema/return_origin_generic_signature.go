package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/types"
)

// A view retains existing descriptors, not a manufactured concrete FnInfo.
// A borrowed formal can keep its original outer reference descriptor while
// effects inspect the actual operand's content after the signature is proved.
type returnOriginSignature struct {
	params  []types.TypeID
	effects []types.TypeID
	result  types.TypeID
}

func returnOriginSignatureTypes(info *types.FnInfo, view ...*returnOriginSignature) ([]types.TypeID, types.TypeID) {
	if len(view) != 0 && view[0] != nil {
		return view[0].params, view[0].result
	}
	return info.Params, info.Result
}

// matchReturnOriginSourceType substitutes only exact original parameters while
// comparing existing descriptors. It never interns missing wrapper instances.
func matchReturnOriginSourceType(in *types.Interner, original, actual types.TypeID, params, args []types.TypeID) string {
	if in == nil || len(params) != len(args) {
		return "source signature has inconsistent original bindings"
	}
	for i, param := range params {
		if typ, present := in.TypeParamInfo(param); !present || typ == nil || slices.Contains(params[:i], param) {
			return "source signature lacks unique original parameters"
		}
		if _, present := in.Lookup(args[i]); !present {
			return "source signature lacks existing argument descriptors"
		}
	}
	var match func(types.TypeID, types.TypeID, bool, int) bool
	match = func(left, right types.TypeID, bind bool, depth int) bool {
		if depth > 100 || left == types.NoTypeID || right == types.NoTypeID {
			return false
		}
		if bind {
			if slot := slices.Index(params, left); slot >= 0 {
				return match(args[slot], right, false, depth+1)
			}
		}
		l, lok := in.Lookup(left)
		r, rok := in.Lookup(right)
		if !lok || !rok || l.Kind != r.Kind || l.Kind == types.KindInvalid {
			return false
		}
		if left == right && (!bind || !types.ContainsGenericParam(in, left)) {
			return true
		}
		children := func(a, b []types.TypeID) bool {
			if len(a) != len(b) {
				return false
			}
			for i := range a {
				if !match(a[i], b[i], bind, depth+1) {
					return false
				}
			}
			return true
		}
		switch l.Kind {
		case types.KindGenericParam:
			return left == right
		case types.KindArray, types.KindPointer, types.KindReference, types.KindOwn, types.KindFar:
			return l.Mutable == r.Mutable && l.Count == r.Count && match(l.Elem, r.Elem, bind, depth+1)
		case types.KindStruct:
			a, aok := in.StructInfo(left)
			b, bok := in.StructInfo(right)
			return aok && bok && a != nil && b != nil && a.Name == b.Name && a.Decl == b.Decl &&
				slices.Equal(a.ValueArgs, b.ValueArgs) && children(a.TypeArgs, b.TypeArgs)
		case types.KindAlias:
			a, aok := in.AliasInfo(left)
			b, bok := in.AliasInfo(right)
			return aok && bok && a != nil && b != nil && a.Name == b.Name && a.Decl == b.Decl &&
				children(a.TypeArgs, b.TypeArgs) && match(a.Target, b.Target, bind, depth+1)
		case types.KindUnion:
			a, aok := in.UnionInfo(left)
			b, bok := in.UnionInfo(right)
			if !aok || !bok || a == nil || b == nil || a.Name != b.Name || a.Decl != b.Decl ||
				len(a.Members) != len(b.Members) || !children(a.TypeArgs, b.TypeArgs) {
				return false
			}
			for i, member := range a.Members {
				other := b.Members[i]
				if member.Kind != other.Kind || member.TagName != other.TagName || !children(member.TagArgs, other.TagArgs) ||
					(member.Type != types.NoTypeID || other.Type != types.NoTypeID) && !match(member.Type, other.Type, bind, depth+1) {
					return false
				}
			}
			return true
		case types.KindEnum:
			a, aok := in.EnumInfo(left)
			b, bok := in.EnumInfo(right)
			return aok && bok && a != nil && b != nil && a.Name == b.Name && a.Decl == b.Decl &&
				children(a.TypeArgs, b.TypeArgs) && match(a.BaseType, b.BaseType, bind, depth+1)
		case types.KindTuple:
			a, aok := in.TupleInfo(left)
			b, bok := in.TupleInfo(right)
			return aok && bok && a != nil && b != nil && children(a.Elems, b.Elems)
		case types.KindFn:
			a, aok := in.FnInfo(left)
			b, bok := in.FnInfo(right)
			return aok && bok && a != nil && b != nil && a.ReturnSources().Equal(b.ReturnSources()) &&
				children(a.Params, b.Params) && match(a.Result, b.Result, bind, depth+1)
		default:
			return l == r
		}
	}
	if !match(original, actual, true, 0) {
		return "existing descriptor disagrees with its substituted source signature"
	}
	return ""
}

func returnOriginBoundType(id types.TypeID, params, args []types.TypeID) types.TypeID {
	if slot := slices.Index(params, id); slot >= 0 && slot < len(args) {
		return args[slot]
	}
	return id
}

// Outer conversions follow admitted checker rules. A stored reference borrow
// still needs the FromExpr certificate; the flow reader supplies current storage.
func (u *returnOriginUnitIndex) originalArgumentType(expr ast.ExprID, original types.TypeID, params, args []types.TypeID) (types.TypeID, string) {
	in := u.Sema.TypeInterner
	actual := u.Sema.ExprTypes[expr]
	formal := returnOriginBoundType(original, params, args)
	f, fok := in.Lookup(formal)
	a, aok := in.Lookup(actual)
	originType := actual
	if fok && f.Kind == types.KindReference {
		originType = formal
	}
	if matchReturnOriginSourceType(in, original, actual, params, args) == "" {
		return originType, ""
	}
	if !fok || !aok {
		return types.NoTypeID, "generic original call argument lacks its typed descriptor"
	}
	match := func(left, right types.TypeID) bool {
		if slices.Contains(params, original) {
			return matchReturnOriginSourceType(in, left, right, nil, nil) == ""
		}
		return matchReturnOriginSourceType(in, left, right, params, args) == ""
	}
	if f.Kind == types.KindReference {
		if a.Kind == types.KindReference {
			if !f.Mutable && a.Mutable && match(f.Elem, a.Elem) {
				return formal, ""
			}
		} else if match(f.Elem, actual) {
			kind, _ := returnOriginFormalBorrowKind(in, formal)
			var evidence *BorrowInfo
			for i := range u.Sema.Borrows {
				borrow := &u.Sema.Borrows[i]
				if borrow.Life.FromExpr == expr {
					if evidence != nil {
						return types.NoTypeID, "generic original call has ambiguous implicit borrow evidence"
					}
					evidence = borrow
				}
			}
			if evidence != nil && evidence.ID != NoBorrowID && evidence.Kind == kind && !evidence.Reserved && evidence.Place.IsValid() {
				return formal, ""
			}
			return types.NoTypeID, "generic original call lacks its admitted implicit borrow evidence"
		}
	}
	if f.Kind == types.KindOwn && a.Kind != types.KindOwn && u.Sema.IsCopyType(actual) && match(f.Elem, actual) {
		return actual, ""
	}
	if a.Kind == types.KindOwn && match(formal, a.Elem) {
		return a.Elem, ""
	}
	return types.NoTypeID, "generic original call argument disagrees with its substituted source signature"
}
