package mono

import (
	"fmt"
	"slices"

	"fortio.org/safecast"

	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
)

func (b *monoBuilder) seed() error {
	if b == nil || b.mod == nil {
		return nil
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

	if err := b.indexUseSites(); err != nil {
		return err
	}

	// The finalized closure is the sole callable worklist. Canonical sorting
	// makes allocation and emitted output independent of raw SymbolID order.
	if b.closure != nil {
		for i := range b.closure.Instances {
			instance := &b.closure.Instances[i]
			if !typeArgsAreConcrete(b.types, instance.TemplateArgs) {
				return fmt.Errorf("mono: authoritative callable %s has non-concrete type arguments", b.monoName(instance.Template, instance.TemplateArgs))
			}
		}
		for _, key := range b.sortedRetainedCallableKeys() {
			retained := b.retainedCallables[key]
			if err := b.ensureFunc(retained.Template, retained.TemplateArgs, nil); err != nil {
				return err
			}
		}
	} else {
		// Compatibility for isolated low-level callers without sema closure:
		// instantiate every concrete non-generic HIR function first.
		for _, fn := range b.mod.Funcs {
			if fn == nil || !fn.SymbolID.IsValid() {
				continue
			}
			if fn.IsGeneric() || b.symbolTypeParamCount(fn.SymbolID) > 0 || b.funcHasGenericTypes(fn) {
				continue
			}
			if err := b.ensureFunc(fn.SymbolID, nil, nil); err != nil {
				return err
			}
		}
	}

	// Legacy generic seed for isolated callers without finalized sema.
	if b.closure == nil && b.inst != nil {
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
			if err := b.ensureFunc(e.Key.Sym, e.TypeArgs, nil); err != nil {
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

func (b *monoBuilder) indexUseSites() error {
	if b == nil {
		return nil
	}
	if b.closure != nil {
		for i := range b.closure.UseSites {
			use := &b.closure.UseSites[i]
			if use.Site == (source.Span{}) || !use.CallerTemplate.IsValid() || !use.CalleeTemplate.IsValid() || len(use.TemplateArgs) == 0 {
				continue
			}
			callerKey, err := canonicalCallableKey(b.identity, use.CallerTemplate, use.CallerTemplateArgs)
			if err != nil {
				return err
			}
			if use.Caller.TemplateKey != "" && (callerKey.TemplateKey != use.Caller.TemplateKey || callerKey.ArgsKey != use.Caller.ArgsKey) {
				return fmt.Errorf("mono: closure use at %s disagrees with its canonical caller key", use.Site)
			}
			calleeKey, err := canonicalCallableKey(b.identity, use.CalleeTemplate, use.TemplateArgs)
			if err != nil {
				return err
			}
			if calleeKey.TemplateKey != use.Callee.TemplateKey || calleeKey.ArgsKey != use.Callee.ArgsKey {
				return fmt.Errorf("mono: closure use at %s disagrees with its canonical callee key", use.Site)
			}
			kind := monoInstantiationKind(use.Kind)
			key := useSiteKey{
				Kind:              kind,
				Caller:            callerKey,
				CalleeTemplateKey: calleeKey.TemplateKey,
				Span:              use.Site,
			}
			b.useSites[key] = slices.Clone(use.TemplateArgs)

			callKey := callSiteKey{
				Kind:   kind,
				Caller: callerKey,
				Span:   use.Site,
			}
			b.callSites[callKey] = callSiteInfo{
				Callee:   use.CalleeTemplate,
				TypeArgs: slices.Clone(use.TemplateArgs),
			}
		}
		for i := range b.closure.ResolvedDeferredCalls {
			call := sema.CloneResolvedDeferredCallForConsumer(&b.closure.ResolvedDeferredCalls[i])
			callerKey, err := canonicalCallableKey(b.identity, call.CallerTemplate, call.CallerTemplateArgs)
			if err != nil {
				return err
			}
			if callerKey.TemplateKey != call.Caller.TemplateKey || callerKey.ArgsKey != call.Caller.ArgsKey {
				return fmt.Errorf("mono: deferred callable %s disagrees with its canonical caller key", call.UseID)
			}
			key := deferredCallSiteKey{
				Caller: call.Caller, UseID: call.UseID,
			}
			b.deferredCalls[key] = call
		}
		return nil
	}
	if b.inst == nil {
		return nil
	}
	for _, e := range b.inst.Entries {
		if e == nil || !e.Key.Sym.IsValid() || len(e.TypeArgs) == 0 {
			continue
		}
		for _, us := range e.UseSites {
			if us.Span == (source.Span{}) || !us.Caller.IsValid() {
				continue
			}
			callerKey, err := canonicalCallableKey(b.identity, us.Caller, nil)
			if err != nil {
				return err
			}
			calleeKey, err := canonicalCallableKey(b.identity, e.Key.Sym, e.TypeArgs)
			if err != nil {
				return err
			}
			key := useSiteKey{
				Kind:              e.Kind,
				Caller:            callerKey,
				CalleeTemplateKey: calleeKey.TemplateKey,
				Span:              us.Span,
			}
			if _, ok := b.useSites[key]; ok {
				continue
			}
			b.useSites[key] = slices.Clone(e.TypeArgs)

			callKey := callSiteKey{
				Kind:   e.Kind,
				Caller: callerKey,
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
	return nil
}
