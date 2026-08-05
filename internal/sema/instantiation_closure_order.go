package sema

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

func sortClosureInstances(instances []InstantiationInstance) {
	sort.SliceStable(instances, func(i, j int) bool {
		return compareInstanceKey(instances[i].Key, instances[j].Key) < 0
	})
}

func compareInstanceKey(left, right InstanceKey) int {
	if left.TemplateKey != right.TemplateKey {
		return strings.Compare(left.TemplateKey, right.TemplateKey)
	}
	return strings.Compare(left.ArgsKey, right.ArgsKey)
}

func canonicalInstantiationWitness(original *InstantiationWitness, identity InstantiationIdentity) (InstantiationWitness, error) {
	witness := cloneInstantiationWitness(original)
	if witness.Caller.IsValid() && witness.CallerKey == "" {
		if identity.ResolveTemplate == nil {
			return InstantiationWitness{}, fmt.Errorf("instantiation witness: missing canonical template resolver")
		}
		callerKey, err := identity.ResolveTemplate(witness.Caller)
		if err != nil {
			return InstantiationWitness{}, fmt.Errorf("instantiation witness caller %d: %w", witness.Caller, err)
		}
		witness.CallerKey = callerKey
	}
	if witness.SourceKey == "" && identity.ResolveSource != nil {
		sourceKey, err := identity.ResolveSource(witness.Site.File)
		if err != nil {
			return InstantiationWitness{}, fmt.Errorf("instantiation witness source %d: %w", witness.Site.File, err)
		}
		witness.SourceKey = sourceKey
	}
	return witness, nil
}

func witnessSiteLabel(witness *InstantiationWitness) string {
	if witness.SourceKey == "" {
		return witness.Site.String()
	}
	return fmt.Sprintf("%s:%d-%d", witness.SourceKey, witness.Site.Start, witness.Site.End)
}

func stepSiteLabel(step *InstantiationStep) string {
	if step.SourceKey != "" {
		return fmt.Sprintf("%s:%d-%d", step.SourceKey, step.Site.Start, step.Site.End)
	}
	return step.Site.String()
}

func cloneInstantiationInstance(instance *InstantiationInstance) InstantiationInstance {
	cloned := *instance
	cloned.TemplateArgs = slices.Clone(instance.TemplateArgs)
	cloned.Witness = cloneInstantiationWitness(&instance.Witness)
	return cloned
}

func sortAndCompactConcreteUses(uses []ConcreteInstantiationUse) []ConcreteInstantiationUse {
	sort.SliceStable(uses, func(i, j int) bool {
		left, right := uses[i], uses[j]
		if cmp := compareInstanceKey(left.Caller, right.Caller); cmp != 0 {
			return cmp < 0
		}
		if cmp := compareInstanceKey(left.Callee, right.Callee); cmp != 0 {
			return cmp < 0
		}
		if left.SourceKey != right.SourceKey {
			return left.SourceKey < right.SourceKey
		}
		if cmp := compareSpanOffsets(left.Site, right.Site); cmp != 0 {
			return cmp < 0
		}
		return left.Reason < right.Reason
	})
	out := uses[:0]
	for i := range uses {
		use := &uses[i]
		if len(out) > 0 && concreteUsesEqual(&out[len(out)-1], use) {
			continue
		}
		cloned := *use
		cloned.TemplateArgs = slices.Clone(use.TemplateArgs)
		cloned.CallerTemplateArgs = slices.Clone(use.CallerTemplateArgs)
		out = append(out, cloned)
	}
	return out
}

func concreteUsesEqual(left, right *ConcreteInstantiationUse) bool {
	return left.Caller == right.Caller &&
		left.Callee == right.Callee &&
		left.Kind == right.Kind &&
		compareSpanOffsets(left.Site, right.Site) == 0 &&
		left.SourceKey == right.SourceKey &&
		left.Reason == right.Reason &&
		left.Caller.ArgsKey == right.Caller.ArgsKey &&
		left.Callee.ArgsKey == right.Callee.ArgsKey
}
