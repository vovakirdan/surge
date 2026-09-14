package driver

import (
	"crypto/sha256"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

// P0 certifies capture/admission, never the future P1 origin/effect verdict.
// The whole owning-unit input and actual current Pending/error remain visible.
func TestCaptureReturnOriginStorageP0(t *testing.T) {
	if len(storageP0Cases) != 12 {
		t.Fatal("frozen P0 source roster changed")
	}
	for _, tc := range storageP0Cases {
		t.Run(tc.name, func(t *testing.T) {
			logReturnOriginCallEvidence(t, map[string]any{"p0_case": tc.name, "stage": "source",
				"source": tc.source, "source_sha256": sha256.Sum256([]byte(tc.source))})
			res := returnOriginStdlibFixture(t, tc.source, false)
			closureErr := FinalizeInstantiationClosure(t.Context(), res, 64)
			logReturnOriginCallEvidence(t, map[string]any{"p0_case": tc.name, "stage": "closure",
				"closure_error": errorReturnOriginCallText(closureErr), "closure": res.Sema.InstantiationClosure,
				"roots": res.Sema.InstantiationGraph.Roots(), "edges": res.Sema.InstantiationGraph.Edges(),
				"deferred": res.Sema.InstantiationGraph.DeferredCallables()})
			checkReturnOriginStdlibBags(t, res, false)
			if closureErr != nil || res.Sema.InstantiationClosure == nil {
				t.Fatal("PRECONDITION: actual generic closure did not finish")
			}
			inputs, err := collectReturnOriginUnits(res)
			if err != nil {
				t.Fatalf("PRECONDITION: original owning-unit collection: %v", err)
			}
			rootKey, _ := checkReturnOriginCloneUnits(t, res, inputs.units)
			for _, unit := range inputs.units {
				if unit.SourceKey == rootKey {
					captureStorageP0Bindings(t, tc.name, unit)
				}
			}
			for _, target := range tc.targets {
				captureStorageP0Target(t, res, inputs.units, rootKey, tc.name, target)
			}
			if tc.name == "p09_alias_coalescence" {
				checkStorageP0Coalescence(t, res)
			}
			analysis, analysisErr := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"p0_case": tc.name, "stage": "actual_origin",
				"analysis": analysis, "analysis_error": errorReturnOriginCallText(analysisErr),
				"analysis_complete": analysis.Complete(), "owning_units": len(inputs.units)})
			if analysis == nil && analysisErr == nil {
				t.Fatal("origin capture produced neither a result nor an explicit error")
			}
			checkReturnOriginStdlibBags(t, res, false)
		})
	}
}

func captureStorageP0Bindings(t *testing.T, name string, unit sema.ReturnOriginUnit) {
	t.Helper()
	file := unit.Builder.Files.Get(unit.FileID).Span.File
	for i, sym := range unit.Symbols.Table.Symbols.Data() {
		if sym.Span.File == file && sym.Decl.ASTFile == unit.FileID {
			logReturnOriginCallEvidence(t, map[string]any{"p0_case": name, "stage": "original_symbol",
				"source_key": unit.SourceKey, "symbol_id": i + 1, "symbol": sym})
		}
	}
	for i, scope := range unit.Symbols.Table.Scopes.Data() {
		if scope.Owner.SourceFile == file && scope.Owner.ASTFile == unit.FileID {
			logReturnOriginCallEvidence(t, map[string]any{"p0_case": name, "stage": "original_scope",
				"source_key": unit.SourceKey, "scope_id": i + 1, "scope": scope})
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"p0_case": name, "stage": "existing_storage_facts",
		"source_key": unit.SourceKey, "binding_types": unit.Sema.BindingTypes, "expr_borrows": unit.Sema.ExprBorrows,
		"borrows": unit.Sema.Borrows, "borrow_bindings": unit.Sema.BorrowBindings, "borrow_events": unit.Sema.BorrowEvents,
		"scope_end_drops": unit.Sema.ScopeEndDrops, "early_exit_drops": unit.Sema.EarlyExitDrops,
		"reassign_old_drops": unit.Sema.ReassignOldDrops, "expr_types": unit.Sema.ExprTypes,
		"index_symbols": unit.Sema.IndexSymbols, "index_set_symbols": unit.Sema.IndexSetSymbols,
		"range_symbols": unit.Sema.RangeSymbols, "range_types": unit.Sema.RangeTypes})
}

func captureStorageP0Target(t *testing.T, res *DiagnoseResult, units []sema.ReturnOriginUnit, rootKey, name string, target storageP0Target) {
	t.Helper()
	key := target.unit
	if key == "" {
		key = rootKey
	}
	count := 0
	for _, unit := range units {
		if unit.SourceKey != key {
			continue
		}
		file := res.FileSet.Get(unit.Builder.Files.Get(unit.FileID).Span.File)
		ids := make([]ast.ExprID, 0, len(unit.Sema.ExprTypes))
		for id := range unit.Sema.ExprTypes {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			node := unit.Builder.Exprs.Get(id)
			if node == nil || node.Kind != target.kind || node.Span.File != file.ID ||
				int(node.Span.End) > len(file.Content) || node.Span.Start > node.Span.End ||
				string(file.Content[node.Span.Start:node.Span.End]) != target.text {
				continue
			}
			count++
			typ := unit.Sema.ExprTypes[id]
			_, known := unit.Sema.TypeInterner.Lookup(typ)
			selected := unit.Symbols.ExprSymbols[id]
			children := []ast.ExprID{}
			var operation any
			switch node.Kind {
			case ast.ExprCall:
				call, ok := unit.Builder.Exprs.Call(id)
				if !ok || call == nil {
					t.Fatal("PRECONDITION: original call operands are absent")
				}
				operation = call
				children = append(children, call.Target)
				for _, arg := range call.Args {
					children = append(children, arg.Value)
				}
			case ast.ExprIndex:
				index, ok := unit.Builder.Exprs.Index(id)
				if !ok || index == nil {
					t.Fatal("PRECONDITION: original index operands are absent")
				}
				operation, selected = index, unit.Sema.IndexSymbols[id]
				children = append(children, index.Target, index.Index)
			case ast.ExprBinary:
				binary, ok := unit.Builder.Exprs.Binary(id)
				if !ok || binary == nil || binary.Op != ast.ExprBinaryAssign {
					t.Fatal("PRECONDITION: expected an original assignment")
				}
				operation = binary
				children = append(children, binary.Left, binary.Right)
				selected = unit.Sema.IndexSetSymbols[binary.Left]
			}
			logReturnOriginCallEvidence(t, map[string]any{"p0_case": name, "stage": "typed_operation",
				"source_key": key, "source_sha256": sha256.Sum256(file.Content), "text": target.text,
				"expr_id": id, "node": node, "type_id": typ, "type_facts": storageP0TypeFacts(unit.Sema.TypeInterner, typ), "operation": operation,
				"selected_symbol_id": selected, "selected_symbol": unit.Symbols.Table.Symbols.Get(selected),
				"clone_symbol": unit.Sema.CloneSymbols[id], "conversion": unit.Sema.ImplicitConversions[id],
				"deferred_clone_use": unit.Sema.DeferredCallableUses[sema.DeferredUseRef{Expr: id, Kind: sema.DeferredCloneCall}]})
			for _, child := range children {
				childType, present := unit.Sema.ExprTypes[child]
				logReturnOriginCallEvidence(t, map[string]any{"p0_case": name, "stage": "original_operand",
					"source_key": key, "parent": id, "expr_id": child, "node": unit.Builder.Exprs.Get(child),
					"type_present": present, "type_id": childType, "type_facts": storageP0TypeFacts(unit.Sema.TypeInterner, childType),
					"symbol": unit.Symbols.ExprSymbols[child]})
			}
			if typ == types.NoTypeID || !known {
				t.Fatalf("PRECONDITION: %s/%s target %q lacks its actual type", name, key, target.text)
			}
			if target.selected == "native_array_range" {
				captureStorageP0NativeArrayRange(t, unit, name, id)
			} else if target.selected != "" {
				captureStorageP0Selection(t, res, units, unit, name, selected, target.selected)
			}
		}
	}
	if count != target.count {
		t.Fatalf("PRECONDITION: %s/%s target %q count=%d, want %d", name, key, target.text, count, target.count)
	}
}

// The original core self[r] has a native typed index, not a selected magic call.
// A present but invalid entry is a different fact and must never pass this probe.
func captureStorageP0NativeArrayRange(t *testing.T, unit sema.ReturnOriginUnit, name string, id ast.ExprID) {
	t.Helper()
	selected, present := unit.Sema.IndexSymbols[id]
	index, indexed := unit.Builder.Exprs.Index(id)
	logReturnOriginCallEvidence(t, map[string]any{"p0_case": name, "stage": "native_index_selection",
		"source_key": unit.SourceKey, "expr_id": id, "selection_present": present, "selected_symbol_id": selected})
	if present || unit.SourceKey != "core/array.sg" || !indexed || index == nil {
		t.Fatal("PRECONDITION: original core native index must have absent selection")
	}
	in := unit.Sema.TypeInterner
	result := unit.Sema.ExprTypes[id]
	receiver, receiverKnown := in.Lookup(unit.Sema.ExprTypes[index.Target])
	array, arrayKnown := in.StructInfo(result)
	originalArray, originalKnown := in.StructInfo(in.ArrayNominalType())
	indexType := unit.Sema.ExprTypes[index.Index]
	bound, rangeKnown := in.RangeBoundType(indexType)
	rangeInfo, _ := in.StructInfo(indexType)
	logReturnOriginCallEvidence(t, map[string]any{"p0_case": name, "stage": "native_array_range_shape",
		"source_key": unit.SourceKey, "expr_id": id, "span": unit.Builder.Exprs.Get(id).Span,
		"receiver": receiver, "result_type": result, "array": array, "original_array": originalArray,
		"range_type": indexType, "range": rangeInfo, "range_bound": bound})
	if !receiverKnown || receiver.Kind != types.KindReference || receiver.Mutable || receiver.Elem != result ||
		!arrayKnown || array == nil || !originalKnown || originalArray == nil ||
		array.Name != originalArray.Name || array.Decl != originalArray.Decl || len(array.TypeArgs) != 1 ||
		!rangeKnown || bound != in.Builtins().Int || rangeInfo == nil {
		t.Fatal("PRECONDITION: native core index lost its original Array<T>/Range<int>/Array<T> shape")
	}
	if _, known := in.Lookup(array.TypeArgs[0]); !known || array.TypeArgs[0] == types.NoTypeID {
		t.Fatal("PRECONDITION: native core index lost its exact original element descriptor")
	}
}

// Only the selected value and its immediate referent descriptors, never the interner.
func storageP0TypeFacts(in *types.Interner, id types.TypeID) map[string]any {
	typ, known := in.Lookup(id)
	nominal, _ := in.StructInfo(id)
	union, _ := in.UnionInfo(id)
	param, _ := in.TypeParamInfo(id)
	element, elementKnown := in.Lookup(typ.Elem)
	elementNominal, _ := in.StructInfo(typ.Elem)
	return map[string]any{"known": known, "descriptor": typ, "struct": nominal, "union": union,
		"type_param": param, "element_known": elementKnown, "element": element, "element_struct": elementNominal}
}

// Read the existing publication back to the physical original declaration.
// This is evidence only: no operation semantics or replacement type is synthesized.
func captureStorageP0Selection(t *testing.T, res *DiagnoseResult, units []sema.ReturnOriginUnit, caller sema.ReturnOriginUnit, name string, selected symbols.SymbolID, want string) {
	t.Helper()
	var candidate *sema.CallableCandidate
	for i := range res.Sema.CallableCandidates {
		c := &res.Sema.CallableCandidates[i]
		matches := slices.Contains(caller.Publication.LocalSymbols(c.Symbol), selected)
		if len(caller.Publication.RootToLocalSymbols) == 0 {
			matches = c.Symbol == selected
		}
		if matches {
			if candidate != nil {
				t.Fatal("PRECONDITION: selected symbol has multiple canonical candidates")
			}
			candidate = c
		}
	}
	if !selected.IsValid() || candidate == nil || candidate.Name != want || candidate.BodyKey == "" {
		t.Fatalf("PRECONDITION: selected %s has no exact published candidate: symbol=%d candidate=%+v", want, selected, candidate)
	}
	owners := 0
	for _, unit := range units {
		file := unit.Builder.Files.Get(unit.FileID).Span.File
		if file != candidate.Source.File {
			continue
		}
		for _, identity := range unit.Publication.LocalCallables {
			if identity.BodyKey != candidate.BodyKey || identity.SourceKey != candidate.SourceKey {
				continue
			}
			sym := unit.Symbols.Table.Symbols.Get(identity.Symbol)
			if sym == nil || sym.Kind != symbols.SymbolFunction || sym.Span != candidate.Source ||
				sym.Decl.SourceFile != file || sym.Decl.ASTFile != unit.FileID {
				t.Fatal("PRECONDITION: canonical candidate lost its physical original symbol")
			}
			fn, _ := unit.Builder.Items.Fn(sym.Decl.Item)
			if fn == nil {
				for memberID, local := range unit.Symbols.ExternSyms {
					if local == identity.Symbol {
						member := unit.Builder.Items.ExternMember(memberID)
						if member != nil && member.Kind == ast.ExternMemberFn {
							fn = unit.Builder.Items.FnByPayload(member.Fn)
						}
					}
				}
			}
			info, typed := unit.Sema.TypeInterner.FnInfo(sym.Type)
			if fn == nil || fn.NameSpan != candidate.Source || !typed || info == nil ||
				!slices.Equal(info.Params, candidate.ParamTypes) || info.Result != candidate.ResultType {
				t.Fatal("PRECONDITION: selected original AST/FnInfo disagrees with its candidate")
			}
			syntax := symbols.FunctionReturnSourceSyntax(unit.Builder, fn)
			logReturnOriginCallEvidence(t, map[string]any{"p0_case": name, "stage": "selected_original_declaration",
				"source_key": unit.SourceKey, "candidate": candidate, "identity": identity, "symbol": sym,
				"original_fn": fn, "original_fn_info": info, "sources_all_inputs": info.ReturnSources().IsAllInputs(),
				"source_slots": info.ReturnSources().Slots(), "syntax_span": syntax.Span(), "syntax_params": syntax.Params(),
				"syntax_result": syntax.Result(), "syntax_markers": syntax.Markers()})
			owners++
		}
	}
	if owners != 1 {
		t.Fatalf("PRECONDITION: selected %s has %d original owners, want 1", want, owners)
	}
}

func checkStorageP0Coalescence(t *testing.T, res *DiagnoseResult) {
	t.Helper()
	matched := 0
	for _, candidate := range res.Sema.CallableCandidates {
		if candidate.Name != "write_then_read" || candidate.Source.File != res.File.ID {
			continue
		}
		if len(candidate.TemplateParams) != 2 || candidate.TemplateParams[0] == candidate.TemplateParams[1] {
			t.Fatal("PRECONDITION: T and U lost their distinct original descriptors")
		}
		for _, root := range res.Sema.InstantiationGraph.Roots() {
			if root.Kind == sema.InstantiationFunction && root.Template == candidate.Symbol {
				if len(root.TemplateArgs) != 2 || root.TemplateArgs[0] == types.NoTypeID || root.TemplateArgs[0] != root.TemplateArgs[1] {
					t.Fatal("PRECONDITION: distinct original T/U did not coalesce to the same actual type")
				}
				logReturnOriginCallEvidence(t, map[string]any{"p0_case": "p09_alias_coalescence", "stage": "coalesced_formals",
					"candidate": candidate, "original_root": root})
				matched++
			}
		}
	}
	if matched != 1 {
		t.Fatalf("PRECONDITION: coalescence requires one original root, got %d", matched)
	}
}
