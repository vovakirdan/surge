package sema

import "slices"

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
