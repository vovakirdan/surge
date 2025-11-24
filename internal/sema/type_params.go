package sema

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type genericParamKind uint8

const (
	paramKindType genericParamKind = iota
	paramKindConst
)

type genericParamSpec struct {
	name      source.StringID
	kind      genericParamKind
	constType types.TypeID
}

// pushTypeParams installs generic parameters into the current environment and records their bounds.
func (tc *typeChecker) pushTypeParams(owner symbols.SymbolID, params []genericParamSpec, bindings []types.TypeID) bool {
	if len(params) == 0 || tc.types == nil {
		return false
	}
	if len(bindings) > 0 && len(bindings) != len(params) {
		return false
	}
	var ownerBounds map[source.StringID][]symbols.BoundInstance
	if owner.IsValid() {
		if sym := tc.symbolFromID(owner); sym != nil && len(sym.TypeParamSymbols) > 0 {
			ownerBounds = make(map[source.StringID][]symbols.BoundInstance, len(sym.TypeParamSymbols))
			for _, tp := range sym.TypeParamSymbols {
				ownerBounds[tp.Name] = tp.Bounds
			}
		}
	}
	scope := make(map[source.StringID]types.TypeID, len(params))
	tc.typeParamMarks = append(tc.typeParamMarks, len(tc.typeParamStack))
	for i, param := range params {
		var id types.TypeID
		if len(bindings) > 0 {
			id = bindings[i]
		} else {
			ui32, err := safecast.Conv[uint32](i)
			if err != nil {
				panic(fmt.Errorf("type param index overflow: %w", err))
			}
			isConst := param.kind == paramKindConst
			id = tc.types.RegisterTypeParam(param.name, uint32(owner), ui32, isConst, param.constType)
			tc.typeParamNames[id] = param.name
		}
		scope[param.name] = id
		tc.typeParamStack = append(tc.typeParamStack, id)
		if ownerBounds != nil {
			if bounds := ownerBounds[param.name]; len(bounds) > 0 {
				tc.typeParamBounds[id] = bounds
			}
		}
	}
	tc.typeParams = append(tc.typeParams, scope)
	tc.typeParamEnv = append(tc.typeParamEnv, tc.nextParamEnv)
	tc.nextParamEnv++
	return true
}

// applyTypeParamBounds applies already-resolved bounds from the owner symbol onto the current env ids.
func (tc *typeChecker) applyTypeParamBounds(owner symbols.SymbolID) {
	if !owner.IsValid() {
		return
	}
	sym := tc.symbolFromID(owner)
	if sym == nil || len(sym.TypeParamSymbols) == 0 {
		return
	}
	for _, tp := range sym.TypeParamSymbols {
		if id := tc.lookupTypeParam(tp.Name); id != types.NoTypeID {
			tc.typeParamBounds[id] = tp.Bounds
		}
	}
}

func (tc *typeChecker) popTypeParams() {
	if len(tc.typeParams) == 0 {
		return
	}
	tc.typeParams = tc.typeParams[:len(tc.typeParams)-1]
	if len(tc.typeParamEnv) > 0 {
		tc.typeParamEnv = tc.typeParamEnv[:len(tc.typeParamEnv)-1]
	}
	if len(tc.typeParamMarks) > 0 {
		start := tc.typeParamMarks[len(tc.typeParamMarks)-1]
		tc.typeParamMarks = tc.typeParamMarks[:len(tc.typeParamMarks)-1]
		if start >= 0 && start <= len(tc.typeParamStack) {
			for i := len(tc.typeParamStack) - 1; i >= start; i-- {
				id := tc.typeParamStack[i]
				delete(tc.typeParamBounds, id)
			}
			tc.typeParamStack = tc.typeParamStack[:start]
		}
	}
}

func (tc *typeChecker) currentTypeParamEnv() uint32 {
	if len(tc.typeParamEnv) == 0 {
		return 0
	}
	return tc.typeParamEnv[len(tc.typeParamEnv)-1]
}

func (tc *typeChecker) lookupTypeParam(name source.StringID) types.TypeID {
	if name == source.NoStringID {
		return types.NoTypeID
	}
	for i := len(tc.typeParams) - 1; i >= 0; i-- {
		scope := tc.typeParams[i]
		if id, ok := scope[name]; ok {
			return id
		}
	}
	return types.NoTypeID
}

func specsFromNames(names []source.StringID) []genericParamSpec {
	if len(names) == 0 {
		return nil
	}
	specs := make([]genericParamSpec, 0, len(names))
	for _, n := range names {
		if n == source.NoStringID {
			continue
		}
		specs = append(specs, genericParamSpec{name: n, kind: paramKindType})
	}
	return specs
}

func (tc *typeChecker) specsFromTypeParams(ids []ast.TypeParamID, scope symbols.ScopeID) []genericParamSpec {
	if tc.builder == nil || len(ids) == 0 {
		return nil
	}
	scope = tc.scopeOrFile(scope)
	specs := make([]genericParamSpec, 0, len(ids))
	for _, pid := range ids {
		param := tc.builder.Items.TypeParam(pid)
		if param == nil {
			continue
		}
		spec := genericParamSpec{
			name: param.Name,
			kind: paramKindType,
		}
		if param.IsConst {
			spec.kind = paramKindConst
			if param.ConstType.IsValid() {
				spec.constType = tc.resolveTypeExprWithScope(param.ConstType, scope)
			}
			if spec.constType == types.NoTypeID && tc.types != nil {
				spec.constType = tc.types.Builtins().Int
			}
		}
		specs = append(specs, spec)
	}
	return specs
}

func specsFromSymbolParams(params []symbols.TypeParamSymbol) []genericParamSpec {
	if len(params) == 0 {
		return nil
	}
	specs := make([]genericParamSpec, 0, len(params))
	for _, p := range params {
		kind := paramKindType
		if p.IsConst {
			kind = paramKindConst
		}
		specs = append(specs, genericParamSpec{
			name:      p.Name,
			kind:      kind,
			constType: p.ConstType,
		})
	}
	return specs
}
