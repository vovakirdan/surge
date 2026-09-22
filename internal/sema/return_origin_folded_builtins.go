package sema

import (
	"slices"
	"strings"

	"surge/internal/symbols"
	"surge/internal/types"
)

// foldedBuiltinDeclaration says that a body-less builtin declaration of this unit
// is one the merged catalog folded into the standard library's record of the same
// operation (dedupeBuiltinOperations): a copy of `core` read from elsewhere in the
// tree spells every intrinsic out again. Such a declaration names no body and no
// promise of its own, so the unit registers nothing for it, and a call to it is
// refused where it is made: the selected symbol reaches no published record.
// Anything else keeps the hard identity error: a record that survived, an incomplete
// or ambiguous local identity, or an
// operation without exactly one standard-library record that agrees with the
// declaration's own typed signature.
func (u *returnOriginUnitIndex) foldedBuiltinDeclaration(id symbols.SymbolID, sym *symbols.Symbol, info *types.FnInfo) bool {
	if sym.Flags&symbols.SymbolFlagBuiltin == 0 || sym.Signature.HasBody {
		return false
	}
	var local FinalizationCallableIdentity
	for _, identity := range u.Publication.LocalCallables {
		if identity.Symbol != id {
			continue
		}
		if local.BodyKey != "" {
			return false
		}
		local = identity
	}
	operation, named := returnOriginBodyKeyOperation(local.BodyKey)
	if !named || local.SourceKey == "" {
		return false
	}
	survivors := 0
	for i := range u.authority.CallableCandidates {
		c := &u.authority.CallableCandidates[i]
		if c.BodyKey == local.BodyKey && c.SourceKey == local.SourceKey {
			return false
		}
		other, found := returnOriginBodyKeyOperation(c.BodyKey)
		if found && other == operation && c.Builtin && !c.HasBody && isCoreRuntimeModulePath(c.ModulePath) &&
			slices.Equal(c.ParamTypes, info.Params) && c.ResultType == info.Result {
			survivors++
		}
	}
	return survivors == 1
}

// returnOriginBodyKeyOperation drops the module path and the source span a body key
// begins with (canonicalCallableBodyKey: "path|source:start:end|…"), leaving what the
// declaration itself says: receiver, name, signature, attributes and promises.
func returnOriginBodyKeyOperation(key string) (string, bool) {
	parts := strings.SplitN(key, "|", 3)
	if len(parts) != 3 || parts[0] == "" {
		return "", false
	}
	return parts[2], true
}
