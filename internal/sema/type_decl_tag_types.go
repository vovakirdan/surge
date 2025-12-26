package sema

import (
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) resolveTagType(symID symbols.SymbolID, name source.StringID, args []types.TypeID, argSpans []source.Span, span source.Span) types.TypeID {
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return types.NoTypeID
	}
	expected := len(sym.TypeParams)
	if expected == 0 {
		if len(args) > 0 {
			tc.report(diag.SemaTypeMismatch, span, "%s does not take type arguments", tc.lookupName(sym.Name))
			return types.NoTypeID
		}
		return tc.instantiateTagType(name, nil)
	}
	if len(args) == 0 {
		tc.report(diag.SemaTypeMismatch, span, "%s requires %d type argument(s)", tc.lookupName(sym.Name), expected)
		return types.NoTypeID
	}
	if len(args) != expected {
		tc.report(diag.SemaTypeMismatch, span, "%s expects %d type argument(s), got %d", tc.lookupName(sym.Name), expected, len(args))
		return types.NoTypeID
	}
	for i, tp := range sym.TypeParamSymbols {
		if i >= len(args) {
			break
		}
		if tp.IsConst {
			if !tc.constArgAcceptable(args[i], tp.ConstType) {
				argLabel := tc.typeLabel(args[i])
				argSpan := span
				if i < len(argSpans) && argSpans[i] != (source.Span{}) {
					argSpan = argSpans[i]
				}
				tc.report(diag.SemaTypeMismatch, argSpan, "%s requires const argument %s for %s", tc.lookupName(sym.Name), tc.lookupName(tp.Name), argLabel)
				return types.NoTypeID
			}
		}
	}
	tc.enforceTypeArgBounds(sym, args, argSpans, span)
	return tc.instantiateTagType(name, args)
}

func (tc *typeChecker) resolveImportedTagType(tag *symbols.ExportedSymbol, name source.StringID, args []types.TypeID, argSpans []source.Span, span source.Span) types.TypeID {
	if tag == nil {
		return types.NoTypeID
	}
	expected := len(tag.TypeParams)
	displayName := tc.lookupName(name)
	if displayName == "" {
		displayName = tag.Name
	}
	if expected == 0 {
		if len(args) > 0 {
			tc.report(diag.SemaTypeMismatch, span, "%s does not take type arguments", displayName)
			return types.NoTypeID
		}
		return tc.instantiateTagType(tc.importedTagName(tag, name), nil)
	}
	if len(args) == 0 {
		tc.report(diag.SemaTypeMismatch, span, "%s requires %d type argument(s)", displayName, expected)
		return types.NoTypeID
	}
	if len(args) != expected {
		tc.report(diag.SemaTypeMismatch, span, "%s expects %d type argument(s), got %d", displayName, expected, len(args))
		return types.NoTypeID
	}
	for i, tp := range tag.TypeParamSyms {
		if i >= len(args) {
			break
		}
		if tp.IsConst {
			if !tc.constArgAcceptable(args[i], tp.ConstType) {
				argLabel := tc.typeLabel(args[i])
				argSpan := span
				if i < len(argSpans) && argSpans[i] != (source.Span{}) {
					argSpan = argSpans[i]
				}
				tc.report(diag.SemaTypeMismatch, argSpan, "%s requires const argument %s for %s", displayName, tc.lookupName(tp.Name), argLabel)
				return types.NoTypeID
			}
		}
	}
	return tc.instantiateTagType(tc.importedTagName(tag, name), args)
}

func (tc *typeChecker) importedTagName(tag *symbols.ExportedSymbol, fallback source.StringID) source.StringID {
	if tag == nil || tc.builder == nil || tc.builder.StringsInterner == nil {
		return fallback
	}
	if tag.Name == "" {
		return fallback
	}
	return tc.builder.StringsInterner.Intern(tag.Name)
}
