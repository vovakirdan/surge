package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type externMethodHeader struct {
	result      types.TypeID
	fnType      types.TypeID
	bindings    []types.TypeID
	scope       symbols.ScopeID
	env         uint32
	diagnostics []*diag.Diagnostic
}

type externHeaderReporter struct{ diagnostics []*diag.Diagnostic }

func (r *externHeaderReporter) Report(code diag.Code, severity diag.Severity, span source.Span, message string, notes []diag.Note, fixes []*diag.Fix) {
	r.ReportDiagnostic(&diag.Diagnostic{Code: code, Severity: severity, Primary: span, Message: message, Notes: notes, Fixes: fixes})
}

func (r *externHeaderReporter) ReportDiagnostic(d *diag.Diagnostic) {
	r.diagnostics = append(r.diagnostics, d)
}

// resolveExternMethodHeader is the callable descriptor authority for both the
// early bodyless pass and the ordinary extern walk. Only the latter recovers.
func (tc *typeChecker) resolveExternMethodHeader(fn *ast.FnItem, scope symbols.ScopeID, owner symbols.SymbolID, early bool) *externMethodHeader {
	header := &externMethodHeader{result: tc.types.Builtins().Nothing, scope: scope, env: tc.currentTypeParamEnv()}
	checkpoint := tc.errorCheckpoint()
	if fn.ReturnType.IsValid() {
		header.result = tc.resolveTypeExprWithScopeAllowPointer(fn.ReturnType, scope, true)
		if header.result == types.NoTypeID && !early {
			header.result = tc.types.Builtins().Nothing
		}
	}
	paramIDs := tc.builder.Items.GetFnParamIDs(fn)
	params := make([]types.TypeID, 0, len(paramIDs))
	for _, pid := range paramIDs {
		param := tc.builder.Items.FnParam(pid)
		if param == nil {
			continue
		}
		typ := tc.resolveTypeExprWithScopeAllowPointer(param.Type, scope, true)
		if typ == types.NoTypeID {
			return header
		}
		params = append(params, typ)
	}
	if !owner.IsValid() || (early && (header.result == types.NoTypeID || tc.hasErrorsSince(checkpoint))) {
		return header
	}
	result := header.result
	if fn.Flags&ast.FnModifierAsync != 0 {
		span := fn.ReturnSpan
		if span == (source.Span{}) {
			span = fn.Span
		}
		result = tc.taskType(result, span)
	}
	header.fnType = tc.registerDeclaredFnType(fn, params, result, scope, owner)
	if !early || !tc.hasErrorsSince(checkpoint) {
		tc.assignSymbolType(owner, header.fnType)
	}
	return header
}

func (tc *typeChecker) prepareExternMethodHeaders() {
	if tc.symbols == nil || tc.symbols.Table == nil || tc.types == nil {
		return
	}
	tc.externMethodHeaders = make(map[symbols.SymbolID]*externMethodHeader)
	for i := range tc.symbols.Table.Symbols.Data() {
		id := symbols.SymbolID(i + 1)
		original := *tc.symbolFromID(id)
		fn, block := tc.earlyExternMethod(&original)
		if fn == nil {
			continue
		}
		scope := original.Scope
		for i, candidate := range tc.symbols.Table.Scopes.Data() {
			if candidate.Owner.ASTFile == original.Decl.ASTFile && candidate.Owner.Extern == ast.ExternMemberID(original.Decl.Expr) {
				scope = symbols.ScopeID(i + 1)
				break
			}
		}
		owner := tc.externTargetSymbol(block.Target, original.Scope)
		specs := tc.externTypeParamSpecs(block.Target, original.Scope)
		bindings, valid := tc.externHeaderBindings(&original, owner, specs)
		if !valid || !tc.pushTypeParams(owner, specs, bindings) {
			continue
		}
		bindings = slices.Clone(tc.typeParamStack[len(tc.typeParamStack)-len(specs):])
		for i, binding := range bindings {
			tc.typeParamNames[binding] = specs[i].name
		}
		tc.applyTypeParamBounds(owner)
		// Failed preparation must leave diagnostics and requests to the original
		// owning walk. Its fresh environment cannot reuse failed type-cache keys.
		reporter, errors := tc.reporter, tc.errorCount
		buffer := &externHeaderReporter{}
		tc.reporter = &diagnosticCountingReporter{inner: buffer, errorCount: &tc.errorCount}
		declarations, instantiations := len(tc.result.ReturnSourceDeclarations), len(tc.result.ReturnSourceInstantiations)
		header := tc.resolveExternMethodHeader(fn, scope, id, true)
		failed := tc.hasErrorsSince(errors)
		tc.reporter, tc.errorCount = reporter, errors
		if header.fnType != types.NoTypeID && !failed {
			header.bindings = bindings
			header.diagnostics = buffer.diagnostics
			tc.externMethodHeaders[id] = header
		} else {
			tc.result.ReturnSourceDeclarations = tc.result.ReturnSourceDeclarations[:declarations]
			tc.result.ReturnSourceInstantiations = tc.result.ReturnSourceInstantiations[:instantiations]
		}
		tc.popTypeParams()
	}
}

// Eligibility is structural and source-owned. Unused method generics cannot be
// recovered from self and retain their existing ordinary declaration lifecycle.
func (tc *typeChecker) earlyExternMethod(sym *symbols.Symbol) (*ast.FnItem, *ast.ExternBlock) {
	if sym.Kind != symbols.SymbolFunction || sym.Signature == nil || !sym.Signature.HasSelf || !sym.Decl.ASTFile.IsValid() || !sym.Decl.Expr.IsValid() {
		return nil, nil
	}
	if sym.Decl.ASTFile != tc.fileID {
		if _, ok := tc.symbols.ModuleFiles[sym.Decl.ASTFile]; !ok {
			return nil, nil
		}
	}
	if tc.symbols.Table.Scopes.Get(sym.Scope) == nil {
		return nil, nil
	}
	block, ok := tc.builder.Items.Extern(sym.Decl.Item)
	if !ok || block == nil || uint32(sym.Decl.Expr) < uint32(block.MembersStart) || uint32(sym.Decl.Expr)-uint32(block.MembersStart) >= block.MembersCount {
		return nil, nil
	}
	if _, sealed := tc.externSealedBlocks[sym.Decl.Item]; sealed {
		return nil, nil
	}
	member := tc.builder.Items.ExternMember(ast.ExternMemberID(sym.Decl.Expr))
	if member == nil || member.Kind != ast.ExternMemberFn {
		return nil, nil
	}
	fn := tc.builder.Items.FnByPayload(member.Fn)
	if fn == nil || fn.Name != sym.Name || fn.Span.File != sym.Decl.SourceFile || fn.Body.IsValid() || fn.Flags&ast.FnModifierAsync != 0 || len(fn.Generics) != 0 || fn.TypeParamsCount != 0 {
		return nil, nil
	}
	path, ok := tc.builder.Types.Path(block.Target)
	if !ok || path == nil || len(path.Segments) != 1 || len(path.Segments[0].Generics) == 0 {
		return nil, nil
	}
	specs := tc.externTypeParamSpecs(block.Target, sym.Scope)
	args := path.Segments[0].Generics
	if len(specs) != len(args) || len(sym.TypeParams) != len(args) {
		return nil, nil
	}
	for i, arg := range args {
		param, ok := tc.builder.Types.Path(arg)
		if !ok || param == nil || len(param.Segments) != 1 || len(param.Segments[0].Generics) != 0 || specs[i].kind != paramKindType || param.Segments[0].Name != specs[i].name {
			return nil, nil
		}
	}
	params := tc.builder.Items.GetFnParamIDs(fn)
	if len(params) == 0 {
		return nil, nil
	}
	self := tc.builder.Items.FnParam(params[0])
	if self == nil {
		return nil, nil
	}
	selfType := self.Type
	if unary, ok := tc.builder.Types.UnaryType(selfType); ok && unary != nil {
		if unary.Op != ast.TypeUnaryRef && unary.Op != ast.TypeUnaryRefMut && unary.Op != ast.TypeUnaryOwn {
			return nil, nil
		}
		selfType = unary.Inner
	}
	selfPath, ok := tc.builder.Types.Path(selfType)
	if !ok || selfPath == nil || len(selfPath.Segments) != 1 || selfPath.Segments[0].Name != path.Segments[0].Name || len(selfPath.Segments[0].Generics) != len(args) {
		return nil, nil
	}
	for i, arg := range selfPath.Segments[0].Generics {
		param, ok := tc.builder.Types.Path(arg)
		if !ok || param == nil || len(param.Segments) != 1 || len(param.Segments[0].Generics) != 0 || param.Segments[0].Name != specs[i].name {
			return nil, nil
		}
	}
	owner := tc.symbolFromID(tc.externTargetSymbol(block.Target, sym.Scope))
	if owner == nil {
		return nil, nil
	}
	shape, ok := tc.types.StructInfo(owner.Type)
	if !ok || shape == nil || len(shape.TypeParams) != len(args) {
		return nil, nil
	}
	return fn, block
}

// Existing formal IDs are authority; metadata only validates their original
// owner/order. Never obtain a replacement ID by searching names or owners.
func (tc *typeChecker) externHeaderBindings(sym *symbols.Symbol, owner symbols.SymbolID, specs []genericParamSpec) ([]types.TypeID, bool) {
	if sym.Type == types.NoTypeID {
		return nil, true
	}
	fn, ok := tc.types.FnInfo(sym.Type)
	if !ok || fn == nil || len(fn.Params) == 0 {
		return nil, false
	}
	formal, ok := tc.types.StructInfo(tc.valueType(fn.Params[0]))
	original := tc.symbolFromID(owner)
	if !ok || formal == nil || original == nil || len(formal.TypeArgs) != len(specs) {
		return nil, false
	}
	shape, ok := tc.types.StructInfo(original.Type)
	if !ok || shape == nil || formal.Name != shape.Name || formal.Decl != shape.Decl {
		return nil, false
	}
	for i, binding := range formal.TypeArgs {
		param, ok := tc.types.TypeParamInfo(binding)
		if !ok || param == nil || param.IsConst || param.Owner != uint32(owner) || param.Index != uint32(i) || param.Name != specs[i].name {
			return nil, false
		}
	}
	return formal.TypeArgs, true
}
