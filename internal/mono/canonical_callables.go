package mono

import (
	"fmt"
	"slices"
	"sort"
	"strconv"

	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

// CallableKey is the canonical retained implementation identity shared by
// sema closure authorization, mono, DCE, and downstream callable lookup.
// ArgsKey is empty for a non-generic callable.
type CallableKey struct {
	TemplateKey string
	ArgsKey     string
}

// CallableMap is an immutable-after-build view of retained callable
// instances. Raw symbol aliases are resolved through the canonical semantic
// identity before consulting the map.
type CallableMap struct {
	identity *sema.InstantiationIdentity
	entries  map[CallableKey]symbols.SymbolID
}

func newCallableMap(identity *sema.InstantiationIdentity) CallableMap {
	return CallableMap{
		identity: identity,
		entries:  make(map[CallableKey]symbols.SymbolID),
	}
}

// Lookup returns the emitted instance symbol for one semantic callable
// identity. Invalid or unresolvable aliases fail closed.
func (m CallableMap) Lookup(template symbols.SymbolID, args []types.TypeID) (symbols.SymbolID, bool) {
	id, ok, err := m.LookupChecked(template, args)
	if err != nil {
		return symbols.NoSymbolID, false
	}
	return id, ok
}

// LookupChecked is the fail-closed production lookup. It distinguishes an
// absent retained callable from a corrupt or incomplete canonical identity.
func (m CallableMap) LookupChecked(template symbols.SymbolID, args []types.TypeID) (symbols.SymbolID, bool, error) {
	if m.entries == nil {
		return symbols.NoSymbolID, false, nil
	}
	key, err := canonicalCallableKey(m.identity, template, args)
	if err != nil {
		return symbols.NoSymbolID, false, err
	}
	id, ok := m.entries[key]
	return id, ok, nil
}

func (m *CallableMap) bind(template symbols.SymbolID, args []types.TypeID, instance symbols.SymbolID) error {
	if m == nil {
		return fmt.Errorf("mono: missing callable map")
	}
	if !instance.IsValid() {
		return fmt.Errorf("mono: missing emitted callable instance")
	}
	key, err := canonicalCallableKey(m.identity, template, args)
	if err != nil {
		return err
	}
	if m.entries == nil {
		m.entries = make(map[CallableKey]symbols.SymbolID)
	}
	if existing, found := m.entries[key]; found && existing != instance {
		return fmt.Errorf("mono: canonical callable %s[%s] has conflicting emitted instances %d and %d", key.TemplateKey, key.ArgsKey, existing, instance)
	}
	m.entries[key] = instance
	return nil
}

func canonicalCallableKey(
	identity *sema.InstantiationIdentity,
	template symbols.SymbolID,
	args []types.TypeID,
) (CallableKey, error) {
	if !template.IsValid() {
		return CallableKey{}, fmt.Errorf("mono: missing callable template symbol")
	}
	if identity == nil {
		// Compatibility for isolated low-level mono callers that do not have a
		// finalized sema result. Production closure-backed paths always carry
		// the canonical identity.
		return CallableKey{
			TemplateKey: "raw/" + strconv.FormatUint(uint64(template), 10),
			ArgsKey:     typeArgsKey(NormalizeTypeArgs(nil, args)),
		}, nil
	}
	if identity.ResolveTemplate == nil {
		return CallableKey{}, fmt.Errorf("mono: canonical callable identity has no template resolver")
	}
	templateKey, err := identity.ResolveTemplate(template)
	if err != nil {
		return CallableKey{}, fmt.Errorf("mono: resolve canonical callable template %d: %w", template, err)
	}
	if templateKey == "" {
		return CallableKey{}, fmt.Errorf("mono: callable template %d has an empty canonical identity", template)
	}
	argsKey, err := identity.Types.TypeArgsKey(args)
	if err != nil {
		return CallableKey{}, fmt.Errorf("mono: resolve canonical callable arguments for symbol %d: %w", template, err)
	}
	return CallableKey{TemplateKey: templateKey, ArgsKey: argsKey}, nil
}

type retainedCallable struct {
	Template     symbols.SymbolID
	TemplateArgs []types.TypeID
}

func (b *monoBuilder) initializeRetainedCallables() error {
	if b == nil || b.closure == nil {
		return nil
	}
	if b.identity == nil {
		return fmt.Errorf("mono: finalized instantiation closure has no canonical identity")
	}
	b.retainedCallables = make(map[CallableKey]retainedCallable, len(b.closure.LiveCallables)+len(b.closure.Instances))
	for _, template := range b.closure.LiveCallables {
		key, err := canonicalCallableKey(b.identity, template, nil)
		if err != nil {
			return err
		}
		b.bindRetainedCallable(key, retainedCallable{Template: template})
	}
	for i := range b.closure.Instances {
		instance := &b.closure.Instances[i]
		if len(instance.TemplateArgs) == 0 {
			return fmt.Errorf("mono: authoritative generic callable %d has no template arguments", instance.Template)
		}
		key, err := canonicalCallableKey(b.identity, instance.Template, instance.TemplateArgs)
		if err != nil {
			return err
		}
		if instance.Key.TemplateKey == "" || instance.Key.ArgsKey == "" {
			return fmt.Errorf("mono: authoritative callable %d has no canonical instance key", instance.Template)
		}
		if key.TemplateKey != instance.Key.TemplateKey || key.ArgsKey != instance.Key.ArgsKey {
			return fmt.Errorf(
				"mono: authoritative callable %d identity disagrees with closure key: got %s[%s], want %s[%s]",
				instance.Template,
				key.TemplateKey,
				key.ArgsKey,
				instance.Key.TemplateKey,
				instance.Key.ArgsKey,
			)
		}
		b.bindRetainedCallable(key, retainedCallable{
			Template:     instance.Template,
			TemplateArgs: slices.Clone(instance.TemplateArgs),
		})
	}
	// Combined HIR may carry a body under a raw import/root alias different
	// from the representative kept by sema. A body-bearing alias is safe to
	// select only when it resolves to an already-authorized canonical key.
	for _, fn := range b.mod.Funcs {
		if fn == nil || fn.Body == nil || !fn.SymbolID.IsValid() {
			continue
		}
		templateKey, err := canonicalCallableKey(b.identity, fn.SymbolID, nil)
		if err != nil {
			return err
		}
		for key, retained := range b.retainedCallables {
			if key.TemplateKey != templateKey.TemplateKey {
				continue
			}
			if len(retained.TemplateArgs) > 0 && len(b.templateParams[fn.SymbolID]) != len(retained.TemplateArgs) {
				continue
			}
			b.bindRetainedCallable(key, retainedCallable{
				Template:     fn.SymbolID,
				TemplateArgs: retained.TemplateArgs,
			})
		}
	}
	return nil
}

func (b *monoBuilder) bindRetainedCallable(key CallableKey, candidate retainedCallable) {
	if b == nil || !candidate.Template.IsValid() {
		return
	}
	existing, found := b.retainedCallables[key]
	if !found || b.preferRetainedCallable(candidate, existing) {
		candidate.TemplateArgs = slices.Clone(candidate.TemplateArgs)
		b.retainedCallables[key] = candidate
	}
}

func (b *monoBuilder) preferRetainedCallable(candidate, existing retainedCallable) bool {
	candidateFn := b.origFuncBySym[candidate.Template]
	existingFn := b.origFuncBySym[existing.Template]
	candidateBody := candidateFn != nil && candidateFn.Body != nil
	existingBody := existingFn != nil && existingFn.Body != nil
	if candidateBody != existingBody {
		return candidateBody
	}
	if (candidateFn != nil) != (existingFn != nil) {
		return candidateFn != nil
	}
	return candidate.Template < existing.Template
}

func (b *monoBuilder) sortedRetainedCallableKeys() []CallableKey {
	if b == nil || len(b.retainedCallables) == 0 {
		return nil
	}
	keys := make([]CallableKey, 0, len(b.retainedCallables))
	for key := range b.retainedCallables {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].TemplateKey != keys[j].TemplateKey {
			return keys[i].TemplateKey < keys[j].TemplateKey
		}
		return keys[i].ArgsKey < keys[j].ArgsKey
	})
	return keys
}

func (b *monoBuilder) retainedCallableFor(
	template symbols.SymbolID,
	args []types.TypeID,
) (retainedCallable, bool, error) {
	if b == nil || b.closure == nil {
		return retainedCallable{Template: template, TemplateArgs: slices.Clone(args)}, true, nil
	}
	key, err := canonicalCallableKey(b.identity, template, args)
	if err != nil {
		return retainedCallable{}, false, err
	}
	retained, found := b.retainedCallables[key]
	if !found {
		return retainedCallable{}, false, nil
	}
	retained.TemplateArgs = slices.Clone(retained.TemplateArgs)
	return retained, true, nil
}

func (b *monoBuilder) sameCallableTemplate(left, right symbols.SymbolID) (bool, error) {
	if b == nil || !left.IsValid() || !right.IsValid() {
		return false, nil
	}
	leftKey, err := canonicalCallableKey(b.identity, left, nil)
	if err != nil {
		return false, err
	}
	rightKey, err := canonicalCallableKey(b.identity, right, nil)
	if err != nil {
		return false, err
	}
	return leftKey.TemplateKey == rightKey.TemplateKey, nil
}
