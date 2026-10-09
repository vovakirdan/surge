package sema

import (
	"slices"
	"strings"

	"surge/internal/symbols"
	"surge/internal/types"
)

// A retained body-less core intrinsic, by its original declaration and publication:
// builtin, intrinsic, synchronous, from core/intrinsics at this template and receiver
// arity, with no defaults or variadics, published under its own body key. Its
// return-source promise is its reader's to check. A name alone selects nothing.
func returnOriginCoreIntrinsicDeclaration(fn *returnOriginFunction, templates, receiverArity int) bool {
	if fn == nil || fn.info == nil || fn.item == nil || fn.candidate == nil {
		return false
	}
	c, u := fn.candidate, fn.unit
	if !c.Builtin || !c.Intrinsic || c.HasBody || c.Async || fn.item.Body.IsValid() || c.Name != fn.name ||
		c.SourceKey != "builtin" || c.ModulePath != "core/intrinsics" || u.SourceKey != "core/intrinsics.sg" ||
		len(c.TemplateParams) != templates || c.ReceiverTemplateArity != receiverArity ||
		len(c.Defaults) != len(fn.info.Params) || len(c.Variadic) != len(fn.info.Params) ||
		slices.Contains(c.Defaults, true) || slices.Contains(c.Variadic, true) {
		return false
	}
	identity, err := u.owningCallableIdentity(fn.item, fn.symbol, fn.info)
	return err == nil && identity.BodyKey == fn.key && identity.SourceKey == fn.canonicalSourceKey
}

// A retained body-less intrinsic from one exact standard-library source. The
// callable is published in the builtin namespace, while the indexed physical
// declaration stays tied to that source unit. The loader's builtin mark and the
// original publication prevent a user module with the same name/signature from
// acquiring the certificate.
func returnOriginStdlibIntrinsicDeclaration(fn *returnOriginFunction, sourceKey string) bool {
	if fn == nil || fn.info == nil || fn.item == nil || fn.candidate == nil {
		return false
	}
	c, u := fn.candidate, fn.unit
	sym := u.Symbols.Table.Symbols.Get(fn.symbol)
	if !c.Builtin || !c.Intrinsic || c.HasBody || c.Async || fn.item.Body.IsValid() || c.Name != fn.name ||
		c.SourceKey != "builtin" || u.SourceKey != sourceKey && !strings.HasSuffix(u.SourceKey, "/"+sourceKey) ||
		len(c.TemplateParams) != 0 || c.ReceiverTemplateArity != 0 || c.HasSelf || c.ReceiverType != types.NoTypeID ||
		len(c.Defaults) != len(fn.info.Params) || len(c.Variadic) != len(fn.info.Params) ||
		slices.Contains(c.Defaults, true) || slices.Contains(c.Variadic, true) ||
		sym == nil || sym.Flags&symbols.SymbolFlagBuiltin == 0 {
		return false
	}
	identity, err := u.owningCallableIdentity(fn.item, fn.symbol, fn.info)
	return err == nil && identity.BodyKey == fn.key && identity.SourceKey == fn.canonicalSourceKey
}
