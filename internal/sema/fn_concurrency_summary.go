package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// LockFieldInfo stores lock field information including the field name and lock kind.
// The kind is determined by the field's type: Mutex -> LockKindMutex, RwLock -> LockKindRwWrite.
type LockFieldInfo struct {
	FieldName source.StringID
	Kind      LockKind
}

// FnConcurrencySummary captures concurrency behavior of a function from its attributes.
// This is used for inter-procedural lock contract checking at call sites.
type FnConcurrencySummary struct {
	// RequiresLocks: lock fields from @requires_lock attributes.
	// Caller must hold these locks before calling.
	RequiresLocks []LockFieldInfo

	// AcquiresLocks: lock fields from @acquires_lock attributes.
	// Function acquires these locks; caller must NOT hold them.
	AcquiresLocks []LockFieldInfo

	// ReleasesLocks: lock fields from @releases_lock attributes.
	// Function releases these locks; caller must hold them.
	ReleasesLocks []LockFieldInfo

	// IsNonblocking: true if function has @nonblocking attribute.
	// Function must not call blocking operations.
	IsNonblocking bool

	// WaitsOn: field names from @waits_on attributes.
	// Function may block waiting on these conditions/semaphores.
	WaitsOn []source.StringID
}

// buildFnConcurrencySummary extracts concurrency attributes from a function declaration.
// receiverTypeID is used to determine lock kinds from field types.
// Returns nil if the function has no concurrency-related attributes.
func (tc *typeChecker) buildFnConcurrencySummary(fnItem *ast.FnItem, receiverTypeID types.TypeID) *FnConcurrencySummary {
	if fnItem == nil {
		return nil
	}

	infos := tc.collectAttrs(fnItem.AttrStart, fnItem.AttrCount)
	if len(infos) == 0 {
		return nil
	}

	var summary FnConcurrencySummary
	hasAnyAttr := false

	for _, info := range infos {
		switch info.Spec.Name {
		case "requires_lock":
			if fieldName := tc.extractLockAttrFieldName(info); fieldName != 0 {
				kind := tc.getLockKindForField(receiverTypeID, fieldName)
				summary.RequiresLocks = append(summary.RequiresLocks, LockFieldInfo{fieldName, kind})
				hasAnyAttr = true
			}

		case "acquires_lock":
			if fieldName := tc.extractLockAttrFieldName(info); fieldName != 0 {
				kind := tc.getLockKindForField(receiverTypeID, fieldName)
				summary.AcquiresLocks = append(summary.AcquiresLocks, LockFieldInfo{fieldName, kind})
				hasAnyAttr = true
			}

		case "releases_lock":
			if fieldName := tc.extractLockAttrFieldName(info); fieldName != 0 {
				kind := tc.getLockKindForField(receiverTypeID, fieldName)
				summary.ReleasesLocks = append(summary.ReleasesLocks, LockFieldInfo{fieldName, kind})
				hasAnyAttr = true
			}

		case "nonblocking":
			summary.IsNonblocking = true
			hasAnyAttr = true

		case "waits_on":
			if fieldName := tc.extractLockAttrFieldName(info); fieldName != 0 {
				summary.WaitsOn = append(summary.WaitsOn, fieldName)
				hasAnyAttr = true
			}
		}
	}

	if !hasAnyAttr {
		return nil
	}

	return &summary
}

// getLockKindForField determines the lock kind based on the field's type.
// Returns LockKindMutex for Mutex fields, LockKindRwWrite for RwLock fields.
// For RwLock, we use LockKindRwWrite as the most restrictive kind.
func (tc *typeChecker) getLockKindForField(ownerTypeID types.TypeID, fieldName source.StringID) LockKind {
	if ownerTypeID == types.NoTypeID {
		return LockKindMutex // Default to mutex when type unknown
	}

	// Look up the field type
	structInfo, ok := tc.types.StructInfo(ownerTypeID)
	if !ok || structInfo == nil {
		return LockKindMutex
	}

	fieldNameStr := tc.lookupName(fieldName)
	for _, field := range structInfo.Fields {
		if tc.lookupName(field.Name) == fieldNameStr {
			typeName := tc.typeLabel(field.Type)
			if typeName == "RwLock" {
				return LockKindRwWrite // Use write lock as the more restrictive kind
			}
			return LockKindMutex
		}
	}

	return LockKindMutex // Default to mutex if field not found
}

// extractLockAttrFieldName extracts field name StringID from an attribute argument.
// Used for @requires_lock, @acquires_lock, @releases_lock, @waits_on attributes.
func (tc *typeChecker) extractLockAttrFieldName(info AttrInfo) source.StringID {
	if len(info.Args) == 0 {
		return 0
	}
	argExpr := tc.builder.Exprs.Get(info.Args[0])
	if argExpr == nil || argExpr.Kind != ast.ExprLit {
		return 0
	}
	lit, ok := tc.builder.Exprs.Literal(info.Args[0])
	if !ok || lit.Kind != ast.ExprLitString {
		return 0
	}
	// Get the field name - strip quotes from string literal
	fieldNameRaw := tc.lookupName(lit.Value)
	if len(fieldNameRaw) < 2 {
		return 0
	}
	fieldNameStr := fieldNameRaw[1 : len(fieldNameRaw)-1] // Remove quotes
	if fieldNameStr == "" {
		return 0
	}
	return tc.builder.StringsInterner.Intern(fieldNameStr)
}

// typeIDFromReceiverKey converts a symbol's ReceiverKey to a TypeID.
// This is used to look up the struct type for determining field lock kinds.
func (tc *typeChecker) typeIDFromReceiverKey(receiverKey symbols.TypeKey) types.TypeID {
	if receiverKey == "" {
		return types.NoTypeID
	}

	// Strip reference prefix (&mut or &)
	typeName := string(receiverKey)
	if len(typeName) > 5 && typeName[:5] == "&mut " {
		typeName = typeName[5:]
	} else if len(typeName) > 1 && typeName[0] == '&' {
		typeName = typeName[1:]
	}

	// Search for the type symbol by name
	if tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Symbols == nil {
		return types.NoTypeID
	}

	data := tc.symbols.Table.Symbols.Data()
	for i := range data {
		sym := &data[i]
		if sym.Kind != symbols.SymbolType {
			continue
		}
		symName := tc.symbolName(sym.Name)
		if symName == typeName {
			return sym.Type
		}
	}

	return types.NoTypeID
}

// getFnConcurrencySummary retrieves or builds the concurrency summary for a function.
// Returns nil if the function has no concurrency attributes.
func (tc *typeChecker) getFnConcurrencySummary(symID symbols.SymbolID) *FnConcurrencySummary {
	if tc.fnConcurrencySummaries == nil {
		return nil
	}

	// Check cache first
	if summary, ok := tc.fnConcurrencySummaries[symID]; ok {
		return summary
	}

	// Not in cache - need to find the function and build summary
	fnItem := tc.findFnItemBySymbol(symID)
	if fnItem == nil {
		return nil
	}

	// Get receiver type from symbol's ReceiverKey to determine field lock kinds
	var receiverTypeID types.TypeID
	if tc.symbols != nil && tc.symbols.Table != nil && tc.symbols.Table.Symbols != nil {
		sym := tc.symbols.Table.Symbols.Get(symID)
		if sym != nil && sym.ReceiverKey != "" {
			receiverTypeID = tc.typeIDFromReceiverKey(sym.ReceiverKey)
		}
	}

	summary := tc.buildFnConcurrencySummary(fnItem, receiverTypeID)
	tc.fnConcurrencySummaries[symID] = summary
	return summary
}

// findFnItemBySymbol finds the FnItem for a given function symbol ID.
// This is used to lazily build concurrency summaries for called functions.
func (tc *typeChecker) findFnItemBySymbol(symID symbols.SymbolID) *ast.FnItem {
	if tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Symbols == nil {
		return nil
	}

	sym := tc.symbols.Table.Symbols.Get(symID)
	if sym == nil || sym.Kind != symbols.SymbolFunction {
		return nil
	}

	// For regular functions, Decl.Item is the function item
	if sym.Decl.Item.IsValid() {
		if fnItem, ok := tc.builder.Items.Fn(sym.Decl.Item); ok && fnItem != nil {
			return fnItem
		}

		// For extern methods, Decl.Item is the extern block - search its members
		if block, ok := tc.builder.Items.Extern(sym.Decl.Item); ok && block != nil {
			return tc.findFnInExternBlock(block, sym.Name)
		}
	}

	return nil
}

// findFnInExternBlock finds a function with the given name in an extern block.
func (tc *typeChecker) findFnInExternBlock(block *ast.ExternBlock, fnName source.StringID) *ast.FnItem {
	if block.MembersCount == 0 || !block.MembersStart.IsValid() {
		return nil
	}

	start := uint32(block.MembersStart)
	for offset := range block.MembersCount {
		memberID := ast.ExternMemberID(start + offset)
		member := tc.builder.Items.ExternMember(memberID)
		if member == nil || member.Kind != ast.ExternMemberFn {
			continue
		}
		fnItem := tc.builder.Items.FnByPayload(member.Fn)
		if fnItem == nil {
			continue
		}
		if fnItem.Name == fnName {
			return fnItem
		}
	}
	return nil
}

// HasConcurrencyContract returns true if function has any lock-related attributes.
func (s *FnConcurrencySummary) HasConcurrencyContract() bool {
	if s == nil {
		return false
	}
	return len(s.RequiresLocks) > 0 || len(s.AcquiresLocks) > 0 || len(s.ReleasesLocks) > 0
}

// MayBlock returns true if function has @waits_on attribute (may block).
func (s *FnConcurrencySummary) MayBlock() bool {
	if s == nil {
		return false
	}
	return len(s.WaitsOn) > 0
}

// fnHasNonblocking checks if a function has the @nonblocking attribute.
func (tc *typeChecker) fnHasNonblocking(fnItem *ast.FnItem) bool {
	if fnItem == nil {
		return false
	}
	infos := tc.collectAttrs(fnItem.AttrStart, fnItem.AttrCount)
	_, found := hasAttr(infos, "nonblocking")
	return found
}
