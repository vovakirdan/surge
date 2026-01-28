package mir

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/hir"
	"surge/internal/mono"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func buildTagLayouts(m *Module, src *hir.Module, typesIn *types.Interner) (tagLayouts map[types.TypeID][]TagCaseMeta, tagNames map[symbols.SymbolID]string) {
	if m == nil || src == nil || typesIn == nil {
		return nil, nil
	}
	if src.Symbols == nil || src.Symbols.Table == nil || src.Symbols.Table.Strings == nil || src.Symbols.Table.Symbols == nil {
		return nil, nil
	}
	tagSymByName := make(map[source.StringID]symbols.SymbolID)
	tagNamesBySym := make(map[symbols.SymbolID]string)
	maxSym, err := safecast.Conv[uint32](src.Symbols.Table.Symbols.Len())
	if err != nil {
		panic(fmt.Errorf("mir: symbol arena overflow: %w", err))
	}
	for id := symbols.SymbolID(1); id <= symbols.SymbolID(maxSym); id++ {
		sym := src.Symbols.Table.Symbols.Get(id)
		if sym == nil || sym.Kind != symbols.SymbolTag || sym.Name == source.NoStringID {
			continue
		}
		if name := src.Symbols.Table.Strings.MustLookup(sym.Name); name != "" {
			tagNamesBySym[id] = name
		}
		if existingID, exists := tagSymByName[sym.Name]; exists {
			existing := src.Symbols.Table.Symbols.Get(existingID)
			replace := false
			switch {
			case existing == nil:
				replace = true
			case sym.ModulePath == "core" && existing.ModulePath != "core":
				replace = true
			case sym.ModulePath != "" && existing.ModulePath == "":
				replace = true
			case sym.ModulePath == existing.ModulePath && id > existingID:
				replace = true
			}
			if replace {
				tagSymByName[sym.Name] = id
			}
			continue
		}
		tagSymByName[sym.Name] = id
	}

	typeIDs := make(map[types.TypeID]struct{})
	visited := make(map[types.TypeID]struct{})
	var visitType func(id types.TypeID)
	var visitTypeDeep func(id types.TypeID)
	visitType = func(id types.TypeID) {
		visitTypeDeep(id)
	}
	visitTypeDeep = func(id types.TypeID) {
		if id == types.NoTypeID {
			return
		}
		id = canonicalType(typesIn, id)
		if id == types.NoTypeID {
			return
		}
		if _, ok := visited[id]; ok {
			return
		}
		visited[id] = struct{}{}
		tt, ok := typesIn.Lookup(id)
		if !ok {
			return
		}
		if tt.Kind == types.KindUnion {
			typeIDs[id] = struct{}{}
		}
		switch tt.Kind {
		case types.KindUnion:
			if info, ok := typesIn.UnionInfo(id); ok && info != nil {
				for _, member := range info.Members {
					switch member.Kind {
					case types.UnionMemberType:
						visitTypeDeep(member.Type)
					case types.UnionMemberTag:
						for _, arg := range member.TagArgs {
							visitTypeDeep(arg)
						}
					default:
					}
				}
			}
		case types.KindStruct:
			if info, ok := typesIn.StructInfo(id); ok && info != nil {
				for _, field := range info.Fields {
					visitTypeDeep(field.Type)
				}
			}
		case types.KindTuple:
			if info, ok := typesIn.TupleInfo(id); ok && info != nil {
				for _, elem := range info.Elems {
					visitTypeDeep(elem)
				}
			}
		case types.KindArray:
			visitTypeDeep(tt.Elem)
		case types.KindEnum:
			if info, ok := typesIn.EnumInfo(id); ok && info != nil && info.BaseType != types.NoTypeID {
				visitTypeDeep(info.BaseType)
			}
		default:
		}
	}

	var visitOperand func(op *Operand)
	var visitRValue func(rv *RValue)
	visitOperand = func(op *Operand) {
		if op == nil {
			return
		}
		visitType(op.Type)
		if op.Kind == OperandConst {
			visitType(op.Const.Type)
		}
	}
	visitRValue = func(rv *RValue) {
		if rv == nil {
			return
		}
		switch rv.Kind {
		case RValueUse:
			visitOperand(&rv.Use)
		case RValueUnaryOp:
			visitOperand(&rv.Unary.Operand)
		case RValueBinaryOp:
			visitOperand(&rv.Binary.Left)
			visitOperand(&rv.Binary.Right)
		case RValueCast:
			visitOperand(&rv.Cast.Value)
			visitType(rv.Cast.TargetTy)
		case RValueStructLit:
			visitType(rv.StructLit.TypeID)
			for i := range rv.StructLit.Fields {
				visitOperand(&rv.StructLit.Fields[i].Value)
			}
		case RValueArrayLit:
			for i := range rv.ArrayLit.Elems {
				visitOperand(&rv.ArrayLit.Elems[i])
			}
		case RValueTupleLit:
			for i := range rv.TupleLit.Elems {
				visitOperand(&rv.TupleLit.Elems[i])
			}
		case RValueField:
			visitOperand(&rv.Field.Object)
		case RValueIndex:
			visitOperand(&rv.Index.Object)
			visitOperand(&rv.Index.Index)
		case RValueTagTest:
			visitOperand(&rv.TagTest.Value)
		case RValueTagPayload:
			visitOperand(&rv.TagPayload.Value)
		case RValueIterInit:
			visitOperand(&rv.IterInit.Iterable)
		case RValueIterNext:
			visitOperand(&rv.IterNext.Iter)
		default:
		}
	}

	for _, fn := range m.Funcs {
		if fn == nil {
			continue
		}
		visitType(fn.Result)
		for i := range fn.Locals {
			visitType(fn.Locals[i].Type)
		}
		for bi := range fn.Blocks {
			bb := &fn.Blocks[bi]
			for ii := range bb.Instrs {
				ins := &bb.Instrs[ii]
				switch ins.Kind {
				case InstrAssign:
					visitRValue(&ins.Assign.Src)
				case InstrCall:
					for ai := range ins.Call.Args {
						visitOperand(&ins.Call.Args[ai])
					}
				case InstrDrop:
					// place type is already on locals
				case InstrEndBorrow:
					// place type is already on locals
				case InstrAwait:
					visitOperand(&ins.Await.Task)
				case InstrSpawn:
					visitOperand(&ins.Spawn.Value)
				case InstrBlocking:
					for fi := range ins.Blocking.State.Fields {
						visitOperand(&ins.Blocking.State.Fields[fi].Value)
					}
				case InstrPoll:
					visitOperand(&ins.Poll.Task)
				case InstrJoinAll:
					visitOperand(&ins.JoinAll.Scope)
				case InstrChanSend:
					visitOperand(&ins.ChanSend.Channel)
					visitOperand(&ins.ChanSend.Value)
				case InstrChanRecv:
					visitOperand(&ins.ChanRecv.Channel)
				case InstrTimeout:
					visitOperand(&ins.Timeout.Task)
					visitOperand(&ins.Timeout.Ms)
				case InstrSelect:
					for ai := range ins.Select.Arms {
						arm := &ins.Select.Arms[ai]
						switch arm.Kind {
						case SelectArmTask:
							visitOperand(&arm.Task)
						case SelectArmChanRecv:
							visitOperand(&arm.Channel)
						case SelectArmChanSend:
							visitOperand(&arm.Channel)
							visitOperand(&arm.Value)
						case SelectArmTimeout:
							visitOperand(&arm.Task)
							visitOperand(&arm.Ms)
						}
					}
				default:
				}
			}
			switch bb.Term.Kind {
			case TermReturn:
				if bb.Term.Return.HasValue {
					visitOperand(&bb.Term.Return.Value)
				}
			case TermAsyncYield:
				visitOperand(&bb.Term.AsyncYield.State)
			case TermAsyncReturn:
				visitOperand(&bb.Term.AsyncReturn.State)
				if bb.Term.AsyncReturn.HasValue {
					visitOperand(&bb.Term.AsyncReturn.Value)
				}
			case TermAsyncReturnCancelled:
				visitOperand(&bb.Term.AsyncReturnCancelled.State)
			case TermIf:
				visitOperand(&bb.Term.If.Cond)
			case TermSwitchTag:
				visitOperand(&bb.Term.SwitchTag.Value)
			default:
			}
		}
	}

	layouts := make(map[types.TypeID][]TagCaseMeta)
	strs := src.Symbols.Table.Strings
	type tagKey struct {
		sym  symbols.SymbolID
		name string
	}
	keyFor := func(meta TagCaseMeta) tagKey {
		if meta.TagSym.IsValid() {
			return tagKey{sym: meta.TagSym}
		}
		return tagKey{name: meta.TagName}
	}
	collectTagCases := func(typeID types.TypeID) []TagCaseMeta {
		if typeID == types.NoTypeID {
			return nil
		}
		seen := make(map[tagKey]struct{})
		visiting := make(map[types.TypeID]struct{})
		cases := make([]TagCaseMeta, 0)
		addCase := func(meta TagCaseMeta) {
			key := keyFor(meta)
			if _, ok := seen[key]; ok {
				return
			}
			seen[key] = struct{}{}
			cases = append(cases, meta)
		}
		var visitUnion func(id types.TypeID)
		visitUnion = func(id types.TypeID) {
			if id == types.NoTypeID {
				return
			}
			id = canonicalType(typesIn, id)
			if id == types.NoTypeID {
				return
			}
			if _, ok := visiting[id]; ok {
				return
			}
			visiting[id] = struct{}{}
			info, ok := typesIn.UnionInfo(id)
			if !ok || info == nil || len(info.Members) == 0 {
				return
			}
			for _, member := range info.Members {
				switch member.Kind {
				case types.UnionMemberTag:
					tagName := strs.MustLookup(member.TagName)
					if tagName == "" {
						continue
					}
					payload := make([]types.TypeID, len(member.TagArgs))
					for i := range member.TagArgs {
						payload[i] = canonicalType(typesIn, member.TagArgs[i])
					}
					addCase(TagCaseMeta{
						TagName:      tagName,
						TagSym:       tagSymByName[member.TagName],
						PayloadTypes: payload,
					})
				case types.UnionMemberNothing:
					addCase(TagCaseMeta{TagName: "nothing"})
				case types.UnionMemberType:
					memberType := canonicalType(typesIn, member.Type)
					if memberType == types.NoTypeID {
						continue
					}
					if tt, ok := typesIn.Lookup(memberType); ok && tt.Kind == types.KindUnion {
						visitUnion(memberType)
					}
				default:
				}
			}
		}
		visitUnion(typeID)
		return cases
	}
	for typeID := range typeIDs {
		tt, ok := typesIn.Lookup(typeID)
		if !ok || tt.Kind != types.KindUnion {
			continue
		}
		cases := collectTagCases(typeID)
		if len(cases) == 0 {
			continue
		}
		layouts[typeID] = cases
	}

	if len(tagNamesBySym) == 0 {
		tagNamesBySym = nil
	}
	return layouts, tagNamesBySym
}

func buildTagAliases(mm *mono.MonoModule) map[symbols.SymbolID]symbols.SymbolID {
	if mm == nil || mm.Source == nil || mm.Source.Symbols == nil || mm.Source.Symbols.Table == nil || mm.Source.Symbols.Table.Symbols == nil {
		return nil
	}
	if len(mm.Funcs) == 0 {
		return nil
	}
	syms := mm.Source.Symbols.Table.Symbols
	out := make(map[symbols.SymbolID]symbols.SymbolID)
	for _, mf := range mm.Funcs {
		if mf == nil || !mf.InstanceSym.IsValid() || !mf.OrigSym.IsValid() {
			continue
		}
		if mf.InstanceSym == mf.OrigSym {
			continue
		}
		orig := syms.Get(mf.OrigSym)
		if orig == nil || orig.Kind != symbols.SymbolTag {
			continue
		}
		out[mf.InstanceSym] = mf.OrigSym
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func buildConstMap(src *hir.Module) map[symbols.SymbolID]*hir.ConstDecl {
	if src == nil || len(src.Consts) == 0 {
		return nil
	}
	out := make(map[symbols.SymbolID]*hir.ConstDecl, len(src.Consts))
	for i := range src.Consts {
		decl := &src.Consts[i]
		if !decl.SymbolID.IsValid() {
			continue
		}
		out[decl.SymbolID] = decl
	}
	return out
}

func buildGlobalMap(src *hir.Module) (out []Global, symToGlobal map[symbols.SymbolID]GlobalID) {
	if src == nil || len(src.Globals) == 0 {
		return nil, nil
	}
	out = make([]Global, 0, len(src.Globals))
	symToGlobal = make(map[symbols.SymbolID]GlobalID, len(src.Globals))
	for i := range src.Globals {
		decl := &src.Globals[i]
		if !decl.SymbolID.IsValid() {
			continue
		}
		ty := decl.Type
		if ty == types.NoTypeID && decl.Value != nil {
			ty = decl.Value.Type
		}
		lenOut, err := safecast.Conv[int32](len(out))
		if err != nil {
			panic(fmt.Errorf("mir: global id overflow: %w", err))
		}
		id := GlobalID(lenOut)
		out = append(out, Global{
			Sym:   decl.SymbolID,
			Type:  ty,
			Name:  decl.Name,
			IsMut: decl.IsMut,
			Span:  decl.Span,
		})
		symToGlobal[decl.SymbolID] = id
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, symToGlobal
}

func canonicalType(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if id == types.NoTypeID || typesIn == nil {
		return id
	}
	seen := 0
	for id != types.NoTypeID && seen < 32 {
		seen++
		tt, ok := typesIn.Lookup(id)
		if !ok {
			return id
		}
		switch tt.Kind {
		case types.KindAlias:
			target, ok := typesIn.AliasTarget(id)
			if !ok || target == types.NoTypeID || target == id {
				return id
			}
			id = target
		case types.KindOwn, types.KindReference, types.KindPointer:
			id = tt.Elem
		default:
			return id
		}
	}
	return id
}
