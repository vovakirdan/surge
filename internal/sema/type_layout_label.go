package sema

import (
	"fmt"
	"strings"

	"surge/internal/source"
	"surge/internal/types"
)

func (tc *typeChecker) layoutSubstitute(
	id types.TypeID,
	substitutions map[types.TypeID]types.TypeID,
) types.TypeID {
	seen := make(map[types.TypeID]struct{}, 4)
	for id != types.NoTypeID {
		if _, cycle := seen[id]; cycle {
			return id
		}
		seen[id] = struct{}{}
		next, ok := substitutions[id]
		if !ok || next == types.NoTypeID || next == id {
			return id
		}
		id = next
	}
	return id
}

func (tc *typeChecker) layoutTypeLabel(
	id types.TypeID,
	substitutions map[types.TypeID]types.TypeID,
) string {
	if len(substitutions) == 0 {
		return tc.typeLabel(id)
	}
	return tc.layoutTypeLabelDepth(id, substitutions, 0)
}

func (tc *typeChecker) layoutTypeLabelDepth(
	id types.TypeID,
	substitutions map[types.TypeID]types.TypeID,
	depth int,
) string { //nolint:gocyclo
	if depth > 8 {
		return "..."
	}
	id = tc.layoutSubstitute(id, substitutions)
	if id == types.NoTypeID || tc.types == nil {
		return "unknown"
	}
	t, ok := tc.types.Lookup(id)
	if !ok {
		return "unknown"
	}
	label := func(child types.TypeID) string {
		return tc.layoutTypeLabelDepth(child, substitutions, depth+1)
	}
	if elem, length, fixed := tc.arrayFixedInfo(id); fixed && t.Kind != types.KindAlias {
		return fmt.Sprintf("[%s; %d]", label(elem), length)
	}
	if elem, array := tc.arrayElemType(id); array && t.Kind != types.KindAlias {
		return fmt.Sprintf("[%s]", label(elem))
	}
	switch t.Kind {
	case types.KindBool:
		return "bool"
	case types.KindInt:
		return numericTypeLabel("int", t.Width)
	case types.KindUint:
		return numericTypeLabel("uint", t.Width)
	case types.KindFloat:
		return numericTypeLabel("float", t.Width)
	case types.KindString:
		return "string"
	case types.KindNothing:
		return "nothing"
	case types.KindGenericParam:
		if info, infoOK := tc.types.TypeParamInfo(id); infoOK && info != nil {
			if name := tc.lookupName(info.Name); name != "" {
				return name
			}
		}
		return "T"
	case types.KindConst:
		return fmt.Sprintf("%d", t.Count)
	case types.KindUnit:
		return "unit"
	case types.KindReference:
		prefix := "&"
		if t.Mutable {
			prefix = "&mut "
		}
		return prefix + label(t.Elem)
	case types.KindPointer:
		return "*" + label(t.Elem)
	case types.KindOwn:
		return "own " + label(t.Elem)
	case types.KindFar:
		return "far " + label(t.Elem)
	case types.KindStruct:
		if info, infoOK := tc.types.StructInfo(id); infoOK && info != nil {
			return tc.layoutNominalLabel(id, info.Name, info.TypeArgs, substitutions, depth)
		}
		return "struct"
	case types.KindAlias:
		if info, infoOK := tc.types.AliasInfo(id); infoOK && info != nil {
			if name := tc.layoutNominalLabel(id, info.Name, info.TypeArgs, substitutions, depth); name != "" {
				return name
			}
			return label(info.Target)
		}
		return "alias"
	case types.KindUnion:
		if info, infoOK := tc.types.UnionInfo(id); infoOK && info != nil {
			return tc.layoutNominalLabel(id, info.Name, info.TypeArgs, substitutions, depth)
		}
		return "union"
	case types.KindEnum:
		if info, infoOK := tc.types.EnumInfo(id); infoOK && info != nil {
			return tc.layoutNominalLabel(id, info.Name, info.TypeArgs, substitutions, depth)
		}
		return "enum"
	case types.KindTuple:
		if info, infoOK := tc.types.TupleInfo(id); infoOK && info != nil {
			parts := make([]string, len(info.Elems))
			for i, elem := range info.Elems {
				parts[i] = label(elem)
			}
			if len(parts) == 1 {
				return "(" + parts[0] + ",)"
			}
			return "(" + strings.Join(parts, ", ") + ")"
		}
		return "()"
	case types.KindFn:
		if info, infoOK := tc.types.FnInfo(id); infoOK && info != nil {
			params := make([]string, len(info.Params))
			for i, param := range info.Params {
				params[i] = label(param)
			}
			result := label(info.Result)
			if result == "()" || result == "unit" {
				return fmt.Sprintf("fn(%s)", strings.Join(params, ", "))
			}
			return fmt.Sprintf("fn(%s) -> %s", strings.Join(params, ", "), result)
		}
		return "fn"
	default:
		return t.Kind.String()
	}
}

func (tc *typeChecker) layoutNominalLabel(
	id types.TypeID,
	nameID source.StringID,
	args []types.TypeID,
	substitutions map[types.TypeID]types.TypeID,
	depth int,
) string {
	name := tc.lookupTypeName(id, nameID)
	if name == "" {
		return ""
	}
	if len(args) == 0 {
		return name
	}
	labels := make([]string, len(args))
	for i, arg := range args {
		labels[i] = tc.layoutTypeLabelDepth(arg, substitutions, depth+1)
	}
	return fmt.Sprintf("%s<%s>", name, strings.Join(labels, ", "))
}
