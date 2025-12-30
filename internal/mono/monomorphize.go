package mono

import (
	"fmt"
	"slices"
	"strings"

	"fortio.org/safecast"

	"surge/internal/hir"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type Options struct {
	MaxDepth  int
	EnableDCE bool
}

func MonomorphizeProgram(mods []*hir.Module, inst *InstantiationMap, semaRes *sema.Result, opt Options) (*MonoProgram, error) {
	if opt.MaxDepth <= 0 {
		opt.MaxDepth = 64
	}
	out := &MonoProgram{Modules: make([]*MonoModule, 0, len(mods))}
	for _, m := range mods {
		if m == nil {
			continue
		}
		mm, err := MonomorphizeModule(m, inst, semaRes, opt)
		if err != nil {
			return nil, err
		}
		out.Modules = append(out.Modules, mm)
	}
	return out, nil
}

type MonoProgram struct {
	Modules []*MonoModule
}

func MonomorphizeModule(m *hir.Module, inst *InstantiationMap, semaRes *sema.Result, opt Options) (*MonoModule, error) {
	if opt.MaxDepth <= 0 {
		opt.MaxDepth = 64
	}
	if m == nil {
		return &MonoModule{}, nil
	}
	typesIn := m.TypeInterner
	if typesIn == nil && semaRes != nil {
		typesIn = semaRes.TypeInterner
	}
	b := newMonoBuilder(m, inst, typesIn, opt)
	if err := b.seed(); err != nil {
		return nil, err
	}
	if opt.EnableDCE {
		b.applyDCE()
	}
	return b.mm, nil
}

type useSiteKey struct {
	Kind   InstantiationKind
	Caller symbols.SymbolID
	Callee symbols.SymbolID
	Span   source.Span
}

type callSiteKey struct {
	Kind   InstantiationKind
	Caller symbols.SymbolID
	Span   source.Span
}

type callSiteInfo struct {
	Callee   symbols.SymbolID
	TypeArgs []types.TypeID
}

type monoBuilder struct {
	mod   *hir.Module
	inst  *InstantiationMap
	types *types.Interner
	opt   Options

	origFuncBySym map[symbols.SymbolID]*hir.Func
	typeSymByName map[source.StringID]symbols.SymbolID

	useSites  map[useSiteKey][]types.TypeID
	callSites map[callSiteKey]callSiteInfo

	nextSym  uint32
	nextFunc uint32

	mm *MonoModule
}

func newMonoBuilder(mod *hir.Module, inst *InstantiationMap, typesIn *types.Interner, opt Options) *monoBuilder {
	return &monoBuilder{
		mod:           mod,
		inst:          inst,
		types:         typesIn,
		origFuncBySym: make(map[symbols.SymbolID]*hir.Func),
		typeSymByName: make(map[source.StringID]symbols.SymbolID),
		useSites:      make(map[useSiteKey][]types.TypeID),
		callSites:     make(map[callSiteKey]callSiteInfo),
		nextSym:       1,
		nextFunc:      1,
		mm: &MonoModule{
			Source:    mod,
			Funcs:     make(map[MonoKey]*MonoFunc),
			FuncBySym: make(map[symbols.SymbolID]*MonoFunc),
			Types:     make(map[MonoKey]*MonoType),
		},
		opt: opt,
	}
}

func (b *monoBuilder) seed() error {
	if b == nil || b.mod == nil {
		return nil
	}
	for _, fn := range b.mod.Funcs {
		if fn == nil || !fn.SymbolID.IsValid() {
			continue
		}
		b.origFuncBySym[fn.SymbolID] = fn
	}
	if b.mod.Symbols != nil && b.mod.Symbols.Table != nil && b.mod.Symbols.Table.Symbols != nil {
		syms := b.mod.Symbols.Table.Symbols
		limit, err := safecast.Conv[uint32](syms.Len())
		if err != nil {
			return fmt.Errorf("mono: too many symbols: %w", err)
		}
		for id := symbols.SymbolID(1); id <= symbols.SymbolID(limit); id++ {
			s := syms.Get(id)
			if s == nil {
				continue
			}
			if s.Kind == symbols.SymbolType && s.Name != source.NoStringID {
				if _, ok := b.typeSymByName[s.Name]; !ok {
					b.typeSymByName[s.Name] = id
				}
			}
		}
	}

	b.indexUseSites()

	// 1) Instantiate every non-generic function definition.
	for _, fn := range b.mod.Funcs {
		if fn == nil || !fn.SymbolID.IsValid() {
			continue
		}
		if fn.IsGeneric() || b.symbolTypeParamCount(fn.SymbolID) > 0 || b.funcHasGenericTypes(fn) {
			continue
		}
		if _, err := b.ensureFunc(fn.SymbolID, nil, nil); err != nil {
			return err
		}
	}

	// 2) Instantiate every recorded generic fn/tag instantiation with concrete type args.
	if b.inst != nil {
		entries := make([]*InstEntry, 0, len(b.inst.Entries))
		for _, e := range b.inst.Entries {
			if e == nil || len(e.TypeArgs) == 0 {
				continue
			}
			switch e.Kind {
			case InstFn, InstTag:
				entries = append(entries, e)
			}
		}
		slices.SortStableFunc(entries, func(a, c *InstEntry) int {
			if a.Kind != c.Kind {
				return int(a.Kind) - int(c.Kind)
			}
			if a.Key.Sym != c.Key.Sym {
				if a.Key.Sym < c.Key.Sym {
					return -1
				}
				return 1
			}
			return slices.Compare(a.TypeArgs, c.TypeArgs)
		})
		for _, e := range entries {
			if !typeArgsAreConcrete(b.types, e.TypeArgs) {
				continue
			}
			if _, err := b.ensureFunc(e.Key.Sym, e.TypeArgs, nil); err != nil {
				return err
			}
		}
	}

	// 3) Seed explicit type instantiations.
	if b.inst != nil {
		entries := make([]*InstEntry, 0, len(b.inst.Entries))
		for _, e := range b.inst.Entries {
			if e == nil || len(e.TypeArgs) == 0 || e.Kind != InstType {
				continue
			}
			entries = append(entries, e)
		}
		slices.SortStableFunc(entries, func(a, c *InstEntry) int {
			if a.Key.Sym != c.Key.Sym {
				if a.Key.Sym < c.Key.Sym {
					return -1
				}
				return 1
			}
			return slices.Compare(a.TypeArgs, c.TypeArgs)
		})
		for _, e := range entries {
			if !typeArgsAreConcrete(b.types, e.TypeArgs) {
				continue
			}
			b.ensureTypeBySymbol(e.Key.Sym, e.TypeArgs)
		}
	}

	// 4) Collect nominal types referenced by monomorphized functions.
	b.collectTypesFromFuncs()

	if err := validateMonoModuleNoTypeParams(b.mm, b.types); err != nil {
		return err
	}

	return nil
}

func (b *monoBuilder) indexUseSites() {
	if b == nil || b.inst == nil {
		return
	}
	for _, e := range b.inst.Entries {
		if e == nil || !e.Key.Sym.IsValid() || len(e.TypeArgs) == 0 {
			continue
		}
		for _, us := range e.UseSites {
			if us.Span == (source.Span{}) || !us.Caller.IsValid() {
				continue
			}
			key := useSiteKey{
				Kind:   e.Kind,
				Caller: us.Caller,
				Callee: e.Key.Sym,
				Span:   us.Span,
			}
			if _, ok := b.useSites[key]; ok {
				continue
			}
			b.useSites[key] = slices.Clone(e.TypeArgs)

			callKey := callSiteKey{
				Kind:   e.Kind,
				Caller: us.Caller,
				Span:   us.Span,
			}
			if _, ok := b.callSites[callKey]; ok {
				continue
			}
			b.callSites[callKey] = callSiteInfo{
				Callee:   e.Key.Sym,
				TypeArgs: slices.Clone(e.TypeArgs),
			}
		}
	}
}

func (b *monoBuilder) allocInstanceSym() symbols.SymbolID {
	if b == nil {
		return symbols.NoSymbolID
	}
	id := symbols.SymbolID(0x9000_0000 + b.nextSym)
	b.nextSym++
	return id
}

func (b *monoBuilder) allocFuncID() hir.FuncID {
	if b == nil {
		return hir.NoFuncID
	}
	id := hir.FuncID(0x8000_0000 + b.nextFunc)
	b.nextFunc++
	return id
}

func (b *monoBuilder) monoName(sym symbols.SymbolID, args []types.TypeID) string {
	if b == nil || b.mod == nil || b.mod.Symbols == nil || b.mod.Symbols.Table == nil || b.mod.Symbols.Table.Strings == nil {
		return fmt.Sprintf("sym#%d", sym)
	}
	strs := b.mod.Symbols.Table.Strings
	base := symbolName(b.mod.Symbols, strs, sym)
	if len(args) == 0 {
		return base
	}
	return base + formatTypeArgs(b.types, strs, args)
}

func (b *monoBuilder) ensureFunc(origSym symbols.SymbolID, typeArgs []types.TypeID, stack []MonoKey) (*MonoFunc, error) {
	if b == nil || !origSym.IsValid() {
		return nil, nil
	}
	if b.types == nil {
		return nil, fmt.Errorf("mono: missing types interner")
	}

	normalized := NormalizeTypeArgs(b.types, typeArgs)
	expectedTypeArgs := b.symbolTypeParamCount(origSym)
	switch {
	case expectedTypeArgs == 0 && len(normalized) > 0:
		return nil, fmt.Errorf("mono: non-generic symbol %d cannot be instantiated with type args", origSym)
	case expectedTypeArgs > 0 && len(normalized) != expectedTypeArgs:
		return nil, fmt.Errorf("mono: symbol %d expects %d type args, got %d", origSym, expectedTypeArgs, len(normalized))
	}
	if len(normalized) > 0 && !typeArgsAreConcrete(b.types, normalized) {
		name := b.monoName(origSym, nil)
		args := "<?>"
		if b.mod != nil && b.mod.Symbols != nil && b.mod.Symbols.Table != nil && b.mod.Symbols.Table.Strings != nil {
			args = formatTypeArgs(b.types, b.mod.Symbols.Table.Strings, normalized)
		}
		stackMsg := ""
		if len(stack) > 0 {
			parts := make([]string, 0, len(stack))
			for _, k := range stack {
				parts = append(parts, fmt.Sprintf("%s[%s]", b.monoName(k.Sym, nil), k.ArgsKey))
			}
			stackMsg = " stack=" + strings.Join(parts, " -> ")
		}
		return nil, fmt.Errorf("mono: non-concrete type args for %s (sym=%d args=%s)%s", name, origSym, args, stackMsg)
	}
	key := MonoKey{Sym: origSym, ArgsKey: argsKeyFromTypes(normalized)}
	if existing := b.mm.Funcs[key]; existing != nil {
		return existing, nil
	}

	if len(stack) >= b.opt.MaxDepth {
		return nil, fmt.Errorf("mono: instantiation depth exceeded (%d)", b.opt.MaxDepth)
	}
	for _, k := range stack {
		if k == key {
			return nil, fmt.Errorf("mono: instantiation cycle detected at sym=%d args=%s", key.Sym, key.ArgsKey)
		}
	}

	instanceSym := b.allocInstanceSym()
	out := &MonoFunc{
		Key:         key,
		InstanceSym: instanceSym,
		OrigSym:     origSym,
		TypeArgs:    normalized,
	}
	b.mm.Funcs[key] = out
	b.mm.FuncBySym[instanceSym] = out

	origFn := b.origFuncBySym[origSym]
	if origFn == nil {
		// Imported/intrinsic function without HIR body.
		return out, nil
	}

	if origFn.IsGeneric() {
		if len(normalized) == 0 {
			return nil, fmt.Errorf("mono: missing type args for generic function %s", origFn.Name)
		}
		if len(normalized) != len(origFn.GenericParams) {
			return nil, fmt.Errorf("mono: generic function %s expects %d type args, got %d", origFn.Name, len(origFn.GenericParams), len(normalized))
		}
	}

	clone := cloneFunc(origFn)
	clone.ID = b.allocFuncID()
	clone.SymbolID = instanceSym
	clone.Name = b.monoName(origSym, normalized)
	clone.GenericParams = nil
	clone.Borrow = nil
	clone.MovePlan = nil

	var subst *Subst
	if len(normalized) > 0 {
		subst = &Subst{
			Types:    b.types,
			OwnerSym: origSym,
			TypeArgs: normalized,
		}
		if b.mod != nil && b.mod.Symbols != nil && b.mod.Symbols.Table != nil && b.mod.Symbols.Table.Symbols != nil {
			if owner := b.mod.Symbols.Table.Symbols.Get(origSym); owner != nil && len(owner.TypeParams) == len(normalized) {
				subst.NameArgs = make(map[source.StringID]types.TypeID, len(normalized))
				for i, name := range owner.TypeParams {
					if name != source.NoStringID && normalized[i] != types.NoTypeID {
						subst.NameArgs[name] = normalized[i]
					}
				}
			}
		}
		if recvSym := b.receiverTypeSymbol(origSym); recvSym.IsValid() && recvSym != origSym {
			subst.OwnerSyms = append(subst.OwnerSyms, recvSym)
		}
		if err := subst.ApplyFunc(clone); err != nil {
			return nil, err
		}
	}

	if err := b.rewriteCallsInFunc(clone, origSym, subst, append(stack, key)); err != nil {
		return nil, err
	}

	out.Func = clone
	return out, nil
}

func (b *monoBuilder) isTagSymbol(sym symbols.SymbolID) bool {
	if b == nil || b.mod == nil || b.mod.Symbols == nil || b.mod.Symbols.Table == nil || b.mod.Symbols.Table.Symbols == nil || !sym.IsValid() {
		return false
	}
	s := b.mod.Symbols.Table.Symbols.Get(sym)
	return s != nil && s.Kind == symbols.SymbolTag
}

func (b *monoBuilder) funcHasGenericTypes(fn *hir.Func) bool {
	if b == nil || b.types == nil || fn == nil {
		return false
	}
	if fn.Result != types.NoTypeID {
		if typeContainsGenericParam(b.types, fn.Result, make(map[types.TypeID]struct{})) {
			return true
		}
	}
	for _, p := range fn.Params {
		if p.Type == types.NoTypeID {
			continue
		}
		if typeContainsGenericParam(b.types, p.Type, make(map[types.TypeID]struct{})) {
			return true
		}
	}
	return false
}

func (b *monoBuilder) receiverTypeSymbol(symID symbols.SymbolID) symbols.SymbolID {
	if b == nil || b.mod == nil || b.mod.Symbols == nil || b.mod.Symbols.Table == nil || b.mod.Symbols.Table.Symbols == nil || b.mod.Symbols.Table.Strings == nil {
		return symbols.NoSymbolID
	}
	sym := b.mod.Symbols.Table.Symbols.Get(symID)
	if sym == nil || sym.ReceiverKey == "" {
		return symbols.NoSymbolID
	}
	base := baseTypeName(sym.ReceiverKey)
	if base == "" {
		return symbols.NoSymbolID
	}
	nameID := b.mod.Symbols.Table.Strings.Intern(base)
	if recvSym, ok := b.typeSymByName[nameID]; ok {
		return recvSym
	}
	return symbols.NoSymbolID
}

func baseTypeName(key symbols.TypeKey) string {
	raw := strings.TrimSpace(string(key))
	for {
		switch {
		case strings.HasPrefix(raw, "&mut "):
			raw = strings.TrimSpace(strings.TrimPrefix(raw, "&mut "))
		case strings.HasPrefix(raw, "&"):
			raw = strings.TrimSpace(strings.TrimPrefix(raw, "&"))
		case strings.HasPrefix(raw, "own "):
			raw = strings.TrimSpace(strings.TrimPrefix(raw, "own "))
		case strings.HasPrefix(raw, "*"):
			raw = strings.TrimSpace(strings.TrimPrefix(raw, "*"))
		default:
			goto done
		}
	}
done:
	if idx := strings.IndexAny(raw, "<["); idx >= 0 {
		raw = raw[:idx]
	}
	if idx := strings.LastIndex(raw, "::"); idx >= 0 {
		raw = raw[idx+2:]
	}
	return strings.TrimSpace(raw)
}

func (b *monoBuilder) isCallableSymbol(sym symbols.SymbolID) bool {
	if b == nil || b.mod == nil || b.mod.Symbols == nil || b.mod.Symbols.Table == nil || b.mod.Symbols.Table.Symbols == nil || !sym.IsValid() {
		return false
	}
	s := b.mod.Symbols.Table.Symbols.Get(sym)
	return s != nil && (s.Kind == symbols.SymbolFunction || s.Kind == symbols.SymbolTag)
}

func (b *monoBuilder) isGenericSymbol(sym symbols.SymbolID) bool {
	if b == nil || b.mod == nil || b.mod.Symbols == nil || b.mod.Symbols.Table == nil || b.mod.Symbols.Table.Symbols == nil || !sym.IsValid() {
		return false
	}
	s := b.mod.Symbols.Table.Symbols.Get(sym)
	return s != nil && len(s.TypeParams) > 0
}

func (b *monoBuilder) symbolTypeParamCount(sym symbols.SymbolID) int {
	if b == nil || b.mod == nil || b.mod.Symbols == nil || b.mod.Symbols.Table == nil || b.mod.Symbols.Table.Symbols == nil || !sym.IsValid() {
		return -1
	}
	s := b.mod.Symbols.Table.Symbols.Get(sym)
	if s == nil {
		return -1
	}
	return len(s.TypeParams)
}

func (b *monoBuilder) ensureTypeBySymbol(typeSym symbols.SymbolID, typeArgs []types.TypeID) *MonoType {
	if b == nil || !typeSym.IsValid() || len(typeArgs) == 0 {
		return nil
	}
	normalized := NormalizeTypeArgs(b.types, typeArgs)
	key := MonoKey{Sym: typeSym, ArgsKey: argsKeyFromTypes(normalized)}
	if existing := b.mm.Types[key]; existing != nil {
		return existing
	}
	if b.mod == nil || b.mod.Symbols == nil || b.mod.Symbols.Table == nil || b.mod.Symbols.Table.Symbols == nil || b.mod.Symbols.Table.Strings == nil {
		return nil
	}
	sym := b.mod.Symbols.Table.Symbols.Get(typeSym)
	if sym == nil || sym.Kind != symbols.SymbolType || sym.Name == source.NoStringID || b.types == nil {
		return nil
	}

	base := sym.Type
	if base == types.NoTypeID {
		return nil
	}
	baseTT, ok := b.types.Lookup(base)
	if !ok {
		return nil
	}

	var typeID types.TypeID
	switch baseTT.Kind {
	case types.KindStruct:
		if id, ok := b.types.FindStructInstance(sym.Name, normalized); ok {
			typeID = id
		}
	case types.KindUnion:
		if id, ok := b.types.FindUnionInstance(sym.Name, normalized); ok {
			typeID = id
		}
	case types.KindAlias:
		if id, ok := b.types.FindAliasInstance(sym.Name, normalized); ok {
			typeID = id
		}
	default:
		return nil
	}
	if typeID == types.NoTypeID {
		return nil
	}

	mt := &MonoType{
		Key:      key,
		OrigSym:  typeSym,
		TypeArgs: normalized,
		TypeID:   typeID,
	}
	b.mm.Types[key] = mt
	return mt
}

func (b *monoBuilder) collectTypesFromFuncs() {
	if b == nil || b.mm == nil || b.types == nil {
		return
	}
	seen := make(map[types.TypeID]struct{})
	for _, mf := range b.mm.Funcs {
		if mf == nil || mf.Func == nil {
			continue
		}
		collectTypeFromFunc(mf.Func, func(id types.TypeID) {
			if id == types.NoTypeID {
				return
			}
			if _, ok := seen[id]; ok {
				return
			}
			seen[id] = struct{}{}
			b.ensureTypeFromTypeID(id)
		})
	}
}

func (b *monoBuilder) ensureTypeFromTypeID(id types.TypeID) *MonoType {
	if b == nil || b.types == nil || id == types.NoTypeID {
		return nil
	}
	tt, ok := b.types.Lookup(id)
	if !ok {
		return nil
	}
	switch tt.Kind {
	case types.KindStruct:
		info, ok := b.types.StructInfo(id)
		if !ok || info == nil || info.Name == source.NoStringID || len(info.TypeArgs) == 0 {
			return nil
		}
		if sym, ok := b.typeSymByName[info.Name]; ok {
			return b.ensureTypeBySymbol(sym, info.TypeArgs)
		}
	case types.KindUnion:
		info, ok := b.types.UnionInfo(id)
		if !ok || info == nil || info.Name == source.NoStringID || len(info.TypeArgs) == 0 {
			return nil
		}
		if sym, ok := b.typeSymByName[info.Name]; ok {
			return b.ensureTypeBySymbol(sym, info.TypeArgs)
		}
	case types.KindAlias:
		info, ok := b.types.AliasInfo(id)
		if !ok || info == nil || info.Name == source.NoStringID || len(info.TypeArgs) == 0 {
			return nil
		}
		if sym, ok := b.typeSymByName[info.Name]; ok {
			return b.ensureTypeBySymbol(sym, info.TypeArgs)
		}
	}
	return nil
}
