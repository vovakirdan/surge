package driver

import (
	"crypto/sha256"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// The indexed expression is a borrow, while its existing return consumer
// reads a scalar Copy value before dropping the array's owning storage.
func TestAnalyzeIndexScalarReturnCopiesValue(t *testing.T) {
	const src = "fn probe(values: Array<uint>, index: int) -> uint { return values[index]; }\n"
	t.Logf("RETURN_ORIGIN_INDEX_SCALAR_SOURCE sha256=%x source=%q", sha256.Sum256([]byte(src)), src)
	res := returnOriginStdlibFixture(t, src, false)
	closureErr := FinalizeInstantiationClosure(t.Context(), res, 64)
	checkReturnOriginStdlibBags(t, res, false)
	inputs, unitsErr := collectReturnOriginUnits(res)
	if closureErr != nil || unitsErr != nil || len(inputs.units) != 11 || string(res.File.Content) != src {
		t.Fatalf("PRECONDITION: original scalar return input: closure=%v units=%v", closureErr, unitsErr)
	}
	for file, bag := range inputs.bags {
		if bag == nil || bag.Len() != 0 {
			t.Fatalf("PRECONDITION: scalar return requires a clean original bag for file %d", file)
		}
	}
	rootKey := ""
	seen := make(map[string]bool)
	for _, unit := range inputs.units {
		file := res.FileSet.Get(unit.Builder.Files.Get(unit.FileID).Span.File)
		if file == nil || seen[unit.SourceKey] {
			t.Fatal("PRECONDITION: missing or repeated original source unit")
		}
		seen[unit.SourceKey] = true
		logReturnOriginCallEvidence(t, map[string]any{"scalar_return_unit": unit.SourceKey,
			"source_sha256": sha256.Sum256(file.Content), "publication": unit.Publication})
		if file.ID == res.File.ID {
			if unit.Builder != res.Builder || unit.Sema != res.Sema || unit.FileID != res.FileID {
				t.Fatal("PRECONDITION: scalar return lost its original owning artifacts")
			}
			rootKey = unit.SourceKey
		} else if !strings.HasPrefix(unit.SourceKey, "core/") {
			t.Fatal("PRECONDITION: unexpected non-core source unit")
		}
	}
	var probe *sema.CallableCandidate
	for i := range res.Sema.CallableCandidates {
		c := &res.Sema.CallableCandidates[i]
		if c.SourceKey == rootKey && c.Source.File == res.File.ID && c.Name == "probe" {
			if probe != nil {
				t.Fatal("PRECONDITION: duplicate original probe candidate")
			}
			probe = c
		}
	}
	in := res.Sema.TypeInterner
	if rootKey == "" || probe == nil || !probe.HasBody || probe.BodyKey == "" || len(probe.ParamTypes) != 2 || probe.ResultType != in.Builtins().Uint {
		t.Fatal("PRECONDITION: original probe does not return scalar uint")
	}
	probeSymbol := res.Symbols.Table.Symbols.Get(probe.Symbol)
	if probeSymbol == nil || probeSymbol.Decl.ASTFile != res.FileID || probeSymbol.Decl.SourceFile != res.File.ID || probeSymbol.Span != probe.Source {
		t.Fatal("PRECONDITION: probe lost its original function declaration")
	}
	probeType, fnTyped := in.FnInfo(probeSymbol.Type)
	logReturnOriginCallEvidence(t, map[string]any{"probe_symbol": probeSymbol, "probe_function_type": probeType})
	if !fnTyped || probeType == nil || probeType.Result != probe.ResultType || !slices.Equal(probeType.Params, probe.ParamTypes) {
		t.Fatal("PRECONDITION: probe's original function type differs from its canonical candidate")
	}
	var expr ast.ExprID
	for raw := uint32(1); raw <= res.Builder.Exprs.Arena.Len(); raw++ {
		id := ast.ExprID(raw)
		if node := res.Builder.Exprs.Get(id); node.Kind == ast.ExprIndex && node.Span.File == res.File.ID {
			if expr.IsValid() {
				t.Fatal("PRECONDITION: scalar source has multiple index expressions")
			}
			expr = id
		}
	}
	index, ok := res.Builder.Exprs.Index(expr)
	if !ok || index == nil || res.Builder.Exprs.Get(expr).Span != (source.Span{File: res.File.ID, Start: 59, End: 72}) {
		t.Fatal("PRECONDITION: exact scalar index expression is missing")
	}
	checkReturnOriginSelectedIndex(t, res, inputs.units, expr, *probe)
	actual, typed := in.Lookup(res.Sema.ExprTypes[expr])
	container, present := in.StructInfo(res.Sema.ExprTypes[index.Target])
	registered, registeredOK := in.StructInfo(in.ArrayNominalType())
	ownerID := res.Symbols.ExprSymbols[index.Target]
	owner := res.Symbols.Table.Symbols.Get(ownerID)
	ownerName := ""
	if owner != nil {
		ownerName, _ = res.Symbols.Table.Strings.Lookup(owner.Name)
	}
	logReturnOriginCallEvidence(t, map[string]any{"scalar_index": expr, "index_ast": index, "actual_type": actual,
		"expected_type": probe.ResultType, "copy": res.Sema.IsCopyType(probe.ResultType), "probe": probe,
		"container": container, "registered_array": registered, "owner_id": ownerID, "owner": owner,
		"return_span": source.Span{File: res.File.ID, Start: 52, End: 73}, "scopes": res.Symbols.Table.Scopes.Data()})
	if !typed || actual.Kind != types.KindReference || actual.Mutable || actual.Elem != probe.ResultType || !res.Sema.IsCopyType(probe.ResultType) ||
		!present || container == nil || !registeredOK || registered == nil || container.Name != registered.Name || container.Decl != registered.Decl ||
		!slices.Equal(container.TypeArgs, []types.TypeID{in.Builtins().Uint}) || res.Sema.ExprTypes[index.Index] != in.Builtins().Int ||
		owner == nil || owner.Kind != symbols.SymbolParam || ownerName != "values" || owner.Decl.ASTFile != res.FileID ||
		owner.Decl.SourceFile != res.File.ID || owner.Type != probe.ParamTypes[0] || owner.Type != res.Sema.ExprTypes[index.Target] {
		t.Fatal("PRECONDITION: scalar read lost its actual borrow, Copy consumer or by-value owner")
	}
	ownerScope := res.Symbols.Table.Scopes.Get(owner.Scope)
	if ownerScope == nil || ownerScope.Kind != symbols.ScopeFunction || ownerScope.Owner.ASTFile != res.FileID ||
		!ownerScope.Owner.Item.IsValid() {
		t.Fatal("PRECONDITION: indexed array parameter has no original function scope")
	}
	fn, found := res.Builder.Items.Fn(ownerScope.Owner.Item)
	if !found || fn == nil || fn.NameSpan != probe.Source || fn.Body.IsValid() != probe.HasBody || ownerScope.Owner.Item != probeSymbol.Decl.Item {
		t.Fatal("PRECONDITION: array storage owner belongs to a different function")
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
	logReturnOriginCallEvidence(t, map[string]any{"scalar_return_analysis": analysis, "error": errorReturnOriginCallText(err), "original_diagnostics": res.Bag.Items()})
	if err != nil || analysis == nil {
		t.Fatalf("scalar return analysis failed to traverse original input: %v", err)
	}
	summary := requireReturnOriginSummary(t, analysis, "probe")
	if summary.BodyKey != probe.BodyKey || summary.Source != probe.Source || summary.NoNormalReturn || summary.Unknown || len(summary.ParamSlots) != 0 {
		t.Errorf("scalar return retained reference origins instead of a copied value: %+v", summary)
	}
	for _, pending := range analysis.Pending {
		if !seen[pending.SourceKey] {
			t.Fatalf("PRECONDITION: obligation lost its original unit: %+v", pending)
		}
		if pending.SourceKey == rootKey {
			t.Errorf("scalar return remains unproved: %+v", pending)
		}
	}
	if len(analysis.Diagnostics) != 0 {
		t.Fatalf("valid scalar Copy return acquired diagnostics: %+v", analysis.Diagnostics)
	}
}
