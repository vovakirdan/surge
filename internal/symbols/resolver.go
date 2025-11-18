package symbols

import (
	"fmt"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

// ResolverOptions configures resolver construction.
type ResolverOptions struct {
	Reporter diag.Reporter
	Prelude  []PreludeEntry
}

// PreludeEntry describes a symbol injected before source traversal.
type PreludeEntry struct {
	Name      string
	Kind      SymbolKind
	Flags     SymbolFlags
	Span      source.Span
	Signature *FunctionSignature
	Type      types.TypeID
}

// KindMask restricts lookup to specific symbol kinds.
type KindMask uint32

const (
	// KindMaskNone filters out all kinds.
	KindMaskNone KindMask = 0
	// KindMaskAny allows all kinds.
	KindMaskAny KindMask = ^KindMask(0)
)

// Mask converts a symbol kind into a KindMask bit.
func (k SymbolKind) Mask() KindMask {
	return KindMask(1 << uint(k))
}

func matchKind(mask KindMask, kind SymbolKind) bool {
	return mask == KindMaskAny || mask&kind.Mask() != 0
}

func allowsOverload(kind SymbolKind) bool {
	return kind == SymbolFunction
}

func canShareName(existing, next SymbolKind) bool {
	if existing == next {
		return allowsOverload(next)
	}
	if (existing == SymbolFunction && next == SymbolTag) || (existing == SymbolTag && next == SymbolFunction) {
		return true
	}
	return false
}

// Resolver drives scope management and declaration/lookup routines.
type Resolver struct {
	table                 *Table
	reporter              diag.Reporter
	stack                 []ScopeID
	scopeMismatchReported map[ScopeID]bool
}

// NewResolver wires a resolver to an existing scope stack. If root is valid it
// becomes the current scope; otherwise scope-sensitive operations are no-ops.
func NewResolver(table *Table, root ScopeID, opts ResolverOptions) *Resolver {
	r := &Resolver{
		table:                 table,
		reporter:              opts.Reporter,
		stack:                 make([]ScopeID, 0, 8),
		scopeMismatchReported: make(map[ScopeID]bool),
	}
	if root.IsValid() {
		r.stack = append(r.stack, root)
	}
	if len(opts.Prelude) > 0 && root.IsValid() {
		r.installPrelude(root, opts.Prelude)
	}
	return r
}

// CurrentScope returns the scope at the top of the stack.
func (r *Resolver) CurrentScope() ScopeID {
	if len(r.stack) == 0 {
		return NoScopeID
	}
	return r.stack[len(r.stack)-1]
}

// Enter creates a child scope, pushes it onto the stack, and returns its ID.
func (r *Resolver) Enter(kind ScopeKind, owner ScopeOwner, span source.Span) ScopeID {
	parent := r.CurrentScope()
	scope := r.table.Scopes.New(kind, parent, owner, span)
	r.stack = append(r.stack, scope)
	return scope
}

// Leave pops the current scope, validating against the expected one. In debug
// builds a mismatch triggers panic; release builds emit a warning diagnostic.
func (r *Resolver) Leave(expected ScopeID) {
	if len(r.stack) == 0 {
		return
	}
	top := r.stack[len(r.stack)-1]
	if expected.IsValid() && top != expected {
		debugScopeMismatch(expected, top)
		r.reportScopeMismatch(expected, top)
		r.stack = r.stack[:len(r.stack)-1]
		return
	}
	r.stack = r.stack[:len(r.stack)-1]
}

// Declare installs a symbol into the current scope. Returns false if there is
// no active scope or declaration conflicts with existing entry.
func (r *Resolver) Declare(name source.StringID, span source.Span, kind SymbolKind, flags SymbolFlags, decl SymbolDecl) (SymbolID, bool) {
	scopeID := r.CurrentScope()
	if !scopeID.IsValid() {
		return NoSymbolID, false
	}
	scope := r.table.Scopes.Get(scopeID)
	if scope == nil {
		return NoSymbolID, false
	}

	if existing := scope.NameIndex[name]; len(existing) > 0 {
		for _, symID := range existing {
			sym := r.table.Symbols.Get(symID)
			if sym == nil {
				continue
			}
			if canShareName(sym.Kind, kind) {
				continue
			}
			r.reportDuplicateSymbol(name, span, sym.Span, sym.Flags)
			return NoSymbolID, false
		}
	}

	if shadow := r.findShadowing(scopeID, name); shadow.IsValid() {
		r.reportShadowing(name, span, shadow)
	}

	id := r.declareWithoutChecks(name, span, kind, flags, decl, nil)
	return id, id.IsValid()
}

func (r *Resolver) declareWithoutChecks(name source.StringID, span source.Span, kind SymbolKind, flags SymbolFlags, decl SymbolDecl, sig *FunctionSignature) SymbolID {
	scopeID := r.CurrentScope()
	if !scopeID.IsValid() {
		return NoSymbolID
	}
	sym := Symbol{
		Name:      name,
		Kind:      kind,
		Scope:     scopeID,
		Span:      span,
		Flags:     flags,
		Decl:      decl,
		Signature: sig,
	}
	id := r.table.Symbols.New(&sym)
	if scope := r.table.Scopes.Get(scopeID); scope != nil {
		scope.Symbols = append(scope.Symbols, id)
		scope.NameIndex[name] = append(scope.NameIndex[name], id)
	}
	return id
}

// Lookup walks the scope chain searching for a symbol with the given name.
func (r *Resolver) Lookup(name source.StringID) (SymbolID, bool) {
	return r.LookupOne(name, KindMaskAny)
}

// LookupOne finds the most recent symbol with matching name and kind mask.
func (r *Resolver) LookupOne(name source.StringID, mask KindMask) (SymbolID, bool) {
	if mask == KindMaskNone {
		return NoSymbolID, false
	}
	scopeID := r.CurrentScope()
	for scopeID.IsValid() {
		scope := r.table.Scopes.Get(scopeID)
		if scope == nil {
			break
		}
		if candidates := r.lookupInScope(scopeID, name, mask); len(candidates) > 0 {
			return candidates[len(candidates)-1], true
		}
		scopeID = scope.Parent
	}
	return NoSymbolID, false
}

// LookupAll collects all visible symbols with the specified name and kind mask.
// Order: innermost scope first, and within the same scope — newest declaration first.
func (r *Resolver) LookupAll(name source.StringID, mask KindMask) []SymbolID {
	if mask == KindMaskNone {
		return nil
	}
	var result []SymbolID
	scopeID := r.CurrentScope()
	for scopeID.IsValid() {
		scope := r.table.Scopes.Get(scopeID)
		if scope == nil {
			break
		}
		if candidates := r.lookupInScope(scopeID, name, mask); len(candidates) > 0 {
			for i := len(candidates) - 1; i >= 0; i-- {
				result = append(result, candidates[i])
			}
		}
		scopeID = scope.Parent
	}
	return result
}

func (r *Resolver) lookupInScope(scopeID ScopeID, name source.StringID, mask KindMask) []SymbolID {
	scope := r.table.Scopes.Get(scopeID)
	if scope == nil {
		return nil
	}
	ids := scope.NameIndex[name]
	if len(ids) == 0 {
		return nil
	}
	if mask == KindMaskAny {
		return ids
	}
	filtered := make([]SymbolID, 0, len(ids))
	for _, id := range ids {
		if sym := r.table.Symbols.Get(id); sym != nil && matchKind(mask, sym.Kind) {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func (r *Resolver) reportDuplicateSymbol(name source.StringID, span, prevSpan source.Span, prevFlags SymbolFlags) {
	if r.reporter == nil {
		return
	}
	nameStr := r.table.Strings.MustLookup(name)
	msg := fmt.Sprintf("duplicate declaration of '%s'", nameStr)
	builder := diag.ReportError(r.reporter, diag.SemaDuplicateSymbol, span, msg)
	if builder == nil {
		return
	}
	noteMsg := "previous declaration here"
	if prevFlags&SymbolFlagBuiltin != 0 {
		noteMsg = "built-in declaration here"
	}
	if prevSpan != (source.Span{}) {
		builder.WithNote(prevSpan, noteMsg)
	}
	builder.Emit()
}

func (r *Resolver) reportScopeMismatch(expected, actual ScopeID) {
	if r.reporter == nil {
		return
	}
	if actual.IsValid() && r.scopeMismatchReported[actual] {
		return
	}
	if actual.IsValid() {
		r.scopeMismatchReported[actual] = true
	}

	var primary source.Span
	var actualLabel string
	if scope := r.table.Scopes.Get(actual); scope != nil {
		primary = scope.Span
		actualLabel = fmt.Sprintf("%s scope #%d", scope.Kind, actual)
	} else {
		actualLabel = fmt.Sprintf("scope #%d", actual)
	}

	expectedLabel := "unknown scope"
	if expectedScope := r.table.Scopes.Get(expected); expectedScope != nil {
		expectedLabel = fmt.Sprintf("%s scope #%d", expectedScope.Kind, expected)
	}

	msg := fmt.Sprintf("scope stack mismatch: closing %s while expecting %s", actualLabel, expectedLabel)
	builder := diag.ReportWarning(r.reporter, diag.SemaScopeMismatch, primary, msg)
	if builder == nil {
		return
	}
	if expectedScope := r.table.Scopes.Get(expected); expectedScope != nil {
		builder.WithNote(expectedScope.Span, "expected scope declared here")
	}
	builder.Emit()
}

// installPrelude declares prelude entries into scope.
func (r *Resolver) installPrelude(scopeID ScopeID, entries []PreludeEntry) {
	scope := r.table.Scopes.Get(scopeID)
	if scope == nil {
		return
	}
	for _, entry := range entries {
		nameID := r.table.Strings.Intern(entry.Name)
		flags := entry.Flags | SymbolFlagBuiltin
		span := entry.Span
		sym := Symbol{
			Name:      nameID,
			Kind:      entry.Kind,
			Scope:     scopeID,
			Span:      span,
			Flags:     flags,
			Type:      entry.Type,
			Signature: entry.Signature,
			Decl: SymbolDecl{
				SourceFile: span.File,
			},
		}
		id := r.table.Symbols.New(&sym)
		scope.Symbols = append(scope.Symbols, id)
		scope.NameIndex[nameID] = append(scope.NameIndex[nameID], id)
	}
}

func (r *Resolver) findShadowing(scopeID ScopeID, name source.StringID) SymbolID {
	scope := r.table.Scopes.Get(scopeID)
	if scope == nil {
		return NoSymbolID
	}
	parent := scope.Parent
	for parent.IsValid() {
		parentScope := r.table.Scopes.Get(parent)
		if parentScope == nil {
			break
		}
		if ids := parentScope.NameIndex[name]; len(ids) > 0 {
			return ids[len(ids)-1]
		}
		parent = parentScope.Parent
	}
	return NoSymbolID
}

func (r *Resolver) reportShadowing(name source.StringID, span source.Span, shadow SymbolID) {
	if r.reporter == nil || !shadow.IsValid() {
		return
	}
	nameStr := r.table.Strings.MustLookup(name)
	if nameStr == "_" {
		return
	}
	msg := fmt.Sprintf("declaration of '%s' shadows previous binding", nameStr)
	builder := diag.ReportWarning(r.reporter, diag.SemaShadowSymbol, span, msg)
	if builder == nil {
		return
	}
	if prev := r.table.Symbols.Get(shadow); prev != nil {
		noteMsg := "previous declaration here"
		if prev.Flags&SymbolFlagBuiltin != 0 {
			noteMsg = "built-in declaration here"
		}
		if prev.Span != (source.Span{}) {
			builder.WithNote(prev.Span, noteMsg)
		}
	}
	builder.Emit()
}
