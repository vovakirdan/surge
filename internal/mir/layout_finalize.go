package mir

import (
	"fmt"
	"sort"

	"surge/internal/layout"
	"surge/internal/symbols"
	"surge/internal/types"
)

// RootRole records how an operation root was reached. Roles are bit flags so a
// type reached through several uses carries every one of them.
type RootRole uint8

const (
	// RootValue marks a root whose values are stored, moved or copied. Every
	// root carries it.
	RootValue RootRole = 1 << iota
	// RootKey marks a root reached as a map key, which additionally needs
	// hashing and equality.
	RootKey
)

// RootCensus is the set of operation roots one module walk reached, in
// discovery order, together with the roles each root was reached through.
type RootCensus struct {
	order []types.TypeID
	roles map[types.TypeID]RootRole
}

// Values returns every root in discovery order.
func (c *RootCensus) Values() []types.TypeID {
	if c == nil {
		return nil
	}
	return append([]types.TypeID(nil), c.order...)
}

// Keys returns, in discovery order, the roots that were reached as a map key.
func (c *RootCensus) Keys() []types.TypeID {
	if c == nil {
		return nil
	}
	keys := make([]types.TypeID, 0, len(c.order))
	for _, id := range c.order {
		if c.roles[id]&RootKey != 0 {
			keys = append(keys, id)
		}
	}
	return keys
}

// Role returns the accumulated roles of id, or zero when id is not a root.
func (c *RootCensus) Role(id types.TypeID) RootRole {
	if c == nil {
		return 0
	}
	return c.roles[id]
}

// record accumulates role for id, adding id to the order the first time it is
// seen. Later sightings only widen the role, never reorder the census.
func (c *RootCensus) record(id types.TypeID, role RootRole) {
	if c.roles == nil {
		c.roles = make(map[types.TypeID]RootRole, 256)
	}
	if _, seen := c.roles[id]; !seen {
		c.order = append(c.order, id)
	}
	c.roles[id] |= role
}

type layoutRootCollector struct {
	types         *types.Interner
	builder       *layout.LayoutEngine
	canonicalSeen map[types.TypeID]struct{}
	census        RootCensus
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
	census, err := collectOperationRoots(m, typesIn, builder)
	if err != nil {
		return err
	}
	registry, err := layout.FinalizeRegistry(builder, census.Values())
	if err != nil {
		return fmt.Errorf("mir: finalize physical layouts: %w", err)
	}
	m.Meta.Layouts = registry
	return nil
}

func collectOperationRoots(m *Module, typesIn *types.Interner, builder *layout.LayoutEngine) (*RootCensus, error) {
	collector := &layoutRootCollector{
		types:         typesIn,
		builder:       builder,
		canonicalSeen: make(map[types.TypeID]struct{}, 256),
	}
	for i := range m.Globals {
		if err := collector.addType(m.Globals[i].Type, RootValue); err != nil {
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
			if err := collector.addType(id, RootValue); err != nil {
				return nil, fmt.Errorf("mir: layout root tag type#%d: %w", id, err)
			}
			for _, tagCase := range m.Meta.TagLayouts[id] {
				for _, payload := range tagCase.PayloadTypes {
					if err := collector.addType(payload, RootValue); err != nil {
						return nil, fmt.Errorf("mir: layout root tag payload type#%d: %w", payload, err)
					}
				}
			}
		}
	}
	return &collector.census, nil
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
	return c.addType(id, RootValue)
}

// addType records role for id and, the first time id's canonical form is seen,
// walks its children. The role is recorded before the canonical guard below, so
// a type first reached as a plain value still gains RootKey when a map later
// reaches it as a key. Callers pass the role; they never write it themselves.
func (c *layoutRootCollector) addType(id types.TypeID, role RootRole) error {
	if id == types.NoTypeID {
		return nil
	}
	c.census.record(id, role)
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
		// A map's payloads are exactly {key, value}: payload 0 is additionally
		// a key. Every other handle owns plain values.
		_, _, isMap := c.types.MapInfo(canonical)
		for i, payload := range payloads {
			payloadRole := RootValue
			if isMap && i == 0 {
				payloadRole |= RootKey
			}
			if err := c.addType(payload, payloadRole); err != nil {
				return err
			}
		}
		return nil
	}

	switch t.Kind {
	case types.KindStruct:
		if elem, _, ok := c.types.ArrayFixedInfo(canonical); ok {
			return c.addType(elem, RootValue)
		}
		info, ok := c.types.StructInfo(canonical)
		if !ok || info == nil {
			return fmt.Errorf("type#%d missing struct metadata", canonical)
		}
		for _, field := range info.Fields {
			if err := c.addType(field.Type, RootValue); err != nil {
				return err
			}
		}
	case types.KindTuple:
		info, ok := c.types.TupleInfo(canonical)
		if !ok || info == nil {
			return fmt.Errorf("type#%d missing tuple metadata", canonical)
		}
		for _, elem := range info.Elems {
			if err := c.addType(elem, RootValue); err != nil {
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
				if err := c.addType(member.Type, RootValue); err != nil {
					return err
				}
			case types.UnionMemberTag:
				for _, arg := range member.TagArgs {
					if err := c.addType(arg, RootValue); err != nil {
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
		if err := c.addType(info.BaseType, RootValue); err != nil {
			return err
		}
	case types.KindFn, types.KindPointer, types.KindReference, types.KindFar:
		// Opaque physical boundaries. Their pointee/signature children do not
		// influence the handle layout and may remain generic indefinitely.
	case types.KindArray:
		if err := c.addType(t.Elem, RootValue); err != nil {
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
	if err := c.addType(fn.Result, RootValue); err != nil {
		return err
	}
	for i := range fn.Locals {
		if err := c.addType(fn.Locals[i].Type, RootValue); err != nil {
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
