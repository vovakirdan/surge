package sema

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestReturnSourceRequirementTransport(t *testing.T) {
	for _, name := range []string{"from_spec", "substitution", "merged_contracts", "merged_members", "duplicate", "clone", "explicit_self", "implicit_receiver", "full_identity"} {
		t.Run(name, func(t *testing.T) {
			if name == "explicit_self" || name == "implicit_receiver" {
				checkReturnSourceReceiverAlignment(t, name == "implicit_receiver")
				return
			}
			declarations := `contract C<T> { fn choose(self: T, @return_source left: &string, right: &string) -> &string; }`
			bounds := "C<E>"
			if name == "merged_contracts" {
				declarations += ` contract D<T> { fn choose(self: T, left: &string, @return_source right: &string) -> &string; }`
				bounds += " + D<E>"
			}
			if name == "merged_members" {
				declarations = `contract C<T> {
    @overload fn choose(self: T, @return_source left: &string, right: &string) -> &string;
    @overload fn choose(self: T, left: &string, @return_source right: &string) -> &string;
}`
			}
			src := declarations + " fn use<E: " + bounds + ">(e: E, left: &string, right: &string) -> &string { return e.choose(left, right); }"
			tc, bag, syms := newContractChecker(t, src)
			owner := lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("C"))
			contract := syms.Table.Symbols.Get(owner)
			use := lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("use"))
			info, ok := tc.types.FnInfo(syms.Table.Symbols.Get(use).Type)
			if !ok || info == nil || len(info.Params) != 3 {
				t.Fatal("source generic caller has no typed physical signature")
			}
			receiver, args := info.Params[0], info.Params[1:]
			_, req, ok := tc.boundMethodRequirement(receiver, "choose", args)
			if !ok || bag.HasErrors() {
				t.Fatalf("source bound requirement rejected: %s", diagnosticsSummary(bag))
			}
			records := req.ReturnSourceRequirements()
			wantCount := 1
			if name == "merged_contracts" || name == "merged_members" {
				wantCount = 2
			}
			if len(records) != wantCount || len(req.Params) != 3 {
				t.Fatalf("lost exact source obligations or physical receiver: records=%d params=%d", len(records), len(req.Params))
			}
			for _, record := range records {
				if !record.Contract.IsValid() || record.Member.End <= record.Member.Start || record.ReceiverPrefix != 0 || record.Sources.IsAllInputs() {
					t.Fatalf("bad original member record: %+v", record)
				}
			}
			switch name {
			case "from_spec":
				method := contract.Contract.Methods[tc.builder.StringsInterner.Intern("choose")][0]
				if records[0].Contract != owner || records[0].Member != method.Span || !records[0].Sources.Equal(method.ReturnSourceSyntax.Sources()) {
					t.Fatal("bound requirement lost its selected original ContractSpec member")
				}
				if pushed := tc.pushTypeParams(owner, specsFromSymbolParams(contract.TypeParamSymbols), nil); pushed {
					defer tc.popTypeParams()
				}
				astReq, valid := tc.contractMethodRequirement(tc.builder.Items.ContractFn(ast.ContractFnID(1)), contract.Scope)
				if !valid || bag.HasErrors() || astReq.span != records[0].Member || !astReq.returnSources.Equal(records[0].Sources) {
					t.Fatal("AST fallback disagrees with original typed member")
				}
				checkImportedReturnSourceContract(t)
			case "substitution":
				copied := symbols.CloneContractSpec(contract.Contract)
				concrete := tc.instantiateContractRequirements(contract, copied, []types.TypeID{args[0]})
				method := concrete.methods[tc.builder.StringsInterner.Intern("choose")][0]
				if method.params[0] != args[0] || method.span != records[0].Member || !method.returnSources.Equal(records[0].Sources) ||
					!types.ContainsGenericParam(tc.types, copied.Methods[method.name][0].Params[0]) {
					t.Fatal("copied/substituted requirement lost original provenance or modified template")
				}
			case "merged_contracts", "merged_members":
				if records[0].Sources.Equal(records[1].Sources) || records[0].Member == records[1].Member ||
					(records[0].Contract == records[1].Contract) != (name == "merged_members") {
					t.Fatal("distinct same-shape source obligations were collapsed")
				}
				if name == "merged_members" && len(req.Contracts) != 1 {
					t.Fatal("legacy contract owner set changed")
				}
			case "duplicate":
				bounds := tc.typeParamContractBounds(receiver)
				tc.typeParamBounds[receiver] = append(slices.Clone(bounds), bounds...)
				_, duplicate, valid := tc.boundMethodRequirement(receiver, "choose", args)
				if !valid || len(duplicate.ReturnSourceRequirements()) != 1 {
					t.Fatal("duplicate original requirement was not collapsed")
				}
				conflict := records[0]
				conflict.Sources = types.ExplicitReturnSources(2)
				defer func() {
					if value := recover(); value == nil || !strings.Contains(fmt.Sprint(value), "return-source metadata conflict") {
						t.Fatalf("inconsistent original member did not fail explicitly: %v", value)
					}
				}()
				mergeReturnSourceRequirements(records, []ReturnSourceRequirement{conflict})
			case "clone":
				copied := cloneDeferredCallableRequirement(&req)
				copied.returnSourceRequirements[0].Contract = symbols.NoSymbolID
				records[0].Member = source.Span{}
				if !req.ReturnSourceRequirements()[0].Contract.IsValid() || req.ReturnSourceRequirements()[0].Member == (source.Span{}) {
					t.Fatal("requirement clone/getter exposed retained records")
				}
			case "full_identity":
				copied := cloneDeferredCallableRequirement(&req)
				copied.returnSourceRequirements[0].Sources = types.ExplicitReturnSources(2)
				if !deferredRequirementShapesEqual(&req, &copied) || deferredRequirementsEqual(&req, &copied) {
					t.Fatal("ordinary shape/full identity confused source promises")
				}
				copied = cloneDeferredCallableRequirement(&req)
				copied.returnSourceRequirements[0].ReceiverPrefix = 1
				if deferredRequirementsEqual(&req, &copied) || len(mergeReturnSourceRequirements(req.returnSourceRequirements, copied.returnSourceRequirements)) != 2 {
					t.Fatal("distinct receiver alignments were erased")
				}
			}
		})
	}
}

func checkReturnSourceReceiverAlignment(t *testing.T, implicit bool) {
	t.Helper()
	src := `contract C<T> { fn choose(@return_source self: T, other: T) -> T; }
type Foo = {}
extern<Foo> { fn choose(@return_source self: &Foo, other: &Foo) -> &Foo; }`
	wantPrefix := uint8(0)
	if implicit {
		src = `contract C<T> { fn choose(@return_source other: T) -> T; }
type Foo = {}
extern<Foo> { fn choose(self: &Foo, @return_source other: &Foo) -> &Foo; }`
		wantPrefix = 1
	}
	tc, bag, syms := newContractChecker(t, src)
	owner := lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("C"))
	contract := syms.Table.Symbols.Get(owner)
	foo := syms.Table.Symbols.Get(lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("Foo")))
	ref := tc.types.Intern(types.MakeReference(foo.Type, false))
	bound := symbols.BoundInstance{Contract: owner, GenericArgs: []types.TypeID{ref}, Span: contract.Span}
	if !tc.checkContractSatisfaction(ref, bound, contract.Span, "") || bag.HasErrors() {
		t.Fatalf("source receiver contract did not match: %s", diagnosticsSummary(bag))
	}
	name := tc.builder.StringsInterner.Intern("choose")
	reqs, ok := tc.requirementsForBound(bound)
	if !ok || len(reqs.methods[name]) != 1 {
		t.Fatal("source did not retain its exact requirement")
	}
	req := reqs.methods[name][0]
	aligned, ok := alignContractMethodRequirement(&req, ref, 2)
	if !ok || aligned.receiverPrefix != wantPrefix || req.receiverPrefix != 0 ||
		!slices.Equal(req.returnSources.Slots(), []uint32{0}) || !aligned.returnSources.Equal(req.returnSources) || len(aligned.params) != 2 {
		t.Fatal("receiver alignment lost or shifted original declaration slots")
	}
	again, ok := alignContractMethodRequirement(&aligned, ref, 2)
	if !ok || again.receiverPrefix != wantPrefix {
		t.Fatal("already aligned requirement was shifted twice")
	}
	matched := false
	for _, candidate := range tc.methodsForType(ref, name) {
		if match, _ := tc.contractSignatureMatches(&aligned, candidate); !match {
			continue
		}
		if !slices.Equal(candidate.returnSources.Slots(), []uint32{uint32(wantPrefix)}) {
			t.Fatal("selected source implementation lost its physical source index")
		}
		matched = true
	}
	if !matched {
		t.Fatal("receiver source fixture had no matching typed implementation")
	}
	found := false
	for _, original := range tc.result.ReturnSourceDeclarations {
		if original.Owner == owner {
			found = true
			if original.Syntax.Span() != req.span || ValidateDeclaredReturnSources(tc.types, original).Status != ReturnSourcesDeferred ||
				ValidateInstantiatedReturnSources(tc.types, original, req.params, req.result).Status != ReturnSourcesValid {
				t.Fatal("receiver specialization invented a new concrete declaration")
			}
		}
	}
	if !found {
		t.Fatal("receiver fixture lost the original conditional generic promise")
	}
}
