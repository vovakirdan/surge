package sema

import (
	"reflect"
	"strings"
	"testing"

	"surge/internal/symbols"
)

func TestPublishFinalizationDecisionsUsesCanonicalCallableIdentity(t *testing.T) {
	authority := &Result{
		CallableCandidates: []CallableCandidate{
			{Symbol: 10, BodyKey: "app|main-body", SourceKey: "app/main.sg"},
			{Symbol: 20, BodyKey: "lib|parser-body", SourceKey: "lib/parser.sg"},
		},
		EntrypointCallableBindings: []EntrypointCallableBinding{{
			Entrypoint: 10,
			Callee:     20,
			CalleeKey:  "lib|parser-body",
			SourceKey:  "app/main.sg",
		}},
	}

	orders := []struct {
		name       string
		entrypoint []symbols.SymbolID
		callee     []symbols.SymbolID
		callables  []FinalizationCallableIdentity
	}{
		{
			name:       "forward",
			entrypoint: []symbols.SymbolID{101, 102},
			callee:     []symbols.SymbolID{201, 202},
			callables: []FinalizationCallableIdentity{
				{Symbol: 101, BodyKey: "app|other-body", SourceKey: "app/other.sg"},
				{Symbol: 102, BodyKey: "app|main-body", SourceKey: "app/main.sg"},
				{Symbol: 201, BodyKey: "lib|other-parser", SourceKey: "lib/other.sg"},
				{Symbol: 202, BodyKey: "lib|parser-body", SourceKey: "lib/parser.sg"},
			},
		},
		{
			name:       "reversed",
			entrypoint: []symbols.SymbolID{102, 101},
			callee:     []symbols.SymbolID{202, 201},
			callables: []FinalizationCallableIdentity{
				{Symbol: 202, BodyKey: "lib|parser-body", SourceKey: "lib/parser.sg"},
				{Symbol: 201, BodyKey: "lib|other-parser", SourceKey: "lib/other.sg"},
				{Symbol: 102, BodyKey: "app|main-body", SourceKey: "app/main.sg"},
				{Symbol: 101, BodyKey: "app|other-body", SourceKey: "app/other.sg"},
			},
		},
	}

	for _, order := range orders {
		t.Run(order.name, func(t *testing.T) {
			dst := &Result{}
			err := PublishFinalizationDecisions(dst, authority, FinalizationPublication{
				SourceKey: "app/main.sg",
				RootToLocalSymbols: map[symbols.SymbolID][]symbols.SymbolID{
					10: order.entrypoint,
					20: order.callee,
				},
				LocalCallables: order.callables,
			})
			if err != nil {
				t.Fatalf("publish decisions: %v", err)
			}
			if len(dst.EntrypointCallableBindings) != 1 {
				t.Fatalf("bindings = %+v", dst.EntrypointCallableBindings)
			}
			binding := dst.EntrypointCallableBindings[0]
			if binding.Entrypoint != 102 || binding.Callee != 202 {
				t.Fatalf("localized binding = %+v, want entrypoint=102 callee=202", binding)
			}
		})
	}
}

func TestPublishFinalizationDecisionsFailsClosedWithoutMutation(t *testing.T) {
	authority := &Result{
		CallableCandidates: []CallableCandidate{
			{Symbol: 10, BodyKey: "app|main-body", SourceKey: "app/main.sg"},
			{Symbol: 20, BodyKey: "lib|parser-body", SourceKey: "lib/parser.sg"},
		},
		EntrypointCallableBindings: []EntrypointCallableBinding{{
			Entrypoint: 10, Callee: 20, CalleeKey: "lib|parser-body", SourceKey: "app/main.sg",
		}},
	}

	for _, tc := range []struct {
		name      string
		callables []FinalizationCallableIdentity
		want      string
	}{
		{
			name: "missing",
			callables: []FinalizationCallableIdentity{
				{Symbol: 101, BodyKey: "app|main-body", SourceKey: "app/main.sg"},
			},
			want: "no local callable",
		},
		{
			name: "ambiguous",
			callables: []FinalizationCallableIdentity{
				{Symbol: 101, BodyKey: "app|main-body", SourceKey: "app/main.sg"},
				{Symbol: 201, BodyKey: "lib|parser-body", SourceKey: "lib/parser.sg"},
				{Symbol: 202, BodyKey: "lib|parser-body", SourceKey: "lib/parser.sg"},
			},
			want: "ambiguous local callable",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sentinel := []EntrypointCallableBinding{{Entrypoint: 77, Callee: 88}}
			dst := &Result{EntrypointCallableBindings: sentinel}
			err := PublishFinalizationDecisions(dst, authority, FinalizationPublication{
				SourceKey: "app/main.sg",
				RootToLocalSymbols: map[symbols.SymbolID][]symbols.SymbolID{
					10: {101},
					20: {201, 202},
				},
				LocalCallables: tc.callables,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if !reflect.DeepEqual(dst.EntrypointCallableBindings, sentinel) {
				t.Fatalf("destination mutated after failed publication: %+v", dst.EntrypointCallableBindings)
			}
		})
	}
}

func TestMergeInstantiationGraphsCanonicalizesCallableAliasesByBody(t *testing.T) {
	dst := &Result{CallableCandidates: []CallableCandidate{
		{Symbol: 10, BodyKey: "lib|parser-body"},
		{Symbol: 30, BodyKey: "app|main-body"},
	}}
	src := &Result{
		CallableCandidates: []CallableCandidate{
			{Symbol: 40, BodyKey: "app|main-body"},
			{Symbol: 20, BodyKey: "lib|parser-body"},
		},
		EntrypointCallableRequests: []EntrypointCallableRequest{{Entrypoint: 40}},
	}

	remap := CanonicalizeInstantiationCallableAliases(dst, src, nil)
	MergeInstantiationGraphs(dst, src, remap)
	if remap[20] != 10 || remap[40] != 30 {
		t.Fatalf("canonical alias remap = %+v", remap)
	}
	if len(dst.CallableCandidates) != 2 {
		t.Fatalf("canonical candidates = %+v", dst.CallableCandidates)
	}
	if len(dst.EntrypointCallableRequests) != 1 || dst.EntrypointCallableRequests[0].Entrypoint != 30 {
		t.Fatalf("canonical entrypoint requests = %+v", dst.EntrypointCallableRequests)
	}
}
