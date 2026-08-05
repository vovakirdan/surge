package sema

import (
	"fmt"
	"slices"

	"fortio.org/safecast"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) rememberInstantiationTemplateParams(template symbols.SymbolID) {
	if tc == nil || tc.result == nil || !template.IsValid() {
		return
	}
	bindings := tc.instantiationCallerBindings(template)
	if len(bindings) == 0 {
		return
	}
	params := make([]types.TypeID, len(bindings))
	for _, binding := range bindings {
		if int(binding.ArgIndex) >= len(params) || binding.Param == types.NoTypeID {
			return
		}
		params[binding.ArgIndex] = binding.Param
	}
	if tc.result.InstantiationTemplateParams == nil {
		tc.result.InstantiationTemplateParams = make(map[symbols.SymbolID][]types.TypeID)
	}
	if existing := tc.result.InstantiationTemplateParams[template]; len(existing) > 0 && !slices.Equal(existing, params) {
		panic(fmt.Errorf("generic template %d has conflicting exact parameter descriptors", template))
	}
	tc.result.InstantiationTemplateParams[template] = params
}

func (tc *typeChecker) rememberInstantiationCallableSeed(callable symbols.SymbolID) {
	if tc == nil || tc.result == nil || !callable.IsValid() {
		return
	}
	sym := tc.symbolFromID(callable)
	if sym == nil || sym.Kind != symbols.SymbolFunction || len(sym.TypeParams) != 0 {
		return
	}
	if tc.result.InstantiationCallableSeeds == nil {
		tc.result.InstantiationCallableSeeds = make(map[symbols.SymbolID]struct{})
	}
	tc.result.InstantiationCallableSeeds[callable] = struct{}{}
}

func (tc *typeChecker) rememberFunctionInstantiation(symID symbols.SymbolID, args []types.TypeID, site source.Span, note string) {
	if !symID.IsValid() || len(args) == 0 || tc.result == nil {
		return
	}

	caller := tc.currentFnSym()
	kind := InstantiationFunction
	if sym := tc.symbolFromID(symID); sym != nil && sym.Kind == symbols.SymbolTag {
		kind = InstantiationTag
	}
	witness := InstantiationWitness{Site: site, Caller: caller, Reason: note}
	if tc.isGenericInstantiationCaller(caller) {
		callerArity, err := safecast.Conv[uint32](len(tc.symbolFromID(caller).TypeParams))
		if err != nil {
			panic(fmt.Errorf("instantiation caller arity overflow: %w", err))
		}
		bindings := tc.instantiationCallerBindings(caller)
		tc.result.InstantiationGraph.recordEdge(&InstantiationEdge{
			Kind:                kind,
			Caller:              caller,
			CallerTemplateArity: callerArity,
			CallerBindings:      bindings,
			Callee:              symID,
			CalleeTemplateArgs:  args,
			Witness:             witness,
		})
	} else {
		tc.result.InstantiationGraph.recordRoot(&InstantiationRoot{
			Kind:         kind,
			Template:     symID,
			TemplateArgs: args,
			Witness:      witness,
		})
	}

	if tc.insts != nil {
		if kind == InstantiationTag {
			tc.insts.RecordTagInstantiation(symID, args, site, caller, note)
		} else {
			tc.insts.RecordFnInstantiation(symID, args, site, caller, note)
		}
	}
}

func (tc *typeChecker) isGenericInstantiationCaller(caller symbols.SymbolID) bool {
	if tc == nil || !caller.IsValid() {
		return false
	}
	sym := tc.symbolFromID(caller)
	return sym != nil && (len(sym.TypeParams) > 0 || len(sym.TypeParamSymbols) > 0)
}

func (tc *typeChecker) instantiationCallerBindings(caller symbols.SymbolID) []InstantiationParamBinding {
	if tc == nil || tc.types == nil {
		return nil
	}
	sym := tc.symbolFromID(caller)
	if sym == nil || len(sym.TypeParams) == 0 {
		return nil
	}
	bindings := make([]InstantiationParamBinding, 0, len(sym.TypeParams))
	searchFrom := 0
	for argIndex, name := range sym.TypeParams {
		argPosition, err := safecast.Conv[uint32](argIndex)
		if err != nil {
			panic(fmt.Errorf("instantiation argument index overflow: %w", err))
		}
		for stackIndex := searchFrom; stackIndex < len(tc.typeParamStack); stackIndex++ {
			param := tc.typeParamStack[stackIndex]
			if tc.typeParamNames[param] != name {
				continue
			}
			info, ok := tc.types.TypeParamInfo(param)
			if !ok || info == nil {
				continue
			}
			bindings = append(bindings, InstantiationParamBinding{
				Param:      param,
				Owner:      symbols.SymbolID(info.Owner),
				ParamIndex: info.Index,
				ArgIndex:   argPosition,
			})
			searchFrom = stackIndex + 1
			break
		}
	}
	return bindings
}
