package symbols

import (
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
)

type TypeKey string

// FunctionSignature captures a simplified view of a function signature.
type FunctionSignature struct {
	Params     []TypeKey
	ParamNames []source.StringID // Parameter names (for named arguments)
	Variadic   []bool
	Defaults   []bool // true if parameter has default value
	AllowTo    []bool // true if parameter allows implicit __to conversion
	Result     TypeKey
	HasBody    bool
	HasSelf    bool
}

func buildFunctionSignature(builder *ast.Builder, fn *ast.FnItem) *FunctionSignature {
	if builder == nil || fn == nil {
		return nil
	}
	ids := builder.Items.GetFnParamIDs(fn)
	resultKey := makeTypeKey(builder, fn.ReturnType)
	if fn.Flags&ast.FnModifierAsync != 0 {
		if resultKey == "" {
			resultKey = "Task<nothing>"
		} else {
			resultKey = TypeKey("Task<" + string(resultKey) + ">")
		}
	}
	sig := &FunctionSignature{
		Params:     make([]TypeKey, 0, len(ids)),
		ParamNames: make([]source.StringID, 0, len(ids)),
		Variadic:   make([]bool, 0, len(ids)),
		Defaults:   make([]bool, 0, len(ids)),
		AllowTo:    make([]bool, 0, len(ids)),
		Result:     resultKey,
		HasBody:    fn.Body.IsValid(),
		HasSelf:    false,
	}
	for i, pid := range ids {
		param := builder.Items.FnParam(pid)
		if param == nil {
			sig.Params = append(sig.Params, TypeKey(""))
			sig.ParamNames = append(sig.ParamNames, source.NoStringID)
			sig.Variadic = append(sig.Variadic, false)
			sig.Defaults = append(sig.Defaults, false)
			sig.AllowTo = append(sig.AllowTo, false)
			continue
		}
		if i == 0 && param.Name != source.NoStringID {
			if builder.StringsInterner.MustLookup(param.Name) == "self" {
				sig.HasSelf = true
			}
		}
		allowTo := false
		if param.AttrCount > 0 && param.AttrStart.IsValid() {
			attrs := builder.Items.CollectAttrs(param.AttrStart, param.AttrCount)
			for _, attr := range attrs {
				name := builder.StringsInterner.MustLookup(attr.Name)
				if strings.EqualFold(name, "allow_to") {
					allowTo = true
					break
				}
			}
		}
		sig.Params = append(sig.Params, makeTypeKey(builder, param.Type))
		sig.ParamNames = append(sig.ParamNames, param.Name)
		sig.Variadic = append(sig.Variadic, param.Variadic)
		sig.Defaults = append(sig.Defaults, param.Default != ast.NoExprID)
		sig.AllowTo = append(sig.AllowTo, allowTo)
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
	case ast.TypeExprConst:
		if c, ok := builder.Types.Const(typeID); ok && c != nil {
			return TypeKey(builder.StringsInterner.MustLookup(c.Value))
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
	if a.Result != b.Result {
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
