package mono

import (
	"fmt"
	"slices"
	"strings"

	"surge/internal/hir"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Options configures the monomorphization process.
type Options struct {
	MaxDepth  int
	EnableDCE bool
}

// MonomorphizeProgram monomorphizes a set of HIR modules into concrete instances.
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

// MonoProgram represents a fully monomorphized program.
// Note: The name stutters with the package name, but is kept for consistency.
type MonoProgram struct { //nolint:revive
	Modules []*MonoModule
}

// MonomorphizeModule monomorphizes a single HIR module.
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
	authoritative, closure, err := authoritativeInstantiationMap(inst, semaRes)
	if err != nil {
		return nil, err
	}
	var templateParams map[symbols.SymbolID][]types.TypeID
	var identity *sema.InstantiationIdentity
	if semaRes != nil {
		templateParams = semaRes.InstantiationTemplateParams
		identity = semaRes.InstantiationIdentity
	}
	b, err := newMonoBuilder(m, authoritative, closure, identity, templateParams, typesIn, opt)
	if err != nil {
		return nil, err
	}
	if semaRes != nil {
		b.entrypointBindings = cloneEntrypointCallableBindings(semaRes.EntrypointCallableBindings)
	}
	if err := b.seed(); err != nil {
		return nil, err
	}
	if opt.EnableDCE {
		if err := b.applyDCE(); err != nil {
			return nil, err
		}
	}
	return b.mm, nil
}

type useSiteKey struct {
	Kind              InstantiationKind
	Caller            CallableKey
	CalleeTemplateKey string
	Span              source.Span
}

type callSiteKey struct {
	Kind   InstantiationKind
	Caller CallableKey
	Span   source.Span
}

type callSiteInfo struct {
	Callee   symbols.SymbolID
	TypeArgs []types.TypeID
}

type deferredCallSiteKey struct {
	Caller sema.InstanceKey
	UseID  sema.DeferredUseID
}

type monoBuilder struct {
	mod            *hir.Module
	inst           *InstantiationMap
	closure        *sema.InstantiationClosure
	identity       *sema.InstantiationIdentity
	templateParams map[symbols.SymbolID][]types.TypeID
	types          *types.Interner
	opt            Options

	origFuncBySym map[symbols.SymbolID]*hir.Func
	typeSymByName map[source.StringID]symbols.SymbolID

	useSites           map[useSiteKey][]types.TypeID
	callSites          map[callSiteKey]callSiteInfo
	deferredCalls      map[deferredCallSiteKey]sema.ResolvedDeferredCall
	entrypointBindings []sema.EntrypointCallableBinding
	// retainedCallables is non-nil whenever a finalized semantic closure is
	// present, including an intentionally empty closure. Its canonical keys
	// collapse every post-merge raw symbol alias to one emitted representative.
	retainedCallables map[CallableKey]retainedCallable

	nextSym  uint32
	nextFunc uint32

	mm *MonoModule
}

func cloneEntrypointCallableBindings(input []sema.EntrypointCallableBinding) []sema.EntrypointCallableBinding {
	out := make([]sema.EntrypointCallableBinding, len(input))
	for i := range input {
		out[i] = input[i]
		out[i].TemplateArgs = slices.Clone(input[i].TemplateArgs)
		out[i].ParamTypes = slices.Clone(input[i].ParamTypes)
	}
	return out
}

func newMonoBuilder(
	mod *hir.Module,
	inst *InstantiationMap,
	closure *sema.InstantiationClosure,
	identity *sema.InstantiationIdentity,
	templateParams map[symbols.SymbolID][]types.TypeID,
	typesIn *types.Interner,
	opt Options,
) (*monoBuilder, error) {
	b := &monoBuilder{
		mod:            mod,
		inst:           inst,
		closure:        closure,
		identity:       identity,
		templateParams: templateParams,
		types:          typesIn,
		origFuncBySym:  make(map[symbols.SymbolID]*hir.Func),
		typeSymByName:  make(map[source.StringID]symbols.SymbolID),
		useSites:       make(map[useSiteKey][]types.TypeID),
		callSites:      make(map[callSiteKey]callSiteInfo),
		deferredCalls:  make(map[deferredCallSiteKey]sema.ResolvedDeferredCall),
		nextSym:        1,
		nextFunc:       1,
		mm: &MonoModule{
			Source:    mod,
			Funcs:     make(map[MonoKey]*MonoFunc),
			FuncBySym: make(map[symbols.SymbolID]*MonoFunc),
			Types:     make(map[MonoKey]*MonoType),
			Callables: newCallableMap(identity),
		},
		opt: opt,
	}
	for _, fn := range b.mod.Funcs {
		if fn != nil && fn.SymbolID.IsValid() {
			b.origFuncBySym[fn.SymbolID] = fn
		}
	}
	if err := b.initializeRetainedCallables(); err != nil {
		return nil, err
	}
	return b, nil
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

func (b *monoBuilder) ensureCallableInstance(origSym symbols.SymbolID, typeArgs []types.TypeID, stack []MonoKey) (symbols.SymbolID, error) {
	if err := b.ensureFunc(origSym, typeArgs, stack); err != nil {
		return symbols.NoSymbolID, err
	}
	instance, ok, err := b.mm.Callables.LookupChecked(origSym, typeArgs)
	if err != nil {
		return symbols.NoSymbolID, err
	}
	if !ok || !instance.IsValid() {
		return symbols.NoSymbolID, fmt.Errorf("mono: callable %s was authorized but has no emitted instance", b.monoName(origSym, typeArgs))
	}
	return instance, nil
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
	for _, __k := range b.mm.SortedFuncKeys() {
		mf := b.mm.Funcs[__k]
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
