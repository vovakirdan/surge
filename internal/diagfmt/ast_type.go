package diagfmt

import (
	"fmt"
	"strings"
	"surge/internal/ast"
	"surge/internal/source"
)

// formatTypeExprInline renders the type expression identified by typeID in builder into a compact inline string.
// It formats path types (including generic arguments), unary modifiers (own, &, &mut, *), arrays (slices, sized, unknown length), tuples, and function types (named and/or variadic parameters and return type), formatting nested types recursively.
// For missing or invalid metadata the function returns explicit placeholders such as "<inferred>", "<invalid>", "<invalid-path>", "<invalid-unary>", "<invalid-array>", "<invalid-tuple>", "<invalid-fn>", or "<unknown-type>".
func formatTypeExprInline(builder *ast.Builder, typeID ast.TypeID) string {
	if !typeID.IsValid() {
		return "<inferred>"
	}
	if builder == nil || builder.Types == nil {
		return "<invalid>"
	}
	typ := builder.Types.Get(typeID)
	if typ == nil {
		return "<invalid>"
	}

	switch typ.Kind {
	case ast.TypeExprPath:
		path, ok := builder.Types.Path(typeID)
		if !ok {
			return "<invalid-path>"
		}
		segments := make([]string, 0, len(path.Segments))
		for _, seg := range path.Segments {
			name := builder.StringsInterner.MustLookup(seg.Name)
			if len(seg.Generics) > 0 {
				genericStrs := make([]string, 0, len(seg.Generics))
				for _, gid := range seg.Generics {
					genericStrs = append(genericStrs, formatTypeExprInline(builder, gid))
				}
				name = fmt.Sprintf("%s<%s>", name, strings.Join(genericStrs, ", "))
			}
			segments = append(segments, name)
		}
		return strings.Join(segments, ".")
	case ast.TypeExprUnary:
		un, ok := builder.Types.UnaryType(typeID)
		if !ok {
			return "<invalid-unary>"
		}
		op := ""
		switch un.Op {
		case ast.TypeUnaryOwn:
			op = "own "
		case ast.TypeUnaryRef:
			op = "&"
		case ast.TypeUnaryRefMut:
			op = "&mut "
		case ast.TypeUnaryPointer:
			op = "*"
		default:
			op = "<?>"
		}
		return op + formatTypeExprInline(builder, un.Inner)
	case ast.TypeExprArray:
		arr, ok := builder.Types.Array(typeID)
		if !ok {
			return "<invalid-array>"
		}
		elem := formatTypeExprInline(builder, arr.Elem)
		switch arr.Kind {
		case ast.ArraySlice:
			return elem + "[]"
		case ast.ArraySized:
			if arr.HasConstLen {
				return fmt.Sprintf("%s[%d]", elem, arr.ConstLength)
			}
			if arr.Length.IsValid() {
				return fmt.Sprintf("%s[%s]", elem, formatExprInline(builder, arr.Length))
			}
			return fmt.Sprintf("%s[?]", elem)
		default:
			return fmt.Sprintf("%s[<?>]", elem)
		}
	case ast.TypeExprConst:
		if c, ok := builder.Types.Const(typeID); ok && c != nil {
			return builder.StringsInterner.MustLookup(c.Value)
		}
		return "<const>"
	case ast.TypeExprTuple:
		tuple, ok := builder.Types.Tuple(typeID)
		if !ok {
			return "<invalid-tuple>"
		}
		if len(tuple.Elems) == 0 {
			return "()"
		}
		elems := make([]string, 0, len(tuple.Elems))
		for _, elem := range tuple.Elems {
			elems = append(elems, formatTypeExprInline(builder, elem))
		}
		return fmt.Sprintf("(%s)", strings.Join(elems, ", "))
	case ast.TypeExprFn:
		fn, ok := builder.Types.Fn(typeID)
		if !ok {
			return "<invalid-fn>"
		}
		paramStrs := make([]string, 0, len(fn.Params))
		for _, param := range fn.Params {
			paramType := formatTypeExprInline(builder, param.Type)
			if param.Variadic {
				paramType = "..." + paramType
			}
			if param.Name != source.NoStringID {
				name := builder.StringsInterner.MustLookup(param.Name)
				paramType = fmt.Sprintf("%s: %s", name, paramType)
			}
			paramStrs = append(paramStrs, paramType)
		}
		ret := formatTypeExprInline(builder, fn.Return)
		return fmt.Sprintf("fn(%s) -> %s", strings.Join(paramStrs, ", "), ret)
	default:
		return "<unknown-type>"
	}
}
