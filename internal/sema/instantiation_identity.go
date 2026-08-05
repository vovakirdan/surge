package sema

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"fortio.org/safecast"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type nominalInstantiationDecl struct {
	kind types.Kind
	name string
	decl source.Span
}

// InstantiationIdentity provides the canonical, post-merge namespaces used by
// concrete instance keys and deterministic source witnesses.
type InstantiationIdentity struct {
	Types           types.CanonicalKeyContext
	ResolveTemplate func(symbols.SymbolID) (string, error)
	ResolveSource   func(source.FileID) (string, error)
}

// NewInstantiationKeyContext binds the shared type-key algorithm to the
// canonical post-merge symbol table. Nominal output uses module identity, not
// discovery-order-dependent FileID text.
func NewInstantiationKeyContext(
	typesIn *types.Interner,
	symbolResult *symbols.Result,
	resolveSource func(source.FileID) (string, error),
) (InstantiationIdentity, error) {
	if typesIn == nil {
		return InstantiationIdentity{}, fmt.Errorf("instantiation identity: missing type interner")
	}
	if typesIn.Strings == nil {
		return InstantiationIdentity{}, fmt.Errorf("instantiation identity: missing string interner")
	}
	if symbolResult == nil || symbolResult.Table == nil || symbolResult.Table.Symbols == nil {
		return InstantiationIdentity{}, fmt.Errorf("instantiation identity: missing post-merge symbol table")
	}

	identities := make(map[nominalInstantiationDecl]string)
	nominalRanks := make(map[nominalInstantiationDecl]int)
	templates := make(map[symbols.SymbolID]string)
	ownerIdentities := make(map[uint32]string)
	paramIdentities := make(map[types.TypeID]string)
	limit, err := safecast.Conv[uint32](symbolResult.Table.Symbols.Len())
	if err != nil {
		return InstantiationIdentity{}, fmt.Errorf("instantiation identity: symbol count overflow: %w", err)
	}
	for rawID := uint32(1); rawID <= limit; rawID++ {
		id := symbols.SymbolID(rawID)
		sym := symbolResult.Table.Symbols.Get(id)
		if sym == nil {
			continue
		}
		if sym.Kind == symbols.SymbolFunction || sym.Kind == symbols.SymbolTag {
			templateIdentity, identityErr := canonicalTemplateIdentity(typesIn, sym, resolveSource)
			if identityErr != nil {
				return InstantiationIdentity{}, fmt.Errorf("instantiation identity for symbol %d: %w", id, identityErr)
			}
			templates[id] = templateIdentity
			ownerIdentities[rawID] = "template/" + templateIdentity
		}
		if sym.Kind == symbols.SymbolTag {
			name := instantiationSymbolName(typesIn, sym)
			if name != "" {
				// A tag constructor owns a synthetic union type whose type
				// metadata has no source span. Key the metadata shape exactly,
				// but derive local identity from the real tag declaration.
				key := nominalInstantiationDecl{kind: types.KindUnion, name: name}
				identityDecl := key
				identityDecl.decl = sym.Span
				identity, identityErr := canonicalNominalIdentity(sym, identityDecl, resolveSource)
				if identityErr != nil {
					return InstantiationIdentity{}, fmt.Errorf("instantiation identity for tag union %d: %w", id, identityErr)
				}
				rank := canonicalNominalIdentityRank(sym, identityDecl, resolveSource)
				if bindErr := bindNominalInstantiationIdentity(identities, nominalRanks, key, identity, rank); bindErr != nil {
					return InstantiationIdentity{}, bindErr
				}
			}
		}
		if (sym.Kind != symbols.SymbolType && sym.Kind != symbols.SymbolTag) || sym.Type == types.NoTypeID {
			continue
		}
		key, ok := nominalInstantiationDeclForType(typesIn, sym.Type)
		if !ok {
			continue
		}
		identity, identityErr := canonicalNominalIdentity(sym, key, resolveSource)
		if identityErr != nil {
			return InstantiationIdentity{}, fmt.Errorf("instantiation identity for type symbol %d: %w", id, identityErr)
		}
		rank := canonicalNominalIdentityRank(sym, key, resolveSource)
		if bindErr := bindNominalInstantiationIdentity(identities, nominalRanks, key, identity, rank); bindErr != nil {
			return InstantiationIdentity{}, bindErr
		}
	}
	if err := bindBuiltinNominalIdentities(typesIn, symbolResult, identities, nominalRanks, paramIdentities); err != nil {
		return InstantiationIdentity{}, fmt.Errorf("instantiation identity: %w", err)
	}
	// Bind every nominal owner only after prelude/core aliases have selected
	// their one canonical module identity.
	for rawID := uint32(1); rawID <= limit; rawID++ {
		sym := symbolResult.Table.Symbols.Get(symbols.SymbolID(rawID))
		if sym == nil || (sym.Kind != symbols.SymbolType && sym.Kind != symbols.SymbolTag) || sym.Type == types.NoTypeID {
			continue
		}
		key, ok := nominalInstantiationDeclForType(typesIn, sym.Type)
		if !ok {
			continue
		}
		if identity := identities[key]; identity != "" && ownerIdentities[rawID] == "" {
			ownerIdentities[rawID] = "nominal/" + identity
		}
	}

	typeKeys := types.CanonicalKeyContext{
		Types: typesIn,
		ResolveNominal: func(kind types.Kind, name string, decl source.Span) (string, error) {
			identity, ok := identities[nominalInstantiationDecl{kind: kind, name: name, decl: decl}]
			if !ok {
				return "", fmt.Errorf("no canonical symbol for %s %q declared at %s", kind, name, decl)
			}
			return identity, nil
		},
		ResolveTypeParam: func(id types.TypeID, info types.TypeParamInfo) (string, error) {
			if identity := paramIdentities[id]; identity != "" {
				return identity, nil
			}
			identity, ok := ownerIdentities[info.Owner]
			if !ok {
				return "", fmt.Errorf("no canonical declaration for parameter owner %d", info.Owner)
			}
			return identity, nil
		},
	}
	return InstantiationIdentity{
		Types: typeKeys,
		ResolveTemplate: func(id symbols.SymbolID) (string, error) {
			identity, ok := templates[id]
			if !ok {
				return "", fmt.Errorf("no canonical generic template for symbol %d", id)
			}
			return identity, nil
		},
		ResolveSource: resolveSource,
	}, nil
}

func bindNominalInstantiationIdentity(
	identities map[nominalInstantiationDecl]string,
	ranks map[nominalInstantiationDecl]int,
	key nominalInstantiationDecl,
	identity string,
	rank int,
) error {
	if existing, found := identities[key]; found && existing != identity {
		switch {
		case ranks[key] < rank:
			identities[key] = identity
			ranks[key] = rank
		case ranks[key] > rank:
			// A merged table may retain a local/prelude view of the same
			// declaration. Keep its explicit module-owned identity.
		default:
			return fmt.Errorf("instantiation identity: declaration %s has conflicting identities %q and %q", key.name, existing, identity)
		}
		return nil
	}
	identities[key] = identity
	ranks[key] = rank
	return nil
}

func instantiationSymbolName(typesIn *types.Interner, sym *symbols.Symbol) string {
	if typesIn == nil || typesIn.Strings == nil || sym == nil {
		return ""
	}
	name, ok := typesIn.Strings.Lookup(sym.Name)
	if (!ok || name == "") && sym.ImportName != source.NoStringID {
		name, ok = typesIn.Strings.Lookup(sym.ImportName)
	}
	if !ok {
		return ""
	}
	return name
}

const builtinNominalIdentityRank = 5

// bindBuiltinNominalIdentities covers language-defined generic nominal types.
// Their exact parameter TypeIDs live in the shared interner, while the raw
// owner number may belong to a different module-local symbol table.
func bindBuiltinNominalIdentities(
	typesIn *types.Interner,
	symbolResult *symbols.Result,
	identities map[nominalInstantiationDecl]string,
	nominalRanks map[nominalInstantiationDecl]int,
	paramIdentities map[types.TypeID]string,
) error {
	builtins := []struct {
		name   string
		typeID types.TypeID
	}{
		{name: "Array", typeID: typesIn.ArrayNominalType()},
		{name: "ArrayFixed", typeID: typesIn.ArrayFixedNominalType()},
		{name: "Map", typeID: typesIn.MapNominalType()},
	}
	for _, builtin := range builtins {
		if builtin.typeID == types.NoTypeID {
			continue
		}
		decl, ok := nominalInstantiationDeclForType(typesIn, builtin.typeID)
		if !ok || decl.kind != types.KindStruct || decl.name != builtin.name {
			return fmt.Errorf("builtin nominal %q has invalid type metadata", builtin.name)
		}
		info, ok := typesIn.StructInfo(builtin.typeID)
		if !ok || info == nil || len(info.TypeParams) == 0 {
			return fmt.Errorf("builtin nominal %q has no parameter metadata", builtin.name)
		}

		owner := findBuiltinNominalOwner(symbolResult.Table, builtin.name)
		if owner == nil {
			return fmt.Errorf("builtin nominal %q has no canonical prelude symbol", builtin.name)
		}
		identity, identityErr := canonicalNominalIdentity(owner, decl, nil)
		if identityErr != nil {
			return fmt.Errorf("builtin nominal %q identity: %w", builtin.name, identityErr)
		}
		for _, paramID := range info.TypeParams {
			if _, found := typesIn.TypeParamInfo(paramID); !found {
				return fmt.Errorf("builtin nominal %q has an invalid parameter type#%d", builtin.name, paramID)
			}
			paramIdentities[paramID] = "nominal/" + identity
		}
		identities[decl] = identity
		nominalRanks[decl] = builtinNominalIdentityRank
	}
	return nil
}

func findBuiltinNominalOwner(table *symbols.Table, wantName string) *symbols.Symbol {
	if table == nil || table.Symbols == nil {
		return nil
	}
	for rawID := 1; rawID <= table.Symbols.Len(); rawID++ {
		sym := table.Symbols.Get(symbols.SymbolID(rawID))
		if isBuiltinNominalOwner(table, sym, wantName) {
			return sym
		}
	}
	return nil
}

func isBuiltinNominalOwner(table *symbols.Table, sym *symbols.Symbol, wantName string) bool {
	if table == nil || sym == nil || sym.Kind != symbols.SymbolType || sym.Flags&symbols.SymbolFlagBuiltin == 0 || sym.ModulePath != "" {
		return false
	}
	if table.Strings == nil {
		return false
	}
	name, ok := table.Strings.Lookup(sym.Name)
	return ok && name == wantName
}

func canonicalNominalIdentityRank(sym *symbols.Symbol, decl nominalInstantiationDecl, resolveSource func(source.FileID) (string, error)) int {
	if sym == nil {
		return 0
	}
	if sym.ModulePath != "" {
		if resolveSource != nil {
			if sourceKey, err := resolveSource(decl.decl.File); err == nil {
				module := normalizeInstantiationIdentityPart(sym.ModulePath)
				if sourceKey == module || strings.HasPrefix(sourceKey, module+"/") {
					return 4
				}
			}
		}
		if sym.Span == decl.decl {
			return 3
		}
		return 2
	}
	if sym.Flags&symbols.SymbolFlagBuiltin != 0 {
		return 0
	}
	if sym.Span == decl.decl {
		return 2
	}
	return 1
}

func canonicalNominalIdentity(
	sym *symbols.Symbol,
	decl nominalInstantiationDecl,
	resolveSource func(source.FileID) (string, error),
) (string, error) {
	if sym == nil {
		return "", fmt.Errorf("missing nominal symbol")
	}
	module := normalizeInstantiationIdentityPart(sym.ModulePath)
	if module == "" && sym.Flags&symbols.SymbolFlagBuiltin != 0 {
		module = "<builtin>"
	}
	sourceIdentity := ""
	if module == "" || sym.Flags&symbols.SymbolFlagFilePrivate != 0 {
		if resolveSource == nil {
			return "", fmt.Errorf("local nominal %q requires a canonical source resolver", decl.name)
		}
		var err error
		sourceIdentity, err = resolveSource(decl.decl.File)
		if err != nil {
			return "", fmt.Errorf("local nominal %q source: %w", decl.name, err)
		}
		sourceIdentity = normalizeInstantiationIdentityPart(sourceIdentity)
		if sourceIdentity == "" {
			return "", fmt.Errorf("local nominal %q has an empty source identity", decl.name)
		}
	}
	parts := []string{decl.kind.String(), module, decl.name, sourceIdentity}
	if sourceIdentity != "" {
		parts = append(parts, strconv.FormatUint(uint64(decl.decl.Start), 10), strconv.FormatUint(uint64(decl.decl.End), 10))
	}
	return encodeInstantiationIdentity(parts), nil
}

func canonicalTemplateIdentity(
	typesIn *types.Interner,
	sym *symbols.Symbol,
	resolveSource func(source.FileID) (string, error),
) (string, error) {
	if sym == nil {
		return "", fmt.Errorf("missing symbol")
	}
	name, ok := typesIn.Strings.Lookup(sym.Name)
	if (!ok || name == "") && sym.ImportName != source.NoStringID {
		name, ok = typesIn.Strings.Lookup(sym.ImportName)
	}
	if !ok || name == "" {
		name = "<anonymous>"
	}
	module := normalizeInstantiationIdentityPart(sym.ModulePath)
	if module == "" && sym.Flags&symbols.SymbolFlagBuiltin != 0 {
		module = "<builtin>"
	}
	local := module == "" || name == "<anonymous>" || sym.Flags&symbols.SymbolFlagFilePrivate != 0
	sourceIdentity := ""
	if local {
		if resolveSource == nil {
			return "", fmt.Errorf("local template %q requires a canonical source resolver", name)
		}
		var err error
		sourceIdentity, err = resolveSource(sym.Span.File)
		if err != nil {
			return "", fmt.Errorf("local template %q source: %w", name, err)
		}
		sourceIdentity = normalizeInstantiationIdentityPart(sourceIdentity)
		if sourceIdentity == "" {
			return "", fmt.Errorf("local template %q has an empty source identity", name)
		}
	}
	parts := []string{
		sym.Kind.String(),
		module,
		string(sym.ReceiverKey),
		name,
		canonicalSignatureIdentity(sym.Signature),
		strconv.Itoa(len(sym.TypeParams)),
		sourceIdentity,
	}
	if sourceIdentity != "" {
		parts = append(parts, strconv.FormatUint(uint64(sym.Span.Start), 10), strconv.FormatUint(uint64(sym.Span.End), 10))
	}
	return encodeInstantiationIdentity(parts), nil
}

func canonicalSignatureIdentity(sig *symbols.FunctionSignature) string {
	if sig == nil {
		return "-"
	}
	parts := make([]string, 0, len(sig.Params)+4)
	parts = append(parts, strconv.Itoa(len(sig.Params)))
	for i, param := range sig.Params {
		variadic := i < len(sig.Variadic) && sig.Variadic[i]
		parts = append(parts, string(param), strconv.FormatBool(variadic))
	}
	parts = append(parts, string(sig.Result), strconv.FormatBool(sig.HasSelf))
	return encodeInstantiationIdentity(parts)
}

func encodeInstantiationIdentity(parts []string) string {
	var out strings.Builder
	for _, part := range parts {
		fmt.Fprintf(&out, "%d:%s", len(part), part)
	}
	return out.String()
}

func normalizeInstantiationIdentityPart(value string) string {
	if value == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(value))
}

func nominalInstantiationDeclForType(typesIn *types.Interner, id types.TypeID) (nominalInstantiationDecl, bool) {
	t, ok := typesIn.Lookup(id)
	if !ok {
		return nominalInstantiationDecl{}, false
	}
	var nameID source.StringID
	var decl source.Span
	switch t.Kind {
	case types.KindStruct:
		info, found := typesIn.StructInfo(id)
		if !found || info == nil {
			return nominalInstantiationDecl{}, false
		}
		nameID, decl = info.Name, info.Decl
	case types.KindUnion:
		info, found := typesIn.UnionInfo(id)
		if !found || info == nil {
			return nominalInstantiationDecl{}, false
		}
		nameID, decl = info.Name, info.Decl
	case types.KindAlias:
		info, found := typesIn.AliasInfo(id)
		if !found || info == nil {
			return nominalInstantiationDecl{}, false
		}
		nameID, decl = info.Name, info.Decl
	case types.KindEnum:
		info, found := typesIn.EnumInfo(id)
		if !found || info == nil {
			return nominalInstantiationDecl{}, false
		}
		nameID, decl = info.Name, info.Decl
	default:
		return nominalInstantiationDecl{}, false
	}
	name, found := typesIn.Strings.Lookup(nameID)
	if !found || name == "" {
		return nominalInstantiationDecl{}, false
	}
	return nominalInstantiationDecl{kind: t.Kind, name: name, decl: decl}, true
}
