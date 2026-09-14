package driver

import (
	"crypto/sha256"
	"encoding/json"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func genericP0Graph(t *testing.T, tc genericP0Case, fixture originalGenericFixture) {
	t.Helper()
	res, authority, unit := fixture.owner, fixture.authority, fixture.unit
	identity, closure := authority.InstantiationIdentity, authority.InstantiationClosure
	roots, edges := authority.InstantiationGraph.Roots(), authority.InstantiationGraph.Edges()
	logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_p0_graph", "case": tc.name,
		"roots": roots, "edges": edges, "closure": closure, "publication": unit.Publication,
		"seeds": authority.InstantiationCallableSeeds, "calls": authority.FunctionCallEdges})
	if identity == nil || identity.ResolveTemplate == nil || identity.ResolveSource == nil || closure == nil ||
		len(roots) != tc.roots || len(edges) != tc.edges || len(closure.Instances) != tc.instances || len(closure.UseSites) != tc.uses {
		t.Fatal("PRECONDITION: source lacks the frozen original/current graph topology")
	}
	candidates := make(map[symbols.SymbolID]sema.CallableCandidate)
	for _, item := range res.Builder.Files.Get(res.FileID).Items {
		fn, present := res.Builder.Items.Fn(item)
		if !present || fn == nil {
			continue
		}
		name, _ := res.Builder.StringsInterner.Lookup(fn.Name)
		candidate := originalGenericSignatureCandidate(t, res, authority, unit, name)
		if candidate.Source != fn.NameSpan || candidate.SourceKey != unit.SourceKey || !candidate.HasBody {
			t.Fatal("PRECONDITION: candidate lost original body syntax")
		}
		candidates[candidate.Symbol] = candidate
	}
	checkSite := func(span source.Span, caller, callee symbols.SymbolID) {
		t.Helper()
		from, fromOK := candidates[caller]
		to, toOK := candidates[callee]
		if !fromOK || !toOK || span.File != res.File.ID || from.SourceKey != to.SourceKey {
			t.Fatal("PRECONDITION: graph request lost original source-owned callables")
		}
		symbol := res.Symbols.Table.Symbols.Get(originalGenericSignatureLocal(t, unit, from))
		fn, found := res.Builder.Items.Fn(symbol.Decl.Item)
		if !found || fn == nil || span.Start < fn.Span.Start || span.End > fn.Span.End {
			t.Fatal("PRECONDITION: witness does not belong to its original caller body")
		}
		matches := 0
		for id, typ := range res.Sema.ExprTypes {
			if node := res.Builder.Exprs.Get(id); node != nil && node.Span == span {
				call, called := res.Builder.Exprs.Call(id)
				if !called || call == nil || node.Kind != ast.ExprCall || typ == types.NoTypeID ||
					res.Symbols.ExprSymbols[id] != originalGenericSignatureLocal(t, unit, to) {
					t.Fatal("PRECONDITION: graph witness differs from the actual typed selected call")
				}
				matches++
			}
		}
		key, err := identity.ResolveSource(span.File)
		if matches != 1 || err != nil || key != unit.SourceKey {
			t.Fatal("PRECONDITION: graph witness lacks one canonical owning expression")
		}
	}
	for _, root := range roots {
		checkSite(root.Witness.Site, root.Witness.Caller, root.Template)
		if root.Kind != sema.InstantiationFunction || root.Witness.SourceKey != unit.SourceKey ||
			len(candidates[root.Witness.Caller].TemplateParams) != 0 || len(root.TemplateArgs) != len(candidates[root.Template].TemplateParams) {
			t.Fatal("PRECONDITION: nongeneric root lost its actual template argument vector")
		}
	}
	for _, edge := range edges {
		checkSite(edge.Witness.Site, edge.Caller, edge.Callee)
		caller := candidates[edge.Caller]
		if edge.Kind != sema.InstantiationFunction || edge.Witness.Caller != edge.Caller || edge.Witness.SourceKey != unit.SourceKey ||
			int(edge.CallerTemplateArity) != len(caller.TemplateParams) || len(edge.CallerBindings) != len(caller.TemplateParams) ||
			len(edge.CalleeTemplateArgs) != len(candidates[edge.Callee].TemplateParams) {
			t.Fatal("PRECONDITION: original edge lacks its full caller binding vector")
		}
		seen := make(map[uint32]bool)
		for _, binding := range edge.CallerBindings {
			info, present := res.Sema.TypeInterner.TypeParamInfo(binding.Param)
			if !present || info == nil || int(binding.ArgIndex) >= len(caller.TemplateParams) || seen[binding.ArgIndex] ||
				binding.Param != caller.TemplateParams[binding.ArgIndex] || info.Index != binding.ParamIndex {
				t.Fatal("PRECONDITION: original binding lost its exact descriptor/index")
			}
			locals := unit.Publication.LocalSymbols(binding.Owner)
			if len(unit.Publication.RootToLocalSymbols) == 0 {
				locals = []symbols.SymbolID{binding.Owner}
			}
			if !slices.Contains(locals, symbols.SymbolID(info.Owner)) {
				t.Fatal("PRECONDITION: original type parameter lost canonical/owning owner relation")
			}
			seen[binding.ArgIndex] = true
		}
	}
	for _, instance := range closure.Instances {
		key, err := sema.NewInstanceKey(*identity, instance.Template, instance.TemplateArgs)
		if err != nil || instance.Key != key || instance.Kind != sema.InstantiationFunction ||
			len(instance.TemplateArgs) != len(candidates[instance.Template].TemplateParams) {
			t.Fatal("PRECONDITION: current instance lacks original canonical template/arguments")
		}
		if len(instance.TemplateArgs) != 1 {
			t.Fatal("PRECONDITION: frozen current instance lost its single concrete argument")
		}
		typ, present := res.Sema.TypeInterner.Lookup(instance.TemplateArgs[0])
		borrowed := tc.name == "opaque_borrowed_result"
		if !present || borrowed && (typ.Kind != types.KindReference || typ.Elem != res.Sema.TypeInterner.Builtins().String) ||
			!borrowed && instance.TemplateArgs[0] != res.Sema.TypeInterner.Builtins().String {
			t.Fatal("PRECONDITION: owned/borrowed concrete substitution changed")
		}
	}
	for _, use := range closure.UseSites {
		checkSite(use.Site, use.CallerTemplate, use.CalleeTemplate)
		instance, present := closure.Lookup(use.Callee)
		if !present || instance.Template != use.CalleeTemplate || !slices.Equal(instance.TemplateArgs, use.TemplateArgs) ||
			use.SourceKey != unit.SourceKey || use.Kind != sema.InstantiationFunction {
			t.Fatal("PRECONDITION: current use disagrees with its finalized callee instance")
		}
		if use.Caller != (sema.InstanceKey{}) {
			caller, found := closure.Lookup(use.Caller)
			if !found || caller.Template != use.CallerTemplate || !slices.Equal(caller.TemplateArgs, use.CallerTemplateArgs) {
				t.Fatal("PRECONDITION: inner use lost its exact current generic caller")
			}
		} else if len(candidates[use.CallerTemplate].TemplateParams) != 0 || len(use.CallerTemplateArgs) != 0 {
			t.Fatal("PRECONDITION: generic caller was relabelled as an original root")
		}
	}
}

// All units are retained. Only source-local descriptors/AST nodes are added;
// existing diagnostics and the actual analysis are logged without truncation.
func genericP0Evidence(t *testing.T, tc genericP0Case, fixture originalGenericFixture) {
	t.Helper()
	res := fixture.owner
	var units, expressions, statements, descriptors, requests []map[string]any
	for _, unit := range fixture.inputs.units {
		file := res.FileSet.Get(unit.Builder.Files.Get(unit.FileID).Span.File)
		bag := fixture.inputs.bags[file.ID]
		if bag == nil || bag.HasErrors() {
			t.Fatal("PRECONDITION: an owning unit lacks its unfiltered admitted bag")
		}
		units = append(units, map[string]any{"source_key": unit.SourceKey, "path": file.Path,
			"sha256": sha256.Sum256(file.Content), "raw_bag": bag.Items()})
	}
	wanted := make(map[types.TypeID]bool)
	for _, candidate := range fixture.authority.CallableCandidates {
		if candidate.Source.File == res.File.ID {
			wanted[candidate.ResultType] = true
			for _, typ := range append(slices.Clone(candidate.ParamTypes), candidate.TemplateParams...) {
				wanted[typ] = true
			}
		}
	}
	for raw := uint32(1); raw <= res.Builder.Exprs.Arena.Len(); raw++ {
		id := ast.ExprID(raw)
		node := res.Builder.Exprs.Get(id)
		if node.Span.File != res.File.ID {
			continue
		}
		typ, typed := res.Sema.ExprTypes[id]
		call, _ := res.Builder.Exprs.Call(id)
		compare, _ := res.Builder.Exprs.Compare(id)
		unary, _ := res.Builder.Exprs.Unary(id)
		expressions = append(expressions, map[string]any{"id": id, "node": node, "typed": typed, "type": typ,
			"selected": res.Symbols.ExprSymbols[id], "call": call, "compare": compare, "unary": unary})
		wanted[typ] = typed
	}
	for raw := uint32(1); raw <= res.Builder.Stmts.Arena.Len(); raw++ {
		id := ast.StmtID(raw)
		node := res.Builder.Stmts.Get(id)
		if node.Span.File == res.File.ID {
			statements = append(statements, map[string]any{"id": id, "node": node, "return": res.Builder.Stmts.Return(id),
				"block": res.Builder.Stmts.Block(id), "if": res.Builder.Stmts.If(id), "let": res.Builder.Stmts.Let(id)})
		}
	}
	for _, request := range res.Sema.ReturnSourceDeclarations {
		requests = append(requests, map[string]any{"request": request, "params": request.Params(), "result": request.Result(),
			"validation": sema.ValidateDeclaredReturnSources(res.Sema.TypeInterner, request)})
	}
	queue := make([]types.TypeID, 0, len(wanted))
	for id, present := range wanted {
		if present && id != types.NoTypeID {
			queue = append(queue, id)
		}
	}
	slices.Sort(queue)
	seen := make(map[types.TypeID]bool)
	for len(queue) != 0 {
		id := queue[0]
		queue = queue[1:]
		if id == types.NoTypeID || seen[id] {
			continue
		}
		seen[id] = true
		typ, present := res.Sema.TypeInterner.Lookup(id)
		param, _ := res.Sema.TypeInterner.TypeParamInfo(id)
		fn, _ := res.Sema.TypeInterner.FnInfo(id)
		union, _ := res.Sema.TypeInterner.UnionInfo(id)
		if !present {
			t.Fatal("PRECONDITION: recorded source descriptor is unavailable")
		}
		row := map[string]any{"id": id, "type": typ, "param": param, "fn": fn, "union": union}
		if fn != nil {
			row["return_sources_all"], row["return_sources_slots"] = fn.ReturnSources().IsAllInputs(), fn.ReturnSources().Slots()
		}
		descriptors = append(descriptors, row)
		queue = append(queue, typ.Elem)
		if fn != nil {
			queue = append(queue, fn.Params...)
			queue = append(queue, fn.Result)
		}
		if union != nil {
			queue = append(queue, union.TypeArgs...)
			for _, member := range union.Members {
				queue = append(queue, member.Type)
				queue = append(queue, member.TagArgs...)
			}
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_p0_typed", "case": tc.name, "units": units,
		"expressions": expressions, "statements": statements, "descriptors": descriptors, "declarations": requests,
		"candidates": fixture.authority.CallableCandidates, "binding_types": res.Sema.BindingTypes,
		"symbols": res.Symbols.Table.Symbols.Data(), "scopes": res.Symbols.Table.Scopes.Data(), "borrows": res.Sema.Borrows})
}

func genericP0InnerAuthority(t *testing.T, fixture originalGenericFixture) {
	t.Helper()
	original := fixture.authority.InstantiationClosure
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	index := -1
	for i, use := range original.UseSites {
		if use.Caller != (sema.InstanceKey{}) {
			if index != -1 {
				t.Fatal("PRECONDITION: owned relay does not have one distinct inner use")
			}
			index = i
		}
	}
	if index == -1 {
		t.Fatal("PRECONDITION: owned relay has no current generic caller")
	}
	for _, name := range []string{"missing_inner_use", "rebound_inner_caller"} {
		var closure sema.InstantiationClosure
		if err := json.Unmarshal(raw, &closure); err != nil {
			t.Fatal(err)
		}
		if name == "missing_inner_use" {
			closure.UseSites = slices.Delete(closure.UseSites, index, index+1)
		} else {
			old := closure.UseSites[index].CallerTemplateArgs
			replacement := fixture.authority.TypeInterner.Builtins().Int64
			if len(old) != 1 || old[0] == replacement {
				t.Fatal("PRECONDITION: no existing incompatible caller argument")
			}
			closure.UseSites[index].CallerTemplateArgs = []types.TypeID{replacement}
		}
		changed := *fixture.authority
		changed.InstantiationClosure = &closure
		units := slices.Clone(fixture.inputs.units)
		units[0].Sema = &changed
		analysis, err := sema.AnalyzeReturnOrigins(t.Context(), &changed, units)
		logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_p0_inner_observation", "case": name,
			"original_use": original.UseSites[index], "changed_closure": closure, "analysis": analysis,
			"error": errorReturnOriginCallText(err), "semantic_complete": analysis != nil && err == nil && analysis.Complete()})
	}
	after, err := json.Marshal(original)
	if err != nil || !slices.Equal(raw, after) {
		t.Fatal("original authority mutated during detached observations")
	}
}

func genericP0NoReturn(t *testing.T, fixture originalGenericFixture, candidate sema.CallableCandidate) {
	t.Helper()
	res, unit := fixture.owner, fixture.unit
	local := originalGenericSignatureLocal(t, unit, candidate)
	sym := res.Symbols.Table.Symbols.Get(local)
	fn, ok := res.Builder.Items.Fn(sym.Decl.Item)
	if !ok || fn == nil || len(candidate.ParamTypes) != 1 || candidate.ParamTypes[0] != candidate.ResultType {
		t.Fatal("PRECONDITION: no-return body lost original borrowed input/result")
	}
	body := res.Builder.Stmts.Block(fn.Body)
	if body == nil || len(body.Stmts) != 2 {
		t.Fatal("PRECONDITION: no-return body must contain exactly loop then return")
	}
	loop, returned := res.Builder.Stmts.While(body.Stmts[0]), res.Builder.Stmts.Return(body.Stmts[1])
	if loop == nil || returned == nil {
		t.Fatal("PRECONDITION: no-return body lost loop/return order")
	}
	cond, literal := res.Builder.Exprs.Literal(loop.Cond)
	loopBody := res.Builder.Stmts.Block(loop.Body)
	paramID := res.Symbols.ExprSymbols[returned.Expr]
	param := res.Symbols.Table.Symbols.Get(paramID)
	if !literal || cond.Kind != ast.ExprLitTrue || loopBody == nil || len(loopBody.Stmts) != 0 ||
		res.Sema.ExprTypes[loop.Cond] != res.Sema.TypeInterner.Builtins().Bool || param == nil || param.Kind != symbols.SymbolParam ||
		param.Decl.Item != sym.Decl.Item || param.Decl.ASTFile != res.FileID || param.Type != candidate.ParamTypes[0] ||
		res.Sema.ExprTypes[returned.Expr] != param.Type || res.Builder.Stmts.Get(body.Stmts[0]).Span.End >= res.Builder.Stmts.Get(body.Stmts[1]).Span.Start {
		t.Fatal("PRECONDITION: literal infinite loop/unreachable return lost exact typed parameter evidence")
	}
	id := genericP0Expression(t, res, "never(value)", "never(value)")
	if res.Symbols.ExprSymbols[id] != local || res.Sema.ExprTypes[id] != candidate.ResultType {
		t.Fatal("PRECONDITION: probe lost selected no-return body/result")
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": "generic_p0_noreturn", "candidate": candidate,
		"loop": loop, "condition": cond, "loop_body": loopBody, "unreachable_return": returned, "parameter": param,
		"selected_call": id, "call_type": res.Sema.ExprTypes[id]})
}
