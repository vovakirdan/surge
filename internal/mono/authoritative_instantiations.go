package mono

import (
	"fmt"

	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
)

func authoritativeInstantiationMap(
	legacy *InstantiationMap,
	semaResult *sema.Result,
) (*InstantiationMap, *sema.InstantiationClosure, error) {
	if semaResult == nil {
		// Provisional compatibility for isolated low-level tests/embedders that
		// construct InstantiationMap directly and have no semantic result.
		return legacy, nil, nil
	}
	if semaResult.InstantiationClosure != nil {
		if semaResult.InstantiationIdentity == nil {
			return nil, nil, fmt.Errorf("mono: finalized instantiation closure has no canonical identity")
		}
		out, err := RebuildInstantiationMapFromClosure(legacy, semaResult.InstantiationClosure, semaResult.InstantiationIdentity)
		if err != nil {
			return nil, nil, err
		}
		return out, semaResult.InstantiationClosure, nil
	}
	if semaResult.InstantiationGraph.IsEmpty() {
		// Compatibility for isolated low-level tests/embedders that did not run
		// the semantic closure pass at all. A present, even empty, closure above
		// is authoritative and must never fall back to this path.
		return legacy, nil, nil
	}
	if semaResult.InstantiationIdentity == nil {
		return nil, nil, fmt.Errorf("mono: non-empty instantiation graph is not finalized; run the post-sema instantiation closure pass before HIR/mono")
	}
	return nil, nil, fmt.Errorf("mono: non-empty instantiation graph is not finalized; run the post-sema instantiation closure pass before HIR/mono")
}

// RebuildInstantiationMapFromClosure makes the finalized sema closure the one
// callable authority while retaining root-local legacy type uses until typed
// nominal demands move into the same graph.
func RebuildInstantiationMapFromClosure(
	legacy *InstantiationMap,
	closure *sema.InstantiationClosure,
	identity *sema.InstantiationIdentity,
) (*InstantiationMap, error) {
	out := NewInstantiationMap()
	copyLegacyTypeInstantiations(out, legacy)
	if closure == nil {
		return out, nil
	}
	if identity == nil {
		return nil, fmt.Errorf("mono: finalized instantiation closure has no canonical identity")
	}
	instances := make(map[sema.InstanceKey]sema.InstantiationInstance, len(closure.Instances))
	for i := range closure.Instances {
		instance := closure.Instances[i]
		key, err := canonicalCallableKey(identity, instance.Template, instance.TemplateArgs)
		if err != nil {
			return nil, err
		}
		if key.TemplateKey != instance.Key.TemplateKey || key.ArgsKey != instance.Key.ArgsKey {
			return nil, fmt.Errorf("mono: closure instance %d disagrees with its canonical key", instance.Template)
		}
		if _, duplicate := instances[instance.Key]; duplicate {
			return nil, fmt.Errorf("mono: closure contains duplicate canonical instance %s[%s]", instance.Key.TemplateKey, instance.Key.ArgsKey)
		}
		instances[instance.Key] = instance
		out.RecordCanonical(
			monoInstantiationKind(instance.Kind),
			instance.Template,
			instance.TemplateArgs,
			source.Span{},
			"",
			symbols.NoSymbolID,
			"",
		)
	}
	for i := range closure.UseSites {
		use := &closure.UseSites[i]
		instance, found := instances[use.Callee]
		if !found {
			return nil, fmt.Errorf("mono: closure use at %s references missing canonical instance %s[%s]", use.Site, use.Callee.TemplateKey, use.Callee.ArgsKey)
		}
		useKey, err := canonicalCallableKey(identity, use.CalleeTemplate, use.TemplateArgs)
		if err != nil {
			return nil, err
		}
		if useKey.TemplateKey != use.Callee.TemplateKey || useKey.ArgsKey != use.Callee.ArgsKey {
			return nil, fmt.Errorf("mono: closure use at %s disagrees with its canonical callee key", use.Site)
		}
		if use.Kind != instance.Kind {
			return nil, fmt.Errorf("mono: closure use at %s has kind %s, retained instance has kind %s", use.Site, use.Kind, instance.Kind)
		}
		out.RecordCanonical(
			monoInstantiationKind(instance.Kind),
			instance.Template,
			instance.TemplateArgs,
			use.Site,
			use.SourceKey,
			use.CallerTemplate,
			use.Reason,
		)
	}
	return out, nil
}

func monoInstantiationKind(kind sema.InstantiationTemplateKind) InstantiationKind {
	if kind == sema.InstantiationTag {
		return InstTag
	}
	return InstFn
}

func copyLegacyTypeInstantiations(dst, src *InstantiationMap) {
	if dst == nil || src == nil {
		return
	}
	for _, entry := range src.Entries {
		if entry == nil || entry.Kind != InstType || !entry.Key.Sym.IsValid() || len(entry.TypeArgs) == 0 {
			continue
		}
		if len(entry.UseSites) == 0 {
			dst.Record(InstType, entry.Key.Sym, entry.TypeArgs, source.Span{}, 0, "")
			continue
		}
		for _, site := range entry.UseSites {
			dst.Record(InstType, entry.Key.Sym, entry.TypeArgs, site.Span, site.Caller, site.Note)
		}
	}
}
