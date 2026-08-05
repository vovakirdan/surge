package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) validateEntrypointArgvParams(
	fn *ast.FnItem,
	entrypoint symbols.SymbolID,
	sym *symbols.Symbol,
	scope symbols.ScopeID,
) {
	if sym == nil || sym.Signature == nil {
		return
	}
	for i, id := range tc.builder.Items.GetFnParamIDs(fn) {
		param := tc.builder.Items.FnParam(id)
		if param == nil {
			continue
		}
		tc.recordEntrypointParamRequest(entrypoint, uint32(i), param, scope, EntrypointParamFromArgv) //nolint:gosec -- AST parameter count is bounded
	}
}

func (tc *typeChecker) validateEntrypointStdinParam(
	fn *ast.FnItem,
	entrypoint symbols.SymbolID,
	sym *symbols.Symbol,
	scope symbols.ScopeID,
) {
	if sym == nil || sym.Signature == nil {
		return
	}
	ids := tc.builder.Items.GetFnParamIDs(fn)
	if len(ids) != 1 {
		span := fn.ParamsSpan
		if span == (source.Span{}) {
			span = fn.Span
		}
		b := diag.ReportError(tc.reporter, diag.SemaEntrypointStdinArity, span,
			fmt.Sprintf("@entrypoint(\"stdin\") requires exactly one parameter; found %d", len(ids)))
		b.WithNote(span, "stdin is delivered as one complete owned string; EOF supplies an empty string")
		b.WithNote(span, "help: declare exactly one parameter implementing FromStdin<T>")
		b.Emit()
		return
	}

	param := tc.builder.Items.FnParam(ids[0])
	if param == nil {
		return
	}
	if param.Default.IsValid() {
		tc.reportStdinDefault(param)
		return
	}
	tc.recordEntrypointParamRequest(entrypoint, 0, param, scope, EntrypointParamFromStdin)
}

func (tc *typeChecker) recordEntrypointParamRequest(
	entrypoint symbols.SymbolID,
	index uint32,
	param *ast.FnParam,
	scope symbols.ScopeID,
	role EntrypointCallableRole,
) {
	paramType := tc.resolveTypeExprWithScope(param.Type, scope)
	if paramType == types.NoTypeID {
		return
	}
	errType := tc.resolveErrorType(param.Span, scope)
	expected := tc.resolveResultType(paramType, errType, param.Span, scope)
	method := "from_str"
	args := []types.TypeID{tc.types.Intern(types.MakeReference(tc.types.Builtins().String, false))}
	if role == EntrypointParamFromStdin {
		method = "from_stdin"
		args[0] = tc.types.Builtins().String
	}
	tc.recordEntrypointCallableRequest(EntrypointCallableRequest{
		Entrypoint:     entrypoint,
		Role:           role,
		ParamIndex:     index,
		ParamName:      tc.lookupName(param.Name),
		TypeLabel:      tc.typeLabel(paramType),
		CanDefineHere:  tc.canDefineEntrypointParserHere(paramType),
		Receiver:       paramType,
		Args:           args,
		ExpectedResult: expected,
		Method:         method,
		AccessModule:   tc.modulePath,
		Site:           param.Span,
	})
}

func (tc *typeChecker) canDefineEntrypointParserHere(typeID types.TypeID) bool {
	if tc == nil || tc.builder == nil || tc.typeIDItems == nil || typeID == types.NoTypeID {
		return false
	}
	itemID := tc.typeIDItems[typeID]
	if !itemID.IsValid() {
		return false
	}
	typeItem, ok := tc.builder.Items.Type(itemID)
	if !ok || typeItem == nil {
		return false
	}
	switch typeItem.Kind {
	case ast.TypeDeclStruct, ast.TypeDeclUnion, ast.TypeDeclEnum:
		return true
	default:
		return false
	}
}

func (tc *typeChecker) reportStdinDefault(param *ast.FnParam) {
	primary := param.Span
	deleteSpan, ok := tc.stdinDefaultSpan(param)
	if ok {
		primary = deleteSpan
	}
	b := diag.ReportError(tc.reporter, diag.SemaEntrypointStdinDefault, primary,
		"@entrypoint(\"stdin\") parameter cannot have a default value")
	b.WithNote(param.Span, "stdin always supplies one owned string; EOF supplies the empty string \"\"")
	b.WithNote(param.Span, "help: remove the parameter initializer")
	if ok {
		b.WithFixSuggestion(stdinDefaultRemovalFix(deleteSpan))
	}
	b.Emit()
}

func (tc *typeChecker) stdinDefaultSpan(param *ast.FnParam) (source.Span, bool) {
	if tc == nil || tc.builder == nil || param == nil || !param.Default.IsValid() {
		return source.Span{}, false
	}
	typeExpr := tc.builder.Types.Get(param.Type)
	defaultExpr := tc.builder.Exprs.Get(param.Default)
	if typeExpr == nil || defaultExpr == nil || typeExpr.Span.File != defaultExpr.Span.File || typeExpr.Span.End > defaultExpr.Span.End {
		return source.Span{}, false
	}
	return source.Span{File: typeExpr.Span.File, Start: typeExpr.Span.End, End: defaultExpr.Span.End}, true
}

type stdinDefaultFixThunk struct{ span source.Span }

func stdinDefaultRemovalFix(span source.Span) *diag.Fix {
	return &diag.Fix{
		ID:            "entrypoint.remove-stdin-default",
		Title:         "remove stdin parameter default",
		Kind:          diag.FixKindQuickFix,
		Applicability: diag.FixApplicabilityManualReview,
		Thunk:         stdinDefaultFixThunk{span: span},
	}
}

// ID identifies the guarded stdin-default removal fix.
func (f stdinDefaultFixThunk) ID() string { return "entrypoint.remove-stdin-default" }

// Build captures the current initializer text so applying the fix is guarded.
func (f stdinDefaultFixThunk) Build(ctx diag.FixBuildContext) (diag.Fix, error) {
	if ctx.FileSet == nil || !ctx.FileSet.HasFile(f.span.File) {
		return diag.Fix{}, fmt.Errorf("stdin default fix: source file is unavailable")
	}
	file := ctx.FileSet.Get(f.span.File)
	if file == nil || f.span.Start > f.span.End || uint64(f.span.End) > uint64(len(file.Content)) {
		return diag.Fix{}, fmt.Errorf("stdin default fix: invalid source range")
	}
	guard := string(file.Content[f.span.Start:f.span.End])
	return diag.Fix{
		ID:            f.ID(),
		Title:         "remove stdin parameter default",
		Kind:          diag.FixKindQuickFix,
		Applicability: diag.FixApplicabilityManualReview,
		Edits:         []diag.TextEdit{{Span: f.span, OldText: guard}},
	}, nil
}
