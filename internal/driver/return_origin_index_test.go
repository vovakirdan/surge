package driver

import (
	"crypto/sha256"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/types"
)

// These component assertions retain the full original stdlib analysis. They
// do not turn unrelated core Pending into a successful public compilation.
func TestAnalyzeTypedNativeIndexOrigins(t *testing.T) {
	for _, tc := range []struct {
		name, src, owner, escape string
		slots                    []uint32
	}{
		{"borrowed_array", "fn probe(values: &Array<int>) -> &int { return values[0]; }\n", "", "", []uint32{0}},
		{"owned_array_escape", "fn probe(values: Array<int>) -> &int { return values[0]; }\n", "values", "return values[0];", nil},
		{"block_array_escape", "fn probe() -> int { let escaped = { let values = [1]; ret values[0]; }; return 1; }\n", "values", "ret values[0];", nil},
		{"string_scalar", "fn probe(value: &string) -> uint32 { return value[0]; }\n", "", "", nil},
		{"subject_effect", "fn probe(value: &string, a: &string, b: &string) -> &string { let mut alias: &string = a; let code = { let _ = nothing; alias = b; ret value; }[0]; return alias; }\n", "", "", []uint32{2}},
		{"index_effect", "fn probe(value: &string, a: &string, b: &string) -> &string { let mut alias: &string = a; let code = value[{ let _ = nothing; alias = b; ret 0; }]; return alias; }\n", "", "", []uint32{2}},
		{"index_inner_escape", "fn probe(value: &string, outside: &string) -> int { let mut escaped: &string = outside; let code = value[{ let owned: string = \"local\"; escaped = &owned; ret 0; }]; return 1; }\n", "owned", "ret 0;", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("RETURN_ORIGIN_INDEX_SOURCE case=%s sha256=%x source=%q", tc.name, sha256.Sum256([]byte(tc.src)), tc.src)
			res := returnOriginStdlibFixture(t, tc.src, tc.owner != "")
			closureErr := FinalizeInstantiationClosure(t.Context(), res, 64)
			checkReturnOriginStdlibBags(t, res, tc.owner != "")
			inputs, unitsErr := collectReturnOriginUnits(res)
			if closureErr != nil || unitsErr != nil || len(inputs.units) != 11 {
				t.Fatalf("PRECONDITION: missing full original stdlib input: closure=%v units=%v", closureErr, unitsErr)
			}
			rootKey := ""
			seen := make(map[string]bool)
			for _, unit := range inputs.units {
				file := res.FileSet.Get(unit.Builder.Files.Get(unit.FileID).Span.File)
				if seen[unit.SourceKey] || file == nil {
					t.Fatal("PRECONDITION: original owning unit is missing or repeated")
				}
				seen[unit.SourceKey] = true
				logReturnOriginCallEvidence(t, map[string]any{"index_unit": unit.SourceKey, "source_sha256": sha256.Sum256(file.Content), "publication": unit.Publication})
				if file.ID == res.File.ID {
					if string(file.Content) != tc.src || unit.Builder != res.Builder || unit.Sema != res.Sema {
						t.Fatal("PRECONDITION: root source or original typed artifacts changed")
					}
					rootKey = unit.SourceKey
				} else if !strings.HasPrefix(unit.SourceKey, "core/") {
					t.Fatal("PRECONDITION: fixture acquired an unexpected owning unit")
				}
			}
			var probe sema.CallableCandidate
			for _, candidate := range res.Sema.CallableCandidates {
				if candidate.SourceKey == rootKey && candidate.Source.File == res.File.ID && candidate.Name == "probe" {
					probe = candidate
				}
			}
			if rootKey == "" || !probe.Symbol.IsValid() || !probe.HasBody || probe.BodyKey == "" {
				t.Fatal("PRECONDITION: original root probe is missing")
			}
			var id ast.ExprID
			for raw := uint32(1); raw <= res.Builder.Exprs.Arena.Len(); raw++ {
				expr := ast.ExprID(raw)
				if node := res.Builder.Exprs.Get(expr); node.Kind == ast.ExprIndex && node.Span.File == res.File.ID {
					if id.IsValid() {
						t.Fatal("PRECONDITION: frozen source has more than one index")
					}
					id = expr
				}
			}
			index, ok := res.Builder.Exprs.Index(id)
			selected := res.Sema.IndexSymbols[id]
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "index_before_precondition": id, "ast": index,
				"index_type": res.Sema.ExprTypes[id], "selected_index": selected, "selected_symbol": res.Symbols.Table.Symbols.Get(selected),
				"closure": res.Sema.InstantiationClosure, "roots": res.Sema.InstantiationGraph.Roots()})
			if !ok || index == nil || res.Sema.ExprTypes[id] == types.NoTypeID || !selected.IsValid() {
				t.Fatal("PRECONDITION: source did not select its typed index operation")
			}
			checkReturnOriginSelectedIndex(t, res, inputs.units, id, probe)
			in := res.Sema.TypeInterner
			containerID := res.Sema.ExprTypes[index.Target]
			container, present := in.Lookup(containerID)
			if present && container.Kind == types.KindReference {
				containerID = container.Elem
				container, present = in.Lookup(containerID)
			}
			result, typed := in.Lookup(res.Sema.ExprTypes[id])
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "index": id, "ast": index, "span": res.Builder.Exprs.Get(id).Span,
				"target_node": res.Builder.Exprs.Get(index.Target), "index_node": res.Builder.Exprs.Get(index.Index), "container": container,
				"result": result, "expr_types": res.Sema.ExprTypes, "binding_types": res.Sema.BindingTypes,
				"index_symbols": res.Sema.IndexSymbols, "scopes": res.Symbols.Table.Scopes.Data(), "symbols": res.Symbols.Table.Symbols.Data(), "probe": probe})
			if !present || !typed || res.Sema.ExprTypes[index.Index] != in.Builtins().Int {
				t.Fatal("PRECONDITION: native index lost its concrete container/result or scalar int index")
			}
			if strings.Contains(tc.name, "array") {
				payload, found := in.StructInfo(containerID)
				registered := false
				for _, base := range []types.TypeID{in.ArrayNominalType(), in.ArrayFixedNominalType()} {
					original, valid := in.StructInfo(base)
					registered = registered || found && payload != nil && valid && original != nil && payload.Name == original.Name && payload.Decl == original.Decl
				}
				if !registered || len(payload.TypeArgs) == 0 || payload.TypeArgs[0] != in.Builtins().Int || result.Kind != types.KindReference || result.Elem != payload.TypeArgs[0] || result.Mutable {
					t.Fatal("PRECONDITION: full stdlib index is not the registered array element borrow")
				}
			} else if container.Kind != types.KindString || result.Kind != types.KindUint || result.Width != 32 {
				t.Fatal("PRECONDITION: full stdlib string index is not the scalar uint32 operation")
			}
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "analysis": analysis, "error": errorReturnOriginCallText(err), "original_diagnostics": res.Bag.Items()})
			if err != nil || analysis == nil {
				t.Fatalf("native index analysis could not traverse original input: %v", err)
			}
			summary := requireReturnOriginSummary(t, analysis, "probe")
			if summary.BodyKey != probe.BodyKey || summary.Source != probe.Source || summary.NoNormalReturn {
				t.Fatalf("native index lost its original reachable probe result: %+v", summary)
			}
			if tc.name != "owned_array_escape" && (summary.Unknown || !slices.Equal(summary.ParamSlots, tc.slots)) {
				t.Errorf("native index or child effects lost actual sources: %+v want=%v", summary, tc.slots)
			}
			for _, pending := range analysis.Pending {
				if !seen[pending.SourceKey] {
					t.Fatalf("PRECONDITION: obligation lost its original owning unit: %+v", pending)
				}
				if tc.owner == "" && pending.SourceKey == rootKey {
					t.Errorf("native index component still unproved: %+v", pending)
				}
			}
			if tc.owner == "" {
				if len(analysis.Diagnostics) != 0 {
					t.Fatalf("valid native index acquired a diagnostic: %+v", analysis.Diagnostics)
				}
				return
			}
			var owner source.Span
			for _, symbol := range res.Symbols.Table.Symbols.Data() {
				name, _ := res.Symbols.Table.Strings.Lookup(symbol.Name)
				if name == tc.owner && symbol.Span.File == res.File.ID {
					if !owner.Empty() {
						t.Fatal("PRECONDITION: original escape owner is ambiguous")
					}
					owner = symbol.Span
				}
			}
			start := strings.Index(tc.src, tc.escape)
			if owner.Empty() || start < 0 {
				t.Fatal("PRECONDITION: frozen escape owner or statement is missing")
			}
			primary := source.Span{File: res.File.ID, Start: uint32(start), End: uint32(start + len(tc.escape))}
			found := false
			for _, d := range analysis.Diagnostics {
				if d.Code == diag.SemaBorrowEscapesReturn && d.Severity == diag.SevError && d.Primary == primary && len(d.Help) > 0 &&
					d.Message == "borrow of '"+tc.owner+"' outlives its owner when this scope exits" &&
					slices.ContainsFunc(d.Notes, func(n diag.Note) bool {
						return n.Span == owner && n.Msg == "'"+tc.owner+"' owns storage that ends in this scope"
					}) {
					found = true
				}
			}
			if !found {
				t.Errorf("native index erased exact %s owner escape at %v: %+v", tc.owner, primary, analysis.Diagnostics)
			}
		})
	}
}
