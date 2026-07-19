package hir

import (
	"fmt"

	"surge/internal/types"
)

func (p *Printer) typeStr(id types.TypeID) string {
	if id == types.NoTypeID {
		return "?"
	}
	if p.interner == nil {
		return fmt.Sprintf("type#%d", id)
	}
	t, ok := p.interner.Lookup(id)
	if !ok {
		return fmt.Sprintf("type#%d", id)
	}
	return p.formatType(t, id)
}

func (p *Printer) formatType(t types.Type, id types.TypeID) string {
	switch t.Kind {
	case types.KindUnit:
		return "()"
	case types.KindNothing:
		return "nothing"
	case types.KindBool:
		return "bool"
	case types.KindString:
		return "string"
	case types.KindInt:
		return p.formatIntType(t.Width, true)
	case types.KindUint:
		return p.formatIntType(t.Width, false)
	case types.KindFloat:
		return p.formatFloatType(t.Width)
	case types.KindPointer:
		return fmt.Sprintf("*%s", p.typeStr(t.Elem))
	case types.KindReference:
		if t.Mutable {
			return fmt.Sprintf("&mut %s", p.typeStr(t.Elem))
		}
		return fmt.Sprintf("&%s", p.typeStr(t.Elem))
	case types.KindOwn:
		return fmt.Sprintf("own %s", p.typeStr(t.Elem))
	case types.KindFar:
		return fmt.Sprintf("far %s", p.typeStr(t.Elem))
	case types.KindArray:
		if t.Count == types.ArrayDynamicLength {
			return fmt.Sprintf("[%s]", p.typeStr(t.Elem))
		}
		return fmt.Sprintf("[%s; %d]", p.typeStr(t.Elem), t.Count)
	case types.KindStruct, types.KindAlias, types.KindUnion, types.KindEnum, types.KindTuple, types.KindFn:
		// For nominal types, we would need more metadata to print the actual name
		return fmt.Sprintf("type#%d", id)
	default:
		return fmt.Sprintf("type#%d", id)
	}
}

func (p *Printer) formatIntType(width types.Width, signed bool) string {
	prefix := "int"
	if !signed {
		prefix = "uint"
	}
	switch width {
	case types.WidthAny:
		return prefix
	case types.Width8:
		return prefix + "8"
	case types.Width16:
		return prefix + "16"
	case types.Width32:
		return prefix + "32"
	case types.Width64:
		return prefix + "64"
	default:
		return prefix
	}
}

func (p *Printer) formatFloatType(width types.Width) string {
	switch width {
	case types.WidthAny:
		return "float"
	case types.Width16:
		return "float16"
	case types.Width32:
		return "float32"
	case types.Width64:
		return "float64"
	default:
		return "float"
	}
}
