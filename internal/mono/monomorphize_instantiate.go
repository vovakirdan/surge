package mono

import (
	"fmt"
	"slices"
	"strings"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (b *monoBuilder) ensureFunc(origSym symbols.SymbolID, typeArgs []types.TypeID, stack []MonoKey) error {
	if b == nil || !origSym.IsValid() {
		return nil
	}
	if b.types == nil {
		return fmt.Errorf("mono: missing types interner")
	}

	normalized := NormalizeTypeArgs(b.types, typeArgs)
	requestedSym := origSym
	requestedArgs := slices.Clone(normalized)
	if b.closure != nil {
		retained, found, err := b.retainedCallableFor(origSym, normalized)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("mono: callable %s is not retained by the authoritative instantiation closure", b.monoName(requestedSym, requestedArgs))
		}
		origSym = retained.Template
		normalized = slices.Clone(retained.TemplateArgs)
	}
	expectedTypeArgs := b.symbolTypeParamCount(origSym)
	switch {
	case expectedTypeArgs == 0 && len(normalized) > 0:
		return fmt.Errorf("mono: non-generic symbol %d cannot be instantiated with type args", origSym)
	case expectedTypeArgs > 0 && len(normalized) != expectedTypeArgs:
		return fmt.Errorf("mono: symbol %d expects %d type args, got %d", origSym, expectedTypeArgs, len(normalized))
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
		return fmt.Errorf("mono: non-concrete type args for %s (sym=%d args=%s)%s", name, origSym, args, stackMsg)
	}
	key := MonoKey{Sym: origSym, ArgsKey: argsKeyFromTypes(normalized)}
	if existing := b.mm.Funcs[key]; existing != nil {
		return nil
	}

	if len(stack) >= b.opt.MaxDepth {
		return fmt.Errorf("mono: instantiation depth exceeded (%d)", b.opt.MaxDepth)
	}
	for _, k := range stack {
		if k == key {
			return fmt.Errorf("mono: instantiation cycle detected at sym=%d args=%s", key.Sym, key.ArgsKey)
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
	if err := b.mm.Callables.bind(origSym, normalized, instanceSym); err != nil {
		return err
	}

	origFn := b.origFuncBySym[origSym]
	if origFn == nil {
		// Imported/intrinsic function without HIR body.
		return nil
	}

	if origFn.IsGeneric() {
		if len(normalized) == 0 {
			return fmt.Errorf("mono: missing type args for generic function %s", origFn.Name)
		}
		if len(normalized) != len(origFn.GenericParams) {
			return fmt.Errorf("mono: generic function %s expects %d type args, got %d", origFn.Name, len(origFn.GenericParams), len(normalized))
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
		if params := b.templateParams[origSym]; len(params) > 0 {
			if len(params) != len(normalized) {
				return fmt.Errorf("mono: generic function %s exact parameter ABI expects %d type args, got %d", origFn.Name, len(params), len(normalized))
			}
			subst.ExactArgs = make(map[types.TypeID]types.TypeID, len(params))
			for i, param := range params {
				if param == types.NoTypeID {
					return fmt.Errorf("mono: generic function %s has a missing exact parameter descriptor at index %d", origFn.Name, i)
				}
				subst.ExactArgs[param] = normalized[i]
			}
		} else if b.closure != nil {
			return fmt.Errorf("mono: generic function %s has no exact parameter ABI", origFn.Name)
		} else if b.mod != nil && b.mod.Symbols != nil && b.mod.Symbols.Table != nil && b.mod.Symbols.Table.Symbols != nil {
			if owner := b.mod.Symbols.Table.Symbols.Get(origSym); owner != nil && len(owner.TypeParams) == len(normalized) {
				subst.NameArgs = make(map[source.StringID]types.TypeID, len(normalized))
				for i, name := range owner.TypeParams {
					if name != source.NoStringID && normalized[i] != types.NoTypeID {
						subst.NameArgs[name] = normalized[i]
					}
				}
			}
		}
		if subst.ExactArgs == nil {
			if recvSym := b.receiverTypeSymbol(origSym); recvSym.IsValid() && recvSym != origSym {
				subst.OwnerSyms = append(subst.OwnerSyms, recvSym)
			}
		}
		if err := subst.ApplyFunc(clone); err != nil {
			return err
		}
	}

	if err := b.rewriteCallsInFunc(clone, origSym, subst, append(stack, key)); err != nil {
		return err
	}
	if err := b.rewriteFuncValuesInFunc(clone, origSym, subst, append(stack, key)); err != nil {
		return err
	}

	out.Func = clone
	return nil
}
