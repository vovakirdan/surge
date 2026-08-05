package sema

import (
	"fmt"
	"testing"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func testInstantiationRoot(template symbols.SymbolID, arg types.TypeID, start uint32) *InstantiationRoot {
	return &InstantiationRoot{
		Kind:         InstantiationFunction,
		Template:     template,
		TemplateArgs: []types.TypeID{arg},
		Witness: InstantiationWitness{
			Site:   source.Span{File: 1, Start: start, End: start + 1},
			Caller: 1,
			Reason: "call",
		},
	}
}

func testInstantiationEdge(caller, callee symbols.SymbolID, arg types.TypeID, start uint32) *InstantiationEdge {
	return &InstantiationEdge{
		Kind:                InstantiationFunction,
		Caller:              caller,
		CallerTemplateArity: 1,
		CallerBindings:      []InstantiationParamBinding{{Owner: caller, ParamIndex: 0, ArgIndex: 0}},
		Callee:              callee,
		CalleeTemplateArgs:  []types.TypeID{arg},
		Witness: InstantiationWitness{
			Site:   source.Span{File: 1, Start: start, End: start + 1},
			Caller: caller,
			Reason: "call",
		},
	}
}

func requireInstantiationClosure(t *testing.T, graph *InstantiationGraph, in *types.Interner, maxDepth int) InstantiationClosure {
	t.Helper()
	closure, err := BuildInstantiationClosure(graph, testInstantiationIdentity(in), maxDepth)
	if err != nil {
		t.Fatalf("build closure: %v", err)
	}
	return closure
}

func requireClosureTemplate(t *testing.T, closure InstantiationClosure, template symbols.SymbolID) InstantiationInstance {
	t.Helper()
	for _, instance := range closure.Instances {
		if instance.Template == template {
			return instance
		}
	}
	t.Fatalf("closure has no symbol %d: %+v", template, closure.Instances)
	return InstantiationInstance{}
}

func testInstantiationIdentity(in *types.Interner) InstantiationIdentity {
	return InstantiationIdentity{
		Types: types.CanonicalKeyContext{
			Types: in,
			ResolveNominal: func(kind types.Kind, name string, decl source.Span) (string, error) {
				return fmt.Sprintf("%s/%s/%d/%d", kind, name, decl.Start, decl.End), nil
			},
		},
		ResolveTemplate: func(id symbols.SymbolID) (string, error) {
			return fmt.Sprintf("sym/%d", id), nil
		},
		ResolveSource: func(id source.FileID) (string, error) {
			return fmt.Sprintf("file/%d", id), nil
		},
	}
}

func ptrGraph(graph InstantiationGraph) *InstantiationGraph { return &graph }
