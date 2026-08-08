package sema

import (
	"sort"
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// magicDeclaration remembers where one hook was written. The magic lookup index
// stores signatures only, and a signature carries no location, so a refusal
// could not otherwise show the reader the rival declaration.
type magicDeclaration struct {
	receiver   symbols.TypeKey
	name       string
	params     []symbols.TypeKey
	span       source.Span
	modulePath string
	// compilerProvided marks a hook supplied for a builtin type rather than one
	// someone wrote, so the note says that instead of pointing at a source line
	// which does not exist.
	compilerProvided bool
}

// aliasTargetSpelling is one type the alias stands for, in both the form the
// magic index is keyed by and the form a reader recognises.
type aliasTargetSpelling struct {
	key   symbols.TypeKey
	label string
}

// isCompilerInvokedMagicName reports whether the compiler, rather than the
// author, chooses the receiver spelling a hook is reached through. `x + y`,
// `x to T`, `clone(&x)`, `for e in x`, `x[i]`, `if x`, `abs(x)` and `len(x)`
// all run a method the source never names, so the author cannot pick between
// two bodies at the call site. An ordinary method is written out and stays the
// author's choice.
func isCompilerInvokedMagicName(name string) bool {
	if isOperatorMagicName(name) {
		return true
	}
	switch name {
	case "__to", "__clone", "__bool", "__range", "__index", "__index_set", "__abs", "__len":
		return true
	default:
		return false
	}
}

// modulePathOf falls back to the module under check for a symbol that carries
// no path of its own, which is how declarations written here are stored.
func (tc *typeChecker) modulePathOf(path string) string {
	if path == "" {
		return tc.modulePath
	}
	return path
}

func (tc *typeChecker) recordMagicDeclaration(decl *magicDeclaration) {
	if decl == nil || decl.receiver == "" || decl.name == "" || !isCompilerInvokedMagicName(decl.name) {
		return
	}
	tc.magicDecls = append(tc.magicDecls, *decl)
}

// rememberMagicDeclaration records a hook someone wrote, wherever the magic
// index picked it up from.
func (tc *typeChecker) rememberMagicDeclaration(sig *symbols.FunctionSignature, receiver symbols.TypeKey, name, modulePath string, span source.Span) {
	if sig == nil {
		return
	}
	tc.recordMagicDeclaration(&magicDeclaration{
		receiver:   receiver,
		name:       name,
		params:     sig.Params,
		span:       span,
		modulePath: tc.modulePathOf(modulePath),
	})
}

// rememberBuiltinMagicDeclaration records a hook the compiler provides for a
// builtin type. It has no source line, but it is what the value would run.
func (tc *typeChecker) rememberBuiltinMagicDeclaration(receiver symbols.TypeKey, name string, params []symbols.TypeKey) {
	tc.recordMagicDeclaration(&magicDeclaration{
		receiver:         receiver,
		name:             name,
		params:           params,
		compilerProvided: true,
	})
}

// checkAliasMagicDeclarations refuses a hook declared on an alias when the type
// the alias stands for answers the same operands. Both declarations are visible
// here, before any use site exists, which is the earliest point that can name
// the real cause.
//
// Only the file under check is walked, so a module of several files reports
// each declaration once. Rivals are drawn from the whole program.
func (tc *typeChecker) checkAliasMagicDeclarations() {
	if tc == nil || tc.reporter == nil || tc.builder == nil || tc.types == nil || len(tc.magicDecls) == 0 {
		return
	}
	file := tc.builder.Files.Get(tc.fileID)
	if file == nil {
		return
	}
	rivals := tc.magicDeclarationsByReceiver()
	for _, itemID := range file.Items {
		block, ok := tc.builder.Items.Extern(itemID)
		if !ok || block == nil || !block.MembersStart.IsValid() {
			continue
		}
		start := uint32(block.MembersStart)
		for offset := range block.MembersCount {
			tc.checkAliasMagicMember(ast.ExternMemberID(start+offset), rivals)
		}
	}
}

func (tc *typeChecker) checkAliasMagicMember(memberID ast.ExternMemberID, rivals map[symbols.TypeKey][]magicDeclaration) {
	member := tc.builder.Items.ExternMember(memberID)
	if member == nil || member.Kind != ast.ExternMemberFn {
		return
	}
	fn := tc.builder.Items.FnByPayload(member.Fn)
	if fn == nil {
		return
	}
	sym := tc.symbolFromID(tc.symbolForExtern(memberID))
	if sym == nil || sym.Signature == nil || sym.ReceiverKey == "" {
		return
	}
	name := tc.symbolName(sym.Name)
	if !isCompilerInvokedMagicName(name) {
		return
	}
	declared := magicDeclaration{receiver: sym.ReceiverKey, name: name, params: sym.Signature.Params}
	for _, target := range tc.aliasTargetSpellings(sym.ReceiverKey) {
		for i := range rivals[target.key] {
			rival := &rivals[target.key][i]
			if rival.name != name || !tc.magicOperandsAgree(&declared, rival) {
				continue
			}
			tc.reportAliasMagicRedeclared(fn, sym.ReceiverKey, target.label, name, rival)
			return
		}
	}
}

// magicDeclarationsByReceiver groups the remembered declarations so a target
// type can be asked what it already answers. Each group is ordered so the named
// rival does not depend on the order modules happened to be merged in.
func (tc *typeChecker) magicDeclarationsByReceiver() map[symbols.TypeKey][]magicDeclaration {
	grouped := make(map[symbols.TypeKey][]magicDeclaration, len(tc.magicDecls))
	for i := range tc.magicDecls {
		key := canonicalTypeKey(tc.magicDecls[i].receiver)
		if key == "" {
			continue
		}
		grouped[key] = append(grouped[key], tc.magicDecls[i])
	}
	for key := range grouped {
		group := grouped[key]
		sort.SliceStable(group, func(i, j int) bool {
			return compareMagicDeclarations(&group[i], &group[j]) < 0
		})
	}
	return grouped
}

func compareMagicDeclarations(left, right *magicDeclaration) int {
	if left.name != right.name {
		return strings.Compare(left.name, right.name)
	}
	// A hook someone wrote is named before one the compiler supplies: it is the
	// one the reader can go and look at.
	if left.compilerProvided != right.compilerProvided {
		if right.compilerProvided {
			return -1
		}
		return 1
	}
	if left.modulePath != right.modulePath {
		return strings.Compare(left.modulePath, right.modulePath)
	}
	if left.span.File != right.span.File {
		if left.span.File < right.span.File {
			return -1
		}
		return 1
	}
	if left.span.Start != right.span.Start {
		if left.span.Start < right.span.Start {
			return -1
		}
		return 1
	}
	return 0
}

// aliasTargetSpellings walks the alias chain of a receiver and returns every
// type it stands for. An alias of an alias is transparent through both steps,
// so a hook on the outermost name rivals a hook on any of them.
func (tc *typeChecker) aliasTargetSpellings(receiver symbols.TypeKey) []aliasTargetSpelling {
	_, base := splitTypeKeyPrefix(string(receiver))
	id := tc.typeFromKey(symbols.TypeKey(base))
	if id == types.NoTypeID {
		return nil
	}
	if tt, ok := tc.types.Lookup(id); !ok || tt.Kind != types.KindAlias {
		return nil
	}
	const maxDepth = 32
	spellings := make([]aliasTargetSpelling, 0, 2)
	seen := map[types.TypeID]struct{}{id: {}}
	current := id
	for range maxDepth {
		target, ok := tc.types.AliasTarget(current)
		if !ok || target == types.NoTypeID {
			break
		}
		if _, repeated := seen[target]; repeated {
			break
		}
		seen[target] = struct{}{}
		if key := canonicalTypeKey(tc.typeKeyForType(target)); key != "" {
			spellings = append(spellings, aliasTargetSpelling{key: key, label: types.Label(tc.types, target)})
		}
		current = target
	}
	return spellings
}

// magicOperandsAgree reports whether two declarations of the same hook answer
// the same question. The receiver is skipped: it is the one position the alias
// and its target are bound to spell differently, and comparing it would make
// every pair look distinct. What remains decides — two `__to` declarations that
// name different targets convert different things and never rival each other,
// while two `__eq` declarations over the same operand do.
func (tc *typeChecker) magicOperandsAgree(declared, rival *magicDeclaration) bool {
	left := magicOperands(declared.params)
	right := magicOperands(rival.params)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if tc.aliasNormalizedKey(left[i]) != tc.aliasNormalizedKey(right[i]) {
			return false
		}
	}
	return true
}

// magicOperands drops the receiver. Every hook takes it first.
func magicOperands(params []symbols.TypeKey) []symbols.TypeKey {
	if len(params) == 0 {
		return nil
	}
	return params[1:]
}

// aliasNormalizedKey spells a parameter through its alias chain, so an operand
// written as the alias and the same operand written as the target compare equal.
func (tc *typeChecker) aliasNormalizedKey(key symbols.TypeKey) string {
	prefix, base := splitTypeKeyPrefix(string(key))
	if id := tc.typeFromKey(symbols.TypeKey(base)); id != types.NoTypeID {
		if resolved := tc.typeKeyForType(tc.resolveAlias(id)); resolved != "" {
			base = strings.TrimSpace(string(resolved))
		}
	}
	return prefix + base
}

// splitTypeKeyPrefix separates a borrow/ownership marker from the type name it
// applies to, keeping the marker so `&T` never compares equal to `T`.
func splitTypeKeyPrefix(key string) (prefix, base string) {
	base = strings.TrimSpace(key)
	switch {
	case strings.HasPrefix(base, "&mut "):
		return "&mut ", strings.TrimSpace(strings.TrimPrefix(base, "&mut "))
	case strings.HasPrefix(base, "&"):
		return "&", strings.TrimSpace(strings.TrimPrefix(base, "&"))
	case strings.HasPrefix(base, "own "):
		return "own ", strings.TrimSpace(strings.TrimPrefix(base, "own "))
	case strings.HasPrefix(base, "*"):
		return "*", strings.TrimSpace(strings.TrimPrefix(base, "*"))
	default:
		return "", base
	}
}
