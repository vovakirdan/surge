package driver

import (
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/symbols"
)

// Imported synthetic symbols carry selection, while the existing publication
// maps that exact selection back to the physical original declaration.
func checkReturnOriginSelectedIndex(t *testing.T, res *DiagnoseResult, units []sema.ReturnOriginUnit, id ast.ExprID, caller sema.CallableCandidate) {
	t.Helper()
	selected := res.Sema.IndexSymbols[id]
	sym := res.Symbols.Table.Symbols.Get(selected)
	var candidate *sema.CallableCandidate
	for i := range res.Sema.CallableCandidates {
		c := &res.Sema.CallableCandidates[i]
		if c.Symbol == selected {
			if candidate != nil {
				t.Fatal("PRECONDITION: duplicate exact selected index candidate")
			}
			candidate = c
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"selected_index_id": selected, "selected_index_candidate": candidate, "selected_symbol": sym})
	if candidate == nil || sym == nil || sym.Kind != symbols.SymbolFunction || sym.Signature == nil ||
		!candidate.Builtin || !candidate.Intrinsic || candidate.HasBody || !candidate.HasSelf || candidate.SourceKey != "builtin" ||
		candidate.ModulePath != "core/intrinsics" || candidate.Name != "__index" || candidate.Source != sym.Span ||
		candidate.BodyKey == "" || len(candidate.ParamTypes) != 2 || !candidate.ReturnSources.Equal(sym.Signature.ReturnSourceSyntax.Sources()) {
		t.Fatal("PRECONDITION: index lacks its exact canonical primitive declaration")
	}
	in := res.Sema.TypeInterner
	selectedInfo, ok := in.FnInfo(sym.Type)
	if !ok || selectedInfo == nil || !slices.Equal(selectedInfo.Params, candidate.ParamTypes) || selectedInfo.Result != candidate.ResultType {
		t.Fatal("PRECONDITION: synthetic selected index differs from its original typed candidate")
	}
	owners := 0
	for _, unit := range units {
		file := unit.Builder.Files.Get(unit.FileID)
		if file.Span.File != candidate.Source.File {
			continue
		}
		if unit.SourceKey != "core/intrinsics.sg" {
			t.Fatal("PRECONDITION: primitive physical owner is not the retained core file")
		}
		for _, identity := range unit.Publication.LocalCallables {
			if identity.BodyKey != candidate.BodyKey || identity.SourceKey != candidate.SourceKey ||
				!slices.Contains(unit.Publication.LocalSymbols(selected), identity.Symbol) {
				continue
			}
			original := unit.Symbols.Table.Symbols.Get(identity.Symbol)
			if original == nil || original.Decl.ASTFile != unit.FileID || original.Decl.SourceFile != file.Span.File ||
				original.Span != candidate.Source || original.Flags&symbols.SymbolFlagBuiltin == 0 {
				t.Fatal("PRECONDITION: mapped index lost its physical original symbol")
			}
			var fn *ast.FnItem
			for memberID, symbol := range unit.Symbols.ExternSyms {
				if symbol != identity.Symbol {
					continue
				}
				member := unit.Builder.Items.ExternMember(memberID)
				if member == nil || member.Kind != ast.ExternMemberFn || fn != nil {
					t.Fatal("PRECONDITION: mapped index has no unique original extern member")
				}
				fn = unit.Builder.Items.FnByPayload(member.Fn)
			}
			info, typed := in.FnInfo(original.Type)
			if fn == nil || fn.Body.IsValid() || fn.NameSpan != candidate.Source || fn.FnKeywordSpan != candidate.DeclKeyword ||
				!typed || info == nil || !slices.Equal(info.Params, candidate.ParamTypes) || info.Result != candidate.ResultType {
				t.Fatal("PRECONDITION: original extern index AST/type disagrees with canonical selection")
			}
			syntax := symbols.FunctionReturnSourceSyntax(unit.Builder, fn)
			if len(syntax.Params()) != 2 || !syntax.Sources().Equal(candidate.ReturnSources) || !info.ReturnSources().Equal(candidate.ReturnSources) {
				t.Fatal("PRECONDITION: primitive index lost its original return-source promise")
			}
			logReturnOriginCallEvidence(t, map[string]any{"original_index_owner": unit.SourceKey, "original_symbol": original,
				"identity": identity, "original_function": fn, "original_function_type": info,
				"syntax_span": syntax.Span(), "syntax_params": syntax.Params(), "syntax_result": syntax.Result(), "markers": syntax.Markers()})
			owners++
		}
	}
	if owners != 1 {
		t.Fatalf("PRECONDITION: expected one physical selected index owner, got %d", owners)
	}
	closure := res.Sema.InstantiationClosure
	if closure == nil || res.Sema.InstantiationIdentity == nil {
		t.Fatal("PRECONDITION: actual index closure is missing")
	}
	if len(candidate.TemplateParams) == 0 {
		if len(closure.UseSites) != 0 || len(closure.Instances) != 0 || candidate.ResultType != in.Builtins().Uint32 {
			t.Fatal("PRECONDITION: scalar string index acquired an unrelated generic use")
		}
		return
	}
	if len(closure.UseSites) != 1 || len(closure.Instances) != 1 || !slices.Equal(candidate.ReturnSources.Slots(), []uint32{0}) {
		t.Fatal("PRECONDITION: array index lacks its one actual generic instance/use or owner promise")
	}
	use, instance := closure.UseSites[0], closure.Instances[0]
	key, err := sema.NewInstanceKey(*res.Sema.InstantiationIdentity, selected, use.TemplateArgs)
	if err != nil || use.Kind != sema.InstantiationFunction || instance.Kind != sema.InstantiationFunction ||
		use.CalleeTemplate != selected || instance.Template != selected || use.Callee != key || instance.Key != key ||
		!slices.Equal(use.TemplateArgs, instance.TemplateArgs) || len(use.TemplateArgs) != len(candidate.TemplateParams) ||
		use.Caller != (sema.InstanceKey{}) || use.CallerTemplate != caller.Symbol || len(use.CallerTemplateArgs) != 0 ||
		use.SourceKey != caller.SourceKey || use.Site != res.Builder.Exprs.Get(id).Span {
		t.Fatal("PRECONDITION: concrete selected index use differs from its source/caller/callee instance")
	}
	roots := 0
	for _, root := range res.Sema.InstantiationGraph.Roots() {
		if root.Kind == use.Kind && root.Template == selected && root.Witness.Caller == caller.Symbol && root.Witness.Site == use.Site &&
			root.Witness.SourceKey == use.SourceKey && slices.Equal(root.TemplateArgs, use.TemplateArgs) {
			roots++
		}
	}
	if roots != 1 {
		t.Fatal("PRECONDITION: concrete selected index lost its unique producing original root")
	}
}
