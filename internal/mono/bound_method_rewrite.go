package mono

import (
	"strings"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (b *monoBuilder) rewriteBoundMethodCall(call *hir.Expr, data *hir.CallData) bool {
	if call == nil || data == nil || b == nil || b.mod == nil || b.mod.Symbols == nil || b.mod.Symbols.Table == nil || b.mod.Symbols.Table.Symbols == nil {
		return false
	}
	if data.SymbolID.IsValid() {
		return false
	}
	if data.Callee == nil || data.Callee.Kind != hir.ExprFieldAccess {
		return false
	}
	fa, ok := data.Callee.Data.(hir.FieldAccessData)
	if !ok || fa.Object == nil || fa.FieldName == "" {
		return false
	}
	if data.Callee.Type != types.NoTypeID {
		// Likely a real field access of a function value.
		return false
	}
	recv := fa.Object
	if recv.Type == types.NoTypeID || b.isGenericParamType(recv.Type) {
		return false
	}
	argTypes := make([]types.TypeID, len(data.Args))
	for i, arg := range data.Args {
		if arg != nil {
			argTypes[i] = arg.Type
		}
	}
	symID, sig := b.resolveMethodSymbol(recv.Type, fa.FieldName, argTypes)
	if !symID.IsValid() {
		return false
	}

	recvAdj := recv
	if sig != nil && len(sig.Params) > 0 {
		recvAdj = b.adjustExprForParam(recv, sig.Params[0])
	}
	newArgs := make([]*hir.Expr, 0, len(data.Args)+1)
	newArgs = append(newArgs, recvAdj)
	for i, arg := range data.Args {
		if sig == nil || i+1 >= len(sig.Params) {
			newArgs = append(newArgs, arg)
			continue
		}
		newArgs = append(newArgs, b.adjustExprForParam(arg, sig.Params[i+1]))
	}

	data.Args = newArgs
	data.SymbolID = symID
	name := symbolName(b.mod.Symbols, b.mod.Symbols.Table.Strings, symID)
	data.Callee = &hir.Expr{
		Kind: hir.ExprVarRef,
		Type: types.NoTypeID,
		Span: call.Span,
		Data: hir.VarRefData{
			Name:     name,
			SymbolID: symID,
		},
	}
	return true
}

func (b *monoBuilder) resolveMethodSymbol(recvType types.TypeID, name string, argTypes []types.TypeID) (symbols.SymbolID, *symbols.FunctionSignature) {
	if b == nil || b.mod == nil || b.mod.Symbols == nil || b.mod.Symbols.Table == nil || b.mod.Symbols.Table.Symbols == nil || b.mod.Symbols.Table.Strings == nil {
		return symbols.NoSymbolID, nil
	}
	if name == "" || recvType == types.NoTypeID {
		return symbols.NoSymbolID, nil
	}
	recvKey := symbols.TypeKey(b.typeKeyForType(recvType))
	if recvKey == "" {
		return symbols.NoSymbolID, nil
	}
	recvKey = symbols.TypeKey(monoCanonicalTypeKey(recvKey))
	strippedRecv := symbols.TypeKey(monoStripTypeKeyWrappers(recvKey))
	typeBase, typeArgs := parseTypeKeyShape(normalizeTypeKey(string(strippedRecv)))

	var bestSym symbols.SymbolID
	var bestSig *symbols.FunctionSignature

	syms := b.mod.Symbols.Table.Symbols
	limit := syms.Len()
	for i := 1; i <= limit; i++ {
		symID, err := safecast.Conv[symbols.SymbolID](i)
		if err != nil {
			break
		}
		entry := syms.Get(symID)
		if entry == nil || entry.Kind != symbols.SymbolFunction || entry.ReceiverKey == "" || entry.Signature == nil {
			continue
		}
		symName, ok := b.mod.Symbols.Table.Strings.Lookup(entry.Name)
		if !ok || symName != name {
			continue
		}
		receiverKey := symbols.TypeKey(monoCanonicalTypeKey(entry.ReceiverKey))
		if !monoReceiverKeyMatches(receiverKey, recvKey) {
			// Allow match against same base with same arity.
			if typeBase != "" {
				base, args := parseTypeKeyShape(normalizeTypeKey(string(monoStripTypeKeyWrappers(receiverKey))))
				if base != typeBase || args != typeArgs {
					continue
				}
			} else {
				continue
			}
		}
		sig := entry.Signature
		if sig != nil && sig.HasSelf && len(sig.Params) > 0 {
			if !monoTypeKeyCompatible(sig.Params[0], recvKey) {
				continue
			}
			paramCount := len(sig.Params) - 1
			if len(argTypes) > paramCount {
				continue
			}
			// Ensure defaults for omitted args.
			for i := len(argTypes) + 1; i < len(sig.Params); i++ {
				if i >= len(sig.Defaults) || !sig.Defaults[i] {
					paramCount = -1
					break
				}
			}
			if paramCount < 0 {
				continue
			}

			matched := true
			for i, argType := range argTypes {
				if argType == types.NoTypeID {
					continue
				}
				actualKey := symbols.TypeKey(b.typeKeyForType(argType))
				if actualKey == "" {
					matched = false
					break
				}
				if !monoTypeKeyCompatible(sig.Params[i+1], actualKey) {
					matched = false
					break
				}
			}
			if !matched {
				continue
			}
		}
		bestSym = symID
		bestSig = sig
		break
	}

	return bestSym, bestSig
}

func (b *monoBuilder) adjustExprForParam(expr *hir.Expr, expected symbols.TypeKey) *hir.Expr {
	if expr == nil {
		return nil
	}
	expectedStr := strings.TrimSpace(string(expected))
	switch {
	case strings.HasPrefix(expectedStr, "&mut "):
		return b.ensureBorrow(expr, true)
	case strings.HasPrefix(expectedStr, "&"):
		return b.ensureBorrow(expr, false)
	default:
		return b.ensureValue(expr)
	}
}

func (b *monoBuilder) ensureBorrow(expr *hir.Expr, mut bool) *hir.Expr {
	if expr == nil {
		return nil
	}
	if _, ok, recvMut := b.referenceInfo(expr.Type); ok {
		if mut && !recvMut {
			return expr
		}
		if isBorrowExpr(expr) {
			return expr
		}
		if expr.Kind == hir.ExprVarRef {
			return b.borrowExpr(expr, mut)
		}
		return expr
	}
	return b.borrowExpr(expr, mut)
}

func (b *monoBuilder) ensureValue(expr *hir.Expr) *hir.Expr {
	if expr == nil {
		return nil
	}
	elem, ok, _ := b.referenceInfo(expr.Type)
	if !ok {
		return expr
	}
	return &hir.Expr{
		Kind: hir.ExprUnaryOp,
		Type: elem,
		Span: expr.Span,
		Data: hir.UnaryOpData{
			Op:      ast.ExprUnaryDeref,
			Operand: expr,
		},
	}
}

func (b *monoBuilder) borrowExpr(expr *hir.Expr, mut bool) *hir.Expr {
	if expr == nil {
		return nil
	}
	if b.types == nil {
		return expr
	}
	refType := b.types.Intern(types.MakeReference(expr.Type, mut))
	op := ast.ExprUnaryRef
	if mut {
		op = ast.ExprUnaryRefMut
	}
	return &hir.Expr{
		Kind: hir.ExprUnaryOp,
		Type: refType,
		Span: expr.Span,
		Data: hir.UnaryOpData{
			Op:      op,
			Operand: expr,
		},
	}
}

func (b *monoBuilder) referenceInfo(id types.TypeID) (elem types.TypeID, ok, mut bool) {
	if id == types.NoTypeID || b == nil || b.types == nil {
		return types.NoTypeID, false, false
	}
	tt, found := b.types.Lookup(id)
	if !found || tt.Kind != types.KindReference {
		return types.NoTypeID, false, false
	}
	return tt.Elem, true, tt.Mutable
}

func isBorrowExpr(e *hir.Expr) bool {
	if e == nil || e.Kind != hir.ExprUnaryOp {
		return false
	}
	data, ok := e.Data.(hir.UnaryOpData)
	if !ok {
		return false
	}
	return data.Op == ast.ExprUnaryRef || data.Op == ast.ExprUnaryRefMut
}

func (b *monoBuilder) isGenericParamType(id types.TypeID) bool {
	if b == nil || b.types == nil || id == types.NoTypeID {
		return false
	}
	resolved := resolveAlias(b.types, id)
	if tt, ok := b.types.Lookup(resolved); ok {
		switch tt.Kind {
		case types.KindReference, types.KindOwn, types.KindPointer:
			return b.isGenericParamType(tt.Elem)
		case types.KindGenericParam:
			return true
		}
	}
	return false
}

func monoTypeKeyCompatible(expected, actual symbols.TypeKey) bool {
	if monoTypeKeyMatchesWithGenerics(expected, actual) {
		return true
	}
	expStr := monoStripTypeKeyWrappers(expected)
	actStr := monoStripTypeKeyWrappers(actual)
	return monoTypeKeyMatchesWithGenerics(symbols.TypeKey(expStr), symbols.TypeKey(actStr))
}

func monoReceiverKeyMatches(expected, actual symbols.TypeKey) bool {
	if monoTypeKeyMatchesWithGenerics(expected, actual) {
		return true
	}
	expStr := monoStripTypeKeyWrappers(expected)
	actStr := monoStripTypeKeyWrappers(actual)
	return monoTypeKeyMatchesWithGenerics(symbols.TypeKey(expStr), symbols.TypeKey(actStr))
}

func monoTypeKeyMatchesWithGenerics(a, b symbols.TypeKey) bool {
	if monoTypeKeyEqual(a, b) {
		return true
	}
	return monoGenericTypeKeyCompatible(a, b) || monoGenericTypeKeyCompatible(b, a)
}

func monoTypeKeyEqual(a, b symbols.TypeKey) bool {
	return monoCanonicalTypeKey(a) == monoCanonicalTypeKey(b)
}

func monoCanonicalTypeKey(key symbols.TypeKey) string {
	if key == "" {
		return ""
	}
	s := strings.TrimSpace(string(key))
	prefix := ""
	switch {
	case strings.HasPrefix(s, "&mut "):
		prefix = "&mut "
		s = strings.TrimSpace(strings.TrimPrefix(s, "&mut "))
	case strings.HasPrefix(s, "&"):
		prefix = "&"
		s = strings.TrimSpace(strings.TrimPrefix(s, "&"))
	case strings.HasPrefix(s, "own "):
		prefix = "own "
		s = strings.TrimSpace(strings.TrimPrefix(s, "own "))
	case strings.HasPrefix(s, "*"):
		prefix = "*"
		s = strings.TrimSpace(strings.TrimPrefix(s, "*"))
	}
	if _, _, hasLen, ok := monoParseArrayKey(s); ok {
		if hasLen {
			return prefix + "[;]"
		}
		return prefix + "[]"
	}
	return prefix + s
}

func monoGenericTypeKeyCompatible(genericKey, concreteKey symbols.TypeKey) bool {
	genericStr := monoStripTypeKeyWrappers(genericKey)
	concreteStr := monoStripTypeKeyWrappers(concreteKey)
	baseGen, genArgs, okGen := monoParseGenericTypeKey(genericStr)
	if !okGen {
		return false
	}
	baseCon, conArgs, okCon := monoParseGenericTypeKey(concreteStr)
	if !okCon {
		return false
	}
	if baseGen != baseCon || len(genArgs) != len(conArgs) {
		return false
	}
	return true
}

func monoStripTypeKeyWrappers(key symbols.TypeKey) string {
	s := strings.TrimSpace(string(key))
	for {
		switch {
		case strings.HasPrefix(s, "&mut "):
			s = strings.TrimSpace(strings.TrimPrefix(s, "&mut "))
		case strings.HasPrefix(s, "&"):
			s = strings.TrimSpace(strings.TrimPrefix(s, "&"))
		case strings.HasPrefix(s, "own "):
			s = strings.TrimSpace(strings.TrimPrefix(s, "own "))
		case strings.HasPrefix(s, "*"):
			s = strings.TrimSpace(strings.TrimPrefix(s, "*"))
		default:
			return s
		}
	}
}

func monoParseGenericTypeKey(raw string) (base string, args []string, ok bool) {
	s := strings.TrimSpace(raw)
	start := strings.Index(s, "<")
	end := strings.LastIndex(s, ">")
	if start < 0 || end <= start {
		return "", nil, false
	}
	base = strings.TrimSpace(s[:start])
	if base == "" {
		return "", nil, false
	}
	args = monoSplitTypeArgs(s[start+1 : end])
	if len(args) == 0 {
		return "", nil, false
	}
	return base, args, true
}

func monoSplitTypeArgs(s string) []string {
	var out []string
	var buf strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '<', '(', '[', '{':
			depth++
		case '>', ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(buf.String())
				if part != "" {
					out = append(out, part)
				}
				buf.Reset()
				continue
			}
		}
		buf.WriteRune(r)
	}
	if tail := strings.TrimSpace(buf.String()); tail != "" {
		out = append(out, tail)
	}
	return out
}

func monoParseArrayKey(raw string) (elem, lengthKey string, hasLen, ok bool) {
	s := strings.TrimSpace(raw)
	if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
		content := strings.TrimSpace(s[1 : len(s)-1])
		if parts := strings.Split(content, ";"); len(parts) == 2 {
			elem = strings.TrimSpace(parts[0])
			lengthKey = strings.TrimSpace(parts[1])
			hasLen = true
			return elem, lengthKey, hasLen, true
		}
		return content, "", false, true
	}
	if strings.HasPrefix(s, "Array<") && strings.HasSuffix(s, ">") {
		content := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "Array<"), ">"))
		return content, "", false, true
	}
	return "", "", false, false
}
