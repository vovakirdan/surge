package sema

import (
	"errors"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestCloneSelectorPrefersConcreteBodyOverGenericOverlap(t *testing.T) {
	in := deferredResolverTestInterner()
	name := in.Strings.Intern("Box")
	decl := source.Span{File: 1, Start: 1, End: 2}
	generic := symbols.SymbolID(10)
	param := in.RegisterTypeParam(in.Strings.Intern("T"), uint32(generic), 0, false, types.NoTypeID)
	template := in.RegisterStructInstance(name, decl, []types.TypeID{param})
	concreteReceiver := in.RegisterStructInstance(name, decl, []types.TypeID{in.Builtins().Int})

	genericHook := cloneTestHook(in, generic, "app|generic.sg:1:2|__clone", template)
	genericHook.TemplateParams = []types.TypeID{param}
	genericHook.TypeParams = []string{"T"}
	genericHook.ReceiverTemplateArity = 1
	genericHook.Public = true
	concreteHook := cloneTestHook(in, 11, "app|concrete.sg:1:2|__clone", concreteReceiver)

	for _, candidates := range [][]CallableCandidate{
		{genericHook, concreteHook}, {concreteHook, genericHook},
	} {
		selector := newCloneCanonicalSelector(candidates, in)
		hook, err := selector.Resolve(concreteReceiver, CloneUseView{AccessModule: "app", SourceKey: "main.sg"}, decl, "Box<int>")
		if err != nil {
			t.Fatalf("concrete-over-generic resolution: %v", err)
		}
		if hook.Callee != concreteHook.Symbol {
			t.Fatalf("overlap selected %d, want the concrete body %d", hook.Callee, concreteHook.Symbol)
		}
	}
}

func TestCloneSelectorRejectsEquallySpecificBodies(t *testing.T) {
	in := deferredResolverTestInterner()
	receiver := in.RegisterStruct(in.Strings.Intern("Value"), source.Span{File: 1, Start: 1, End: 2})
	left := cloneTestHook(in, 10, "left|left/hook.sg:1:2|__clone", receiver)
	left.ModulePath, left.SourceKey = "left", "left/hook.sg"
	left.Source = source.Span{File: 2, Start: 40, End: 47}
	right := cloneTestHook(in, 11, "right|right/hook.sg:1:2|__clone", receiver)
	right.ModulePath, right.SourceKey = "right", "right/hook.sg"
	right.Source = source.Span{File: 3, Start: 10, End: 17}

	var first string
	for _, candidates := range [][]CallableCandidate{{left, right}, {right, left}} {
		selector := newCloneCanonicalSelector(candidates, in)
		_, err := selector.Resolve(receiver, CloneUseView{AccessModule: "app", SourceKey: "main.sg"}, source.Span{File: 1, Start: 5, End: 9}, "Value")
		var canonicality *CloneCanonicalityError
		if !errors.As(err, &canonicality) {
			t.Fatalf("equally specific bodies did not conflict: %v", err)
		}
		diagnostic := canonicality.Diagnostic()
		if diagnostic == nil || diagnostic.Code != diag.SemaCloneHookConflict || len(diagnostic.Notes) != 2 {
			t.Fatalf("conflict diagnostic = %+v", diagnostic)
		}
		rendered := diagnostic.Message
		for _, note := range diagnostic.Notes {
			rendered += "|" + note.Span.String() + " " + note.Msg
		}
		if first == "" {
			first = rendered
			continue
		}
		if rendered != first {
			t.Fatalf("conflict rendering depends on candidate order:\n%s\n%s", first, rendered)
		}
	}
	if !strings.Contains(first, "left/hook.sg") && !strings.Contains(first, "left") {
		t.Fatalf("conflict notes did not name the declarations: %s", first)
	}
}

func TestCloneSelectorMemoizesOneAnswerPerType(t *testing.T) {
	in := deferredResolverTestInterner()
	receiver := in.RegisterStruct(in.Strings.Intern("Value"), source.Span{File: 1, Start: 1, End: 2})
	hook := cloneTestHook(in, 10, "app|main.sg:1:2|__clone", receiver)
	selector := newCloneCanonicalSelector([]CallableCandidate{hook}, in)

	first := selector.Select(receiver)
	second := selector.Select(receiver)
	if first.kind != cloneSelectionResolved || second.kind != cloneSelectionResolved {
		t.Fatalf("memoized selection kinds = %v, %v", first.kind, second.kind)
	}
	if first.hook.BodyKey != second.hook.BodyKey || first.hook.Callee != second.hook.Callee {
		t.Fatalf("memoized selection changed: %+v vs %+v", first.hook, second.hook)
	}
	if len(selector.cache) != 1 {
		t.Fatalf("selector cached %d entries for one type", len(selector.cache))
	}
}

func TestCloneSelectorGivesAliasAndTargetTheSameBody(t *testing.T) {
	in := deferredResolverTestInterner()
	base := in.RegisterStruct(in.Strings.Intern("Base"), source.Span{File: 1, Start: 1, End: 2})
	alias := in.RegisterAlias(in.Strings.Intern("Alias"), source.Span{File: 1, Start: 3, End: 4})
	in.SetAliasTarget(alias, base)
	hook := cloneTestHook(in, 10, "app|main.sg:1:2|__clone", base)
	selector := newCloneCanonicalSelector([]CallableCandidate{hook}, in)

	view := CloneUseView{AccessModule: "app", SourceKey: "main.sg"}
	direct, err := selector.Resolve(base, view, source.Span{}, "Base")
	if err != nil {
		t.Fatalf("resolve base receiver: %v", err)
	}
	through, err := selector.Resolve(alias, view, source.Span{}, "Alias")
	if err != nil {
		t.Fatalf("resolve alias receiver: %v", err)
	}
	if direct.BodyKey != through.BodyKey {
		t.Fatalf("alias clones through %q but its target clones through %q", through.BodyKey, direct.BodyKey)
	}
}

func TestCloneSelectorSeparatesMissingHookFromWrongShape(t *testing.T) {
	in := deferredResolverTestInterner()
	receiver := in.RegisterStruct(in.Strings.Intern("Value"), source.Span{File: 1, Start: 1, End: 2})
	other := in.RegisterStruct(in.Strings.Intern("Other"), source.Span{File: 1, Start: 3, End: 4})
	byValue := cloneTestHook(in, 10, "app|main.sg:1:2|__clone", receiver)
	byValue.ParamTypes = []types.TypeID{receiver}

	empty := newCloneCanonicalSelector(nil, in)
	if _, err := empty.Resolve(receiver, CloneUseView{}, source.Span{}, "Value"); err == nil ||
		!strings.Contains(err.Error(), "is not clonable (no __clone method defined)") {
		t.Fatalf("missing hook error = %v", err)
	}

	shaped := newCloneCanonicalSelector([]CallableCandidate{byValue}, in)
	if _, err := shaped.Resolve(receiver, CloneUseView{}, source.Span{}, "Value"); err == nil ||
		!strings.Contains(err.Error(), "has __clone but with invalid signature") {
		t.Fatalf("wrong shape error = %v", err)
	}
	if _, err := shaped.Resolve(other, CloneUseView{}, source.Span{}, "Other"); err == nil ||
		!strings.Contains(err.Error(), "is not clonable (no __clone method defined)") {
		t.Fatalf("another type's hook was blamed on Other: %v", err)
	}
}

func cloneTestHook(in *types.Interner, sym symbols.SymbolID, key string, receiver types.TypeID) CallableCandidate {
	candidate := deferredResolverMethod(sym, key, "__clone", receiver, receiver)
	candidate.ParamTypes = []types.TypeID{in.Intern(types.MakeReference(receiver, false))}
	return candidate
}
