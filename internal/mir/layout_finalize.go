package mir

import (
	"fmt"
	"sort"

	"surge/internal/layout"
	"surge/internal/symbols"
	"surge/internal/types"
)

type layoutRootCollector struct {
	types         *types.Interner
	builder       *layout.LayoutEngine
	exactSeen     map[types.TypeID]struct{}
	canonicalSeen map[types.TypeID]struct{}
	roots         []types.TypeID
}

// FinalizeModuleMeta collects every final-MIR type root in deterministic order,
// computes checked physical layouts, and publishes one frozen registry.
func FinalizeModuleMeta(m *Module, typesIn *types.Interner, target layout.Target) error {
	if m == nil {
		return fmt.Errorf("mir: cannot finalize layout metadata for a nil module")
	}
	if typesIn == nil {
		return fmt.Errorf("mir: layout metadata finalization requires a type interner")
	}
	if m.Meta == nil {
		m.Meta = &ModuleMeta{}
	}
	builder := layout.New(target, typesIn)
	roots, err := collectLayoutRoots(m, typesIn, builder)
	if err != nil {
		return err
	}
	registry, err := layout.FinalizeRegistry(builder, roots)
	if err != nil {
		return fmt.Errorf("mir: finalize physical layouts: %w", err)
	}
	m.Meta.Layouts = registry
	return nil
}

func collectLayoutRoots(m *Module, typesIn *types.Interner, builder *layout.LayoutEngine) ([]types.TypeID, error) {
	collector := &layoutRootCollector{
		types:         typesIn,
		builder:       builder,
		exactSeen:     make(map[types.TypeID]struct{}, 256),
		canonicalSeen: make(map[types.TypeID]struct{}, 256),
	}
	for i := range m.Globals {
		if err := collector.addType(m.Globals[i].Type); err != nil {
			return nil, fmt.Errorf("mir: layout root global[%d]: %w", i, err)
		}
	}
	for _, id := range m.SortedFuncIDs() {
		fn := m.Funcs[id]
		if fn == nil {
			continue
		}
		if err := collector.walkFunc(fn); err != nil {
			return nil, fmt.Errorf("mir: layout roots function %s: %w", fn.Name, err)
		}
	}
	if m.Meta != nil {
		syms := make([]symbols.SymbolID, 0, len(m.Meta.FuncTypeArgs))
		for sym := range m.Meta.FuncTypeArgs {
			syms = append(syms, sym)
		}
		sort.Slice(syms, func(i, j int) bool { return syms[i] < syms[j] })
		for _, sym := range syms {
			for _, id := range m.Meta.FuncTypeArgs[sym] {
				if err := collector.addFunctionTypeArg(id); err != nil {
					return nil, fmt.Errorf("mir: layout root function type args sym#%d: %w", sym, err)
				}
			}
		}

		tagTypes := make([]types.TypeID, 0, len(m.Meta.TagLayouts))
		for id := range m.Meta.TagLayouts {
			tagTypes = append(tagTypes, id)
		}
		sort.Slice(tagTypes, func(i, j int) bool { return tagTypes[i] < tagTypes[j] })
		for _, id := range tagTypes {
			if err := collector.addType(id); err != nil {
				return nil, fmt.Errorf("mir: layout root tag type#%d: %w", id, err)
			}
			for _, tagCase := range m.Meta.TagLayouts[id] {
				for _, payload := range tagCase.PayloadTypes {
					if err := collector.addType(payload); err != nil {
						return nil, fmt.Errorf("mir: layout root tag payload type#%d: %w", payload, err)
					}
				}
			}
		}
	}
	return append([]types.TypeID(nil), collector.roots...), nil
}

func (c *layoutRootCollector) addFunctionTypeArg(id types.TypeID) error {
	if id == types.NoTypeID {
		return nil
	}
	canonical, err := c.builder.CanonicalType(id)
	if err != nil {
		return err
	}
	t, ok := c.types.Lookup(canonical)
	if !ok {
		return fmt.Errorf("unknown type#%d", canonical)
	}
	if t.Kind == types.KindConst {
		return nil
	}
	return c.addType(id)
}

func (c *layoutRootCollector) addType(id types.TypeID) error {
	if id == types.NoTypeID {
		return nil
	}
	if _, ok := c.exactSeen[id]; !ok {
		c.exactSeen[id] = struct{}{}
		c.roots = append(c.roots, id)
	}
	canonical, err := c.builder.CanonicalType(id)
	if err != nil {
		return err
	}
	if _, ok := c.canonicalSeen[canonical]; ok {
		return nil
	}
	c.canonicalSeen[canonical] = struct{}{}
	t, ok := c.types.Lookup(canonical)
	if !ok {
		return fmt.Errorf("unknown type#%d", canonical)
	}
	if payloads, handleBacked := c.types.RuntimeHandlePayloads(canonical); handleBacked {
		for _, payload := range payloads {
			if err := c.addType(payload); err != nil {
				return err
			}
		}
		return nil
	}

	switch t.Kind {
	case types.KindStruct:
		if elem, _, ok := c.types.ArrayFixedInfo(canonical); ok {
			return c.addType(elem)
		}
		info, ok := c.types.StructInfo(canonical)
		if !ok || info == nil {
			return fmt.Errorf("type#%d missing struct metadata", canonical)
		}
		for _, field := range info.Fields {
			if err := c.addType(field.Type); err != nil {
				return err
			}
		}
	case types.KindTuple:
		info, ok := c.types.TupleInfo(canonical)
		if !ok || info == nil {
			return fmt.Errorf("type#%d missing tuple metadata", canonical)
		}
		for _, elem := range info.Elems {
			if err := c.addType(elem); err != nil {
				return err
			}
		}
	case types.KindUnion:
		info, ok := c.types.UnionInfo(canonical)
		if !ok || info == nil {
			return fmt.Errorf("type#%d missing union metadata", canonical)
		}
		for _, member := range info.Members {
			switch member.Kind {
			case types.UnionMemberType:
				if err := c.addType(member.Type); err != nil {
					return err
				}
			case types.UnionMemberTag:
				for _, arg := range member.TagArgs {
					if err := c.addType(arg); err != nil {
						return err
					}
				}
			case types.UnionMemberNothing:
			default:
				return fmt.Errorf("type#%d has unsupported union member kind %d", canonical, member.Kind)
			}
		}
	case types.KindEnum:
		info, ok := c.types.EnumInfo(canonical)
		if !ok || info == nil {
			return fmt.Errorf("type#%d missing enum metadata", canonical)
		}
		if err := c.addType(info.BaseType); err != nil {
			return err
		}
	case types.KindFn, types.KindPointer, types.KindReference, types.KindFar:
		// Opaque physical boundaries. Their pointee/signature children do not
		// influence the handle layout and may remain generic indefinitely.
	case types.KindArray:
		if err := c.addType(t.Elem); err != nil {
			return err
		}
	case types.KindUnit, types.KindNothing, types.KindBool, types.KindString,
		types.KindInt, types.KindUint, types.KindFloat, types.KindGenericParam, types.KindConst:
	case types.KindInvalid, types.KindAlias, types.KindOwn:
		return fmt.Errorf("unexpected canonical type kind %s for type#%d", t.Kind, canonical)
	default:
		return fmt.Errorf("unsupported type kind %d for type#%d", t.Kind, canonical)
	}
	return nil
}

func (c *layoutRootCollector) walkFunc(fn *Func) error {
	if err := c.addType(fn.Result); err != nil {
		return err
	}
	for i := range fn.Locals {
		if err := c.addType(fn.Locals[i].Type); err != nil {
			return fmt.Errorf("local[%d]: %w", i, err)
		}
	}
	for bi := range fn.Blocks {
		block := &fn.Blocks[bi]
		for ii := range block.Instrs {
			if err := c.walkInstr(&block.Instrs[ii]); err != nil {
				return fmt.Errorf("bb%d instr%d: %w", bi, ii, err)
			}
		}
		if err := c.walkTerm(&block.Term); err != nil {
			return fmt.Errorf("bb%d terminator: %w", bi, err)
		}
	}
	return nil
}
