package sema

import (
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// collectLayoutObligations covers every concrete type surface that survives
// sema or directs later monomorphization. It intentionally does not scan the
// whole interner: unused generic declarations may remain deferred.
func (tc *typeChecker) collectLayoutObligations() []layoutObligation {
	if tc == nil || tc.result == nil {
		return nil
	}
	capacity := len(tc.typeIDItems) + len(tc.result.ExprTypes) + len(tc.result.BindingTypes)
	out := make([]layoutObligation, 0, capacity)
	add := func(typeID types.TypeID, span source.Span) {
		out = append(out, layoutObligation{typeID: typeID, span: span})
	}

	for typeID, itemID := range tc.typeIDItems {
		span := tc.itemSpan(itemID)
		if span == (source.Span{}) {
			span = tc.fallbackTypeSpan(typeID)
		}
		add(typeID, span)
	}
	for exprID, typeID := range tc.result.ExprTypes {
		add(typeID, tc.exprSpan(exprID))
	}
	for exprID, typeID := range tc.result.RangeTypes {
		add(typeID, tc.exprSpan(exprID))
	}
	for symbolID, typeID := range tc.result.BindingTypes {
		add(typeID, tc.symbolSpan(symbolID))
	}
	for exprID, operand := range tc.result.IsOperands {
		add(operand.Type, tc.exprSpan(exprID))
	}
	for exprID, operand := range tc.result.HeirOperands {
		span := tc.exprSpan(exprID)
		add(operand.Left, span)
		add(operand.Right, span)
	}
	for _, conversion := range tc.result.ImplicitConversions {
		add(conversion.Source, conversion.Span)
		add(conversion.Target, conversion.Span)
	}
	tc.appendSignatureObligations(&out)
	// A generic body is checked once with Deferred parameterized layouts, but
	// each concrete call creates fresh storage obligations inside that body.
	// Keep the raw obligations quiet, then replay them through the exact
	// function substitution at the call site before MIR/mono can observe them.
	raw := append([]layoutObligation(nil), out...)
	tc.appendFunctionInstantiationObligations(&out, raw)
	return out
}

func (tc *typeChecker) symbolSpan(id symbols.SymbolID) source.Span {
	if sym := tc.symbolFromID(id); sym != nil {
		return sym.Span
	}
	return source.Span{}
}

func (tc *typeChecker) appendFunctionInstantiationObligations(out *[]layoutObligation, raw []layoutObligation) {
	for symbolID, instances := range tc.result.FunctionInstantiations {
		fallback := tc.symbolSpan(symbolID)
		sites := tc.result.FunctionInstantiationSites[symbolID]
		for index, args := range instances {
			span := fallback
			if index < len(sites) && sites[index] != (source.Span{}) {
				span = sites[index]
			}
			for _, typeID := range args {
				*out = append(*out, layoutObligation{typeID: typeID, span: span})
			}
			for _, obligation := range raw {
				substitutions := collectLayoutSubstitutions(tc.types, symbolID, args, obligation.typeID)
				if len(substitutions) == 0 {
					continue
				}
				*out = append(*out, layoutObligation{
					typeID:        obligation.typeID,
					span:          span,
					substitutions: substitutions,
				})
			}
		}
	}
}

func (tc *typeChecker) appendSignatureObligations(out *[]layoutObligation) {
	if tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Symbols == nil || tc.types == nil {
		return
	}
	file := tc.builder.Files.Get(tc.fileID)
	if file == nil {
		return
	}
	for index := range tc.symbols.Table.Symbols.Data() {
		symbolID := symbols.SymbolID(index + 1) // arena slot zero is reserved
		sym := tc.symbolFromID(symbolID)
		if sym == nil || sym.Span.File != file.Span.File {
			continue
		}
		info, ok := tc.types.FnInfo(sym.Type)
		if !ok || info == nil {
			continue
		}
		for _, param := range info.Params {
			*out = append(*out, layoutObligation{typeID: param, span: sym.Span})
		}
		*out = append(*out, layoutObligation{typeID: info.Result, span: sym.Span})
	}
}
