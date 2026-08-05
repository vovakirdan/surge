package types //nolint:revive

import (
	"fmt"
	"strconv"
	"strings"

	"surge/internal/source"
)

const canonicalTypeKeyDepthLimit = 256

// NominalIdentityResolver maps a compiler-local declaration span to its stable
// post-merge identity (normally module path + declaration name). FileID alone
// is intentionally insufficient because file discovery order may change it.
type NominalIdentityResolver func(kind Kind, name string, decl source.Span) (string, error)

// TypeParamIdentityResolver maps one exact compiler-local parameter descriptor
// to a stable declaration identity. The TypeID matters while module-local
// owner numbers may overlap after graphs are merged.
type TypeParamIdentityResolver func(id TypeID, info TypeParamInfo) (string, error)

// CanonicalKeyContext owns the shared semantic key algorithm used by generic
// instantiation closure and its downstream mono/callable maps.
type CanonicalKeyContext struct {
	Types            *Interner
	ResolveNominal   NominalIdentityResolver
	ResolveTypeParam TypeParamIdentityResolver
}

// TypeKey returns a deterministic semantic identity for a type.
//
// Unlike compiler-local TypeID text, this key is safe to carry across the
// per-file/module merge: generic parameters name their remapped owner and
// index, nested types are encoded recursively, and nominal identities retain
// their declaration and kind. Distinct aliases are deliberately not folded.
func (c CanonicalKeyContext) TypeKey(id TypeID) (string, error) {
	var out strings.Builder
	if err := appendCanonicalTypeKey(&out, c, id, 0); err != nil {
		return "", err
	}
	return out.String(), nil
}

// TypeArgsKey returns the shared semantic identity for an ordered
// list of template arguments. An empty argument list has the empty key.
func (c CanonicalKeyContext) TypeArgsKey(args []TypeID) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	var out strings.Builder
	for _, arg := range args {
		key, err := c.TypeKey(arg)
		if err != nil {
			return "", err
		}
		appendKeyPart(&out, key)
	}
	return out.String(), nil
}

func appendCanonicalTypeKey(out *strings.Builder, c CanonicalKeyContext, id TypeID, depth int) error {
	in := c.Types
	if in == nil {
		return fmt.Errorf("canonical type key: missing interner")
	}
	if id == NoTypeID {
		return fmt.Errorf("canonical type key: missing type")
	}
	if depth >= canonicalTypeKeyDepthLimit {
		return fmt.Errorf("canonical type key: nesting exceeds %d at type#%d", canonicalTypeKeyDepthLimit, id)
	}
	t, ok := in.Lookup(id)
	if !ok {
		return fmt.Errorf("canonical type key: unknown type#%d", id)
	}

	switch t.Kind {
	case KindInvalid:
		return fmt.Errorf("canonical type key: invalid type#%d", id)
	case KindUnit, KindNothing, KindBool, KindString:
		out.WriteString(t.Kind.String())
	case KindInt, KindUint, KindFloat:
		fmt.Fprintf(out, "%s:%d", t.Kind, t.Width)
	case KindConst:
		fmt.Fprintf(out, "const:%d:%d", t.Width, t.Count)
	case KindGenericParam:
		info, found := in.TypeParamInfo(id)
		if !found || info == nil {
			return fmt.Errorf("canonical type key: generic type#%d has no parameter metadata", id)
		}
		if c.ResolveTypeParam == nil {
			// Legacy/local callers may still inspect compiler-local keys. The
			// post-merge instantiation authority always installs a resolver.
			fmt.Fprintf(out, "param:%d:%d:%t", info.Owner, info.Index, info.IsConst)
		} else {
			identity, err := c.ResolveTypeParam(id, *info)
			if err != nil {
				return fmt.Errorf("canonical type key: resolve parameter owner %d index %d: %w", info.Owner, info.Index, err)
			}
			if identity == "" {
				return fmt.Errorf("canonical type key: empty identity for parameter owner %d index %d", info.Owner, info.Index)
			}
			out.WriteString("param:")
			appendKeyPart(out, identity)
			fmt.Fprintf(out, ":%d:%t", info.Index, info.IsConst)
		}
		if info.IsConst && info.ConstType != NoTypeID {
			out.WriteByte('(')
			if err := appendCanonicalTypeKey(out, c, info.ConstType, depth+1); err != nil {
				return err
			}
			out.WriteByte(')')
		}
	case KindPointer, KindReference, KindOwn, KindFar, KindArray:
		fmt.Fprintf(out, "%s:%d:%t(", t.Kind, t.Count, t.Mutable)
		if err := appendCanonicalTypeKey(out, c, t.Elem, depth+1); err != nil {
			return err
		}
		out.WriteByte(')')
	case KindTuple:
		info, found := in.TupleInfo(id)
		if !found || info == nil {
			return fmt.Errorf("canonical type key: tuple type#%d has no metadata", id)
		}
		out.WriteString("tuple")
		return appendCanonicalTypeList(out, c, info.Elems, depth)
	case KindFn:
		info, found := in.FnInfo(id)
		if !found || info == nil {
			return fmt.Errorf("canonical type key: function type#%d has no metadata", id)
		}
		out.WriteString("fn")
		if err := appendCanonicalTypeList(out, c, info.Params, depth); err != nil {
			return err
		}
		out.WriteString("->")
		return appendCanonicalTypeKey(out, c, info.Result, depth+1)
	case KindStruct:
		info, found := in.StructInfo(id)
		if !found || info == nil {
			return fmt.Errorf("canonical type key: struct type#%d has no metadata", id)
		}
		if err := appendNominalKey(out, c, KindStruct, info.Name, info.Decl); err != nil {
			return err
		}
		return appendCanonicalTypeList(out, c, info.TypeArgs, depth)
	case KindUnion:
		info, found := in.UnionInfo(id)
		if !found || info == nil {
			return fmt.Errorf("canonical type key: union type#%d has no metadata", id)
		}
		if err := appendNominalKey(out, c, KindUnion, info.Name, info.Decl); err != nil {
			return err
		}
		return appendCanonicalTypeList(out, c, info.TypeArgs, depth)
	case KindAlias:
		info, found := in.AliasInfo(id)
		if !found || info == nil {
			return fmt.Errorf("canonical type key: alias type#%d has no metadata", id)
		}
		if err := appendNominalKey(out, c, KindAlias, info.Name, info.Decl); err != nil {
			return err
		}
		return appendCanonicalTypeList(out, c, info.TypeArgs, depth)
	case KindEnum:
		info, found := in.EnumInfo(id)
		if !found || info == nil {
			return fmt.Errorf("canonical type key: enum type#%d has no metadata", id)
		}
		if err := appendNominalKey(out, c, KindEnum, info.Name, info.Decl); err != nil {
			return err
		}
		return appendCanonicalTypeList(out, c, info.TypeArgs, depth)
	default:
		return fmt.Errorf("canonical type key: unsupported kind %s for type#%d", t.Kind, id)
	}
	return nil
}

func appendCanonicalTypeList(out *strings.Builder, c CanonicalKeyContext, ids []TypeID, depth int) error {
	out.WriteByte('[')
	for _, id := range ids {
		var part strings.Builder
		if err := appendCanonicalTypeKey(&part, c, id, depth+1); err != nil {
			return err
		}
		appendKeyPart(out, part.String())
	}
	out.WriteByte(']')
	return nil
}

func appendNominalKey(out *strings.Builder, c CanonicalKeyContext, kind Kind, nameID source.StringID, decl source.Span) error {
	in := c.Types
	if in.Strings == nil {
		return fmt.Errorf("canonical type key: %s has no string interner", kind)
	}
	name, ok := in.Strings.Lookup(nameID)
	if !ok || name == "" {
		return fmt.Errorf("canonical type key: %s has no name", kind)
	}
	if c.ResolveNominal == nil {
		return fmt.Errorf("canonical type key: %s %q has no post-merge nominal identity resolver", kind, name)
	}
	identity, err := c.ResolveNominal(kind, name, decl)
	if err != nil {
		return fmt.Errorf("canonical type key: resolve %s %q: %w", kind, name, err)
	}
	if identity == "" {
		return fmt.Errorf("canonical type key: resolver returned empty identity for %s %q", kind, name)
	}
	out.WriteString(kind.String())
	out.WriteByte(':')
	appendKeyPart(out, identity)
	return nil
}

func appendKeyPart(out *strings.Builder, value string) {
	out.WriteString(strconv.Itoa(len(value)))
	out.WriteByte(':')
	out.WriteString(value)
}
