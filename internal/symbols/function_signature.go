package symbols

import (
	"fmt"
	"strings"

	"surge/internal/ast"
)

type TypeKey string

// FunctionSignature captures a simplified view of a function signature.
type FunctionSignature struct {
	Params   []TypeKey
	Variadic []bool
	Result   TypeKey
	HasBody  bool
}

func buildFunctionSignature(builder *ast.Builder, fn *ast.FnItem) *FunctionSignature {
	if builder == nil || fn == nil {
		return nil
	}
	ids := builder.Items.GetFnParamIDs(fn)
	sig := &FunctionSignature{
		Params:   make([]TypeKey, 0, len(ids)),
		Variadic: make([]bool, 0, len(ids)),
		Result:   makeTypeKey(builder, fn.ReturnType),
		HasBody:  fn.Body.IsValid(),
	}
	for _, pid := range ids {
		param := builder.Items.FnParam(pid)
		if param == nil {
			sig.Params = append(sig.Params, TypeKey(""))
			sig.Variadic = append(sig.Variadic, false)
			continue
		}
		sig.Params = append(sig.Params, makeTypeKey(builder, param.Type))
		sig.Variadic = append(sig.Variadic, param.Variadic)
	}
	return sig
}

func makeTypeKey(builder *ast.Builder, typeID ast.TypeID) TypeKey {
	if !typeID.IsValid() || builder == nil {
		return ""
	}
	expr := builder.Types.Get(typeID)
	if expr == nil {
		return TypeKey(fmt.Sprintf("type#%d", typeID))
	}
	switch expr.Kind {
	case ast.TypeExprPath:
		if path, ok := builder.Types.Path(typeID); ok {
			names := make([]string, 0, len(path.Segments))
			for _, seg := range path.Segments {
				name := builder.StringsInterner.MustLookup(seg.Name)
				if len(seg.Generics) > 0 {
					args := make([]string, 0, len(seg.Generics))
					for _, gen := range seg.Generics {
						args = append(args, string(makeTypeKey(builder, gen)))
					}
					name = name + "<" + strings.Join(args, ",") + ">"
				}
				names = append(names, name)
			}
			return TypeKey(strings.Join(names, "::"))
		}
	case ast.TypeExprUnary:
		if unary, ok := builder.Types.UnaryType(typeID); ok {
			inner := string(makeTypeKey(builder, unary.Inner))
			switch unary.Op {
			case ast.TypeUnaryRef:
				return TypeKey("&" + inner)
			case ast.TypeUnaryRefMut:
				return TypeKey("&mut " + inner)
			case ast.TypeUnaryOwn:
				return TypeKey("own " + inner)
			case ast.TypeUnaryPointer:
				return TypeKey("*" + inner)
			}
		}
	case ast.TypeExprFn:
		if fn, ok := builder.Types.Fn(typeID); ok {
			params := make([]string, 0, len(fn.Params))
			for _, p := range fn.Params {
				params = append(params, string(makeTypeKey(builder, p.Type)))
			}
			return TypeKey("fn(" + strings.Join(params, ",") + ")->" + string(makeTypeKey(builder, fn.Return)))
		}
	case ast.TypeExprArray:
		if arr, ok := builder.Types.Array(typeID); ok {
			return TypeKey("[" + string(makeTypeKey(builder, arr.Elem)) + "]")
		}
	case ast.TypeExprTuple:
		if tup, ok := builder.Types.Tuple(typeID); ok {
			elems := make([]string, 0, len(tup.Elems))
			for _, elem := range tup.Elems {
				elems = append(elems, string(makeTypeKey(builder, elem)))
			}
			return TypeKey("(" + strings.Join(elems, ",") + ")")
		}
	case ast.TypeExprOptional:
		if opt, ok := builder.Types.Optional(typeID); ok {
			return TypeKey("Option<" + string(makeTypeKey(builder, opt.Inner)) + ">")
		}
	case ast.TypeExprErrorable:
		if errTy, ok := builder.Types.Errorable(typeID); ok {
			okKey := makeTypeKey(builder, errTy.Inner)
			errKey := makeTypeKey(builder, errTy.Error)
			return TypeKey("Result<" + string(okKey) + "," + string(errKey) + ">")
		}
	}
	return TypeKey(fmt.Sprintf("type#%d", typeID))
}

func signaturesEqual(a, b *FunctionSignature) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Result != b.Result || a.HasBody != b.HasBody {
		return false
	}
	if len(a.Params) != len(b.Params) {
		return false
	}
	for i := range a.Params {
		if a.Params[i] != b.Params[i] || a.Variadic[i] != b.Variadic[i] {
			return false
		}
	}
	return true
}

func signatureDiffersFromAll(sig *FunctionSignature, symbols []*Symbol) bool {
	for _, sym := range symbols {
		if sym == nil {
			continue
		}
		if signaturesEqual(sig, sym.Signature) {
			return false
		}
	}
	return true
}
