package sema

import (
	"fmt"
	"strings"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

func canonicalTypeKey(key symbols.TypeKey) symbols.TypeKey {
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
	if _, _, _, hasLen, ok := parseArrayKey(s); ok {
		if hasLen {
			return symbols.TypeKey(prefix + "[;]")
		}
		return symbols.TypeKey(prefix + "[]")
	}
	return symbols.TypeKey(prefix + s)
}

func isArrayKey(key symbols.TypeKey) bool {
	if key == "" {
		return false
	}
	s := strings.TrimSpace(string(key))
	switch {
	case strings.HasPrefix(s, "&mut "):
		s = strings.TrimSpace(strings.TrimPrefix(s, "&mut "))
	case strings.HasPrefix(s, "&"):
		s = strings.TrimSpace(strings.TrimPrefix(s, "&"))
	case strings.HasPrefix(s, "own "):
		s = strings.TrimSpace(strings.TrimPrefix(s, "own "))
	case strings.HasPrefix(s, "*"):
		s = strings.TrimSpace(strings.TrimPrefix(s, "*"))
	}
	_, _, _, _, ok := parseArrayKey(s)
	return ok
}

func typeKeyEqual(a, b symbols.TypeKey) bool {
	return canonicalTypeKey(a) == canonicalTypeKey(b)
}

func normalizeSignatureForReceiver(sig *symbols.FunctionSignature, receiver symbols.TypeKey) *symbols.FunctionSignature {
	if sig == nil || receiver == "" || len(sig.Params) == 0 || !sig.HasSelf {
		return sig
	}
	recv := canonicalTypeKey(receiver)
	if typeKeyEqual(sig.Params[0], recv) {
		return sig
	}
	selfStr := strings.TrimSpace(string(sig.Params[0]))
	prefix := ""
	switch {
	case strings.HasPrefix(selfStr, "&mut "):
		prefix = "&mut "
	case strings.HasPrefix(selfStr, "&"):
		prefix = "&"
	case strings.HasPrefix(selfStr, "own "):
		prefix = "own "
	case strings.HasPrefix(selfStr, "*"):
		prefix = "*"
	}
	if prefix != "" {
		base := strings.TrimSpace(string(recv))
		base = strings.TrimSpace(strings.TrimPrefix(base, "&mut "))
		base = strings.TrimSpace(strings.TrimPrefix(base, "&"))
		base = strings.TrimSpace(strings.TrimPrefix(base, "own "))
		base = strings.TrimSpace(strings.TrimPrefix(base, "*"))
		recv = symbols.TypeKey(prefix + base)
		if typeKeyEqual(sig.Params[0], recv) {
			return sig
		}
	}
	// For methods (user-defined), preserve the actual self parameter type
	// This allows implicit borrow checking in selfParamCompatible()
	// For operators (__add, __sub, etc.), normalize to enforce alias type safety
	// Operators don't use selfParamCompatible, so they need exact type matching
	clone := *sig
	params := make([]symbols.TypeKey, len(sig.Params))
	copy(params, sig.Params)
	params[0] = recv
	clone.Params = params
	return &clone
}

func (tc *typeChecker) buildMagicIndex() {
	tc.magic = make(map[symbols.TypeKey]map[string][]*symbols.FunctionSignature)
	if tc.symbols != nil && tc.symbols.Table != nil && tc.symbols.Table.Symbols != nil {
		if data := tc.symbols.Table.Symbols.Data(); data != nil {
			for i := range data {
				sym := &data[i]
				if sym.Kind != symbols.SymbolFunction || sym.ReceiverKey == "" || sym.Signature == nil {
					continue
				}
				value, err := safecast.Conv[uint32](i + 1)
				if err != nil {
					panic(fmt.Errorf("sema: symbol id overflow: %w", err))
				}
				symID := symbols.SymbolID(value) // Data() skips index 0
				name := tc.symbolName(sym.Name)
				recordID := symID
				if sym.Flags&symbols.SymbolFlagBuiltin != 0 && !isOperatorMagicName(name) {
					if !isMapIndexMagicName(name) || !isMapReceiverKey(sym.ReceiverKey) {
						recordID = symbols.NoSymbolID
					}
				}
				if name == "__to" && !tc.acceptToSignature(sym.Signature, sym.ReceiverKey, sym) {
					continue
				}
				// Only normalize operators to enforce alias type safety.
				// Preserve actual self parameter types for user methods (for implicit borrow checking).
				normalized := sym.Signature
				if isOperatorMagicName(name) {
					normalized = normalizeSignatureForReceiver(sym.Signature, sym.ReceiverKey)
				}
				tc.addMagicEntry(sym.ReceiverKey, name, normalized, recordID)
			}
		}
	}
	for _, exp := range tc.exports {
		if exp == nil {
			continue
		}
		for _, list := range exp.Symbols {
			for i := range list {
				sym := &list[i]
				if sym.Kind != symbols.SymbolFunction || sym.ReceiverKey == "" || sym.Signature == nil || sym.Name == "" {
					continue
				}
				if sym.Name == "__to" {
					if ok, _ := validToSignature(sym.Signature, sym.ReceiverKey); !ok {
						continue
					}
				}
				// Only normalize operators to enforce alias type safety.
				// Preserve actual self parameter types for user methods (for implicit borrow checking).
				normalized := sym.Signature
				if isOperatorMagicName(sym.Name) {
					normalized = normalizeSignatureForReceiver(sym.Signature, sym.ReceiverKey)
				}
				tc.addMagicEntry(sym.ReceiverKey, sym.Name, normalized, symbols.NoSymbolID)
			}
		}
	}
}

func isOperatorMagicName(name string) bool {
	switch name {
	case "__add", "__sub", "__mul", "__div", "__mod",
		"__bit_and", "__bit_or", "__bit_xor", "__shl", "__shr",
		"__eq", "__ne", "__lt", "__le", "__gt", "__ge",
		"__pos", "__neg", "__not":
		return true
	default:
		return false
	}
}

func isMapIndexMagicName(name string) bool {
	return name == "__index" || name == "__index_set"
}

func isMapReceiverKey(key symbols.TypeKey) bool {
	recv := strings.TrimSpace(string(key))
	for {
		switch {
		case strings.HasPrefix(recv, "&mut "):
			recv = strings.TrimSpace(strings.TrimPrefix(recv, "&mut "))
		case strings.HasPrefix(recv, "&"):
			recv = strings.TrimSpace(strings.TrimPrefix(recv, "&"))
		case strings.HasPrefix(recv, "own "):
			recv = strings.TrimSpace(strings.TrimPrefix(recv, "own "))
		case strings.HasPrefix(recv, "*"):
			recv = strings.TrimSpace(strings.TrimPrefix(recv, "*"))
		default:
			return strings.HasPrefix(recv, "Map")
		}
	}
}

func (tc *typeChecker) addMagicEntry(receiver symbols.TypeKey, name string, sig *symbols.FunctionSignature, symID symbols.SymbolID) {
	if receiver == "" || name == "" || sig == nil {
		return
	}
	receiver = canonicalTypeKey(receiver)
	if tc.magic == nil {
		tc.magic = make(map[symbols.TypeKey]map[string][]*symbols.FunctionSignature)
	}
	methods := tc.magic[receiver]
	if methods == nil {
		methods = make(map[string][]*symbols.FunctionSignature)
		tc.magic[receiver] = methods
	}
	methods[name] = append(methods[name], sig)
	if symID.IsValid() {
		if tc.magicSymbols == nil {
			tc.magicSymbols = make(map[*symbols.FunctionSignature]symbols.SymbolID)
		}
		tc.magicSymbols[sig] = symID
	}
}

func (tc *typeChecker) magicSymbolForSignature(sig *symbols.FunctionSignature) symbols.SymbolID {
	if tc == nil || sig == nil || tc.magicSymbols == nil {
		return symbols.NoSymbolID
	}
	if symID, ok := tc.magicSymbols[sig]; ok {
		return symID
	}
	return symbols.NoSymbolID
}

func (tc *typeChecker) magicResultForBinary(left, right types.TypeID, op ast.ExprBinaryOp) types.TypeID {
	name := magicNameForBinaryOp(op)
	if name == "" {
		return types.NoTypeID
	}
	if sig, lc, rc := tc.magicSignatureForBinary(left, right, op); sig != nil {
		res := tc.typeFromKey(sig.Result)
		if res == types.NoTypeID {
			res = tc.magicResultFallback(sig.Result, lc, rc)
		}
		if res == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.adjustAliasBinaryResult(res, lc, rc)
	}
	return types.NoTypeID
}

func (tc *typeChecker) magicSignatureForUnaryExpr(operandExpr ast.ExprID, operand types.TypeID, op ast.ExprUnaryOp) (sig *symbols.FunctionSignature, cand typeKeyCandidate, ambiguous bool, borrowInfo borrowMatchInfo) {
	name := magicNameForUnaryOp(op)
	if name == "" {
		return nil, typeKeyCandidate{}, false, borrowMatchInfo{}
	}
	bestCost := -1
	var bestSig *symbols.FunctionSignature
	var bestCand typeKeyCandidate
	for _, candidate := range tc.typeKeyCandidates(operand) {
		if candidate.key == "" {
			continue
		}
		for _, method := range tc.lookupMagicMethods(candidate.key, name) {
			if method == nil || !tc.signatureMatchesUnary(method, operand, candidate.key) {
				continue
			}
			cost, ok := tc.magicParamCost(method.Params[0], operand, operandExpr, &borrowInfo)
			if !ok {
				continue
			}
			if bestCost == -1 || cost < bestCost {
				bestCost = cost
				ambiguous = false
				bestSig = method
				bestCand = candidate
			}
		}
	}
	if bestCost == -1 {
		return nil, typeKeyCandidate{}, false, borrowInfo
	}
	return bestSig, bestCand, ambiguous, borrowInfo
}

func (tc *typeChecker) magicSignatureForBinary(left, right types.TypeID, op ast.ExprBinaryOp) (sig *symbols.FunctionSignature, leftCand, rightCand typeKeyCandidate) {
	name := magicNameForBinaryOp(op)
	if name == "" {
		return nil, typeKeyCandidate{}, typeKeyCandidate{}
	}
	leftCandidates := tc.typeKeyCandidates(left)
	rightCandidates := tc.typeKeyCandidates(right)
	for _, lc := range leftCandidates {
		if lc.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(lc.key, name)
		if len(methods) == 0 {
			continue
		}
		for _, sig := range methods {
			if sig == nil {
				continue
			}
			for _, rc := range rightCandidates {
				if rc.key == "" {
					continue
				}
				if !tc.signatureMatchesBinary(sig, left, right, lc.key, rc.key) {
					continue
				}
				if tc.arrayMagicInvolves(sig, lc.key, rc.key) && !tc.arrayMagicCompatible(lc.base, rc.base) {
					continue
				}
				if lc.alias != types.NoTypeID || rc.alias != types.NoTypeID {
					if !compatibleAliasFallback(lc, rc) {
						continue
					}
				}
				return sig, lc, rc
			}
		}
	}
	return nil, typeKeyCandidate{}, typeKeyCandidate{}
}

func (tc *typeChecker) magicSignatureForBinaryExpr(leftExpr, rightExpr ast.ExprID, left, right types.TypeID, op ast.ExprBinaryOp) (sig *symbols.FunctionSignature, leftCand, rightCand typeKeyCandidate, ambiguous bool, borrowInfo borrowMatchInfo) {
	name := magicNameForBinaryOp(op)
	if name == "" {
		return nil, typeKeyCandidate{}, typeKeyCandidate{}, false, borrowMatchInfo{}
	}
	bestCost := -1
	leftCandidates := tc.typeKeyCandidates(left)
	rightCandidates := tc.typeKeyCandidates(right)
	var bestSig *symbols.FunctionSignature
	for _, lc := range leftCandidates {
		if lc.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(lc.key, name)
		if len(methods) == 0 {
			continue
		}
		for _, method := range methods {
			if method == nil {
				continue
			}
			for _, rc := range rightCandidates {
				if rc.key == "" {
					continue
				}
				if !tc.signatureMatchesBinary(method, left, right, lc.key, rc.key) {
					continue
				}
				if tc.arrayMagicInvolves(method, lc.key, rc.key) && !tc.arrayMagicCompatible(lc.base, rc.base) {
					continue
				}
				if lc.alias != types.NoTypeID || rc.alias != types.NoTypeID {
					if !compatibleAliasFallback(lc, rc) {
						continue
					}
				}
				costLeft, ok := tc.magicParamCost(method.Params[0], left, leftExpr, &borrowInfo)
				if !ok {
					continue
				}
				costRight, ok := tc.magicParamCost(method.Params[1], right, rightExpr, &borrowInfo)
				if !ok {
					continue
				}
				cost := costLeft + costRight
				if bestCost == -1 || cost < bestCost {
					bestCost = cost
					ambiguous = false
					bestSig = method
					leftCand = lc
					rightCand = rc
				}
			}
		}
	}
	if bestCost == -1 {
		return nil, typeKeyCandidate{}, typeKeyCandidate{}, false, borrowInfo
	}
	return bestSig, leftCand, rightCand, ambiguous, borrowInfo
}

func (tc *typeChecker) magicResultForCast(source, target types.TypeID) types.TypeID {
	if source == types.NoTypeID || target == types.NoTypeID {
		return types.NoTypeID
	}
	if tc.types != nil {
		targetVal := tc.resolveAlias(target)
		if targetVal == tc.types.Builtins().String {
			srcVal := tc.valueType(source)
			if elem, ok := tc.arrayElemType(srcVal); ok {
				if tc.magicResultForCast(elem, targetVal) == types.NoTypeID {
					return types.NoTypeID
				}
			}
		}
	}
	targetCandidates := tc.typeKeyCandidates(target)
	for _, lc := range tc.typeKeyCandidates(source) {
		if lc.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(lc.key, "__to")
		if len(methods) == 0 {
			continue
		}
		for _, sig := range methods {
			if sig == nil || len(sig.Params) < 2 {
				continue
			}
			if !tc.magicParamCompatible(sig.Params[0], source, lc.key) {
				continue
			}
			for _, rc := range targetCandidates {
				if rc.key == "" || !typeKeyEqual(sig.Params[1], rc.key) {
					continue
				}
				if rc.alias != types.NoTypeID {
					return rc.alias
				}
				return target
			}
		}
	}
	return types.NoTypeID
}

func (tc *typeChecker) lookupMagicMethods(receiver symbols.TypeKey, name string) []*symbols.FunctionSignature {
	if receiver == "" || name == "" {
		return nil
	}
	receiver = canonicalTypeKey(receiver)
	if tc.magic == nil {
		return nil
	}
	if methods := tc.magic[receiver]; methods != nil {
		return methods[name]
	}
	for key, methods := range tc.magic {
		if typeKeyMatchesWithGenerics(key, receiver) {
			if list := methods[name]; len(list) > 0 {
				return list
			}
		}
	}
	return nil
}

func (tc *typeChecker) magicResultFallback(result symbols.TypeKey, left, right typeKeyCandidate) types.TypeID {
	if result == "" {
		return types.NoTypeID
	}
	if typeKeyEqual(result, left.key) && left.base != types.NoTypeID {
		return left.base
	}
	if typeKeyEqual(result, right.key) && right.base != types.NoTypeID {
		return right.base
	}
	return types.NoTypeID
}

func magicNameForBinaryOp(op ast.ExprBinaryOp) string {
	switch op {
	case ast.ExprBinaryAdd:
		return "__add"
	case ast.ExprBinarySub:
		return "__sub"
	case ast.ExprBinaryMul:
		return "__mul"
	case ast.ExprBinaryDiv:
		return "__div"
	case ast.ExprBinaryMod:
		return "__mod"
	case ast.ExprBinaryBitAnd:
		return "__bit_and"
	case ast.ExprBinaryBitOr:
		return "__bit_or"
	case ast.ExprBinaryBitXor:
		return "__bit_xor"
	case ast.ExprBinaryShiftLeft:
		return "__shl"
	case ast.ExprBinaryShiftRight:
		return "__shr"
	case ast.ExprBinaryEq:
		return "__eq"
	case ast.ExprBinaryNotEq:
		return "__ne"
	case ast.ExprBinaryLess:
		return "__lt"
	case ast.ExprBinaryLessEq:
		return "__le"
	case ast.ExprBinaryGreater:
		return "__gt"
	case ast.ExprBinaryGreaterEq:
		return "__ge"
	default:
		return ""
	}
}

func magicNameForUnaryOp(op ast.ExprUnaryOp) string {
	switch op {
	case ast.ExprUnaryPlus:
		return "__pos"
	case ast.ExprUnaryMinus:
		return "__neg"
	case ast.ExprUnaryNot:
		return "__not"
	default:
		return ""
	}
}

func (tc *typeChecker) magicParamCompatible(expected symbols.TypeKey, actual types.TypeID, actualKey symbols.TypeKey) bool {
	if expected == "" || actual == types.NoTypeID {
		return false
	}
	if tc.methodParamMatches(expected, actual) {
		return true
	}
	expectedStr := strings.TrimSpace(string(expected))
	if strings.HasPrefix(expectedStr, "own ") {
		inner := strings.TrimSpace(strings.TrimPrefix(expectedStr, "own "))
		return typeKeyEqual(symbols.TypeKey(inner), actualKey)
	}
	if strings.HasPrefix(expectedStr, "&") {
		return tc.selfParamCompatible(actual, expected, actualKey)
	}
	return false
}

func (tc *typeChecker) magicParamCost(expected symbols.TypeKey, actual types.TypeID, expr ast.ExprID, info *borrowMatchInfo) (int, bool) {
	if expected == "" || actual == types.NoTypeID {
		return 0, false
	}
	expectedStr := strings.TrimSpace(string(expected))
	switch {
	case strings.HasPrefix(expectedStr, "&mut "):
		if tc.isReferenceType(actual) {
			if !tc.isMutRefType(actual) {
				return 0, false
			}
			return 0, true
		}
		if !tc.isAddressableExpr(expr) {
			if info != nil {
				info.record(expr, true, borrowFailureNotAddressable)
			}
			return 0, false
		}
		if !tc.isMutablePlaceExpr(expr) {
			if info != nil {
				info.record(expr, true, borrowFailureImmutable)
			}
			return 0, false
		}
		return 1, true
	case strings.HasPrefix(expectedStr, "&"):
		if tc.isReferenceType(actual) {
			return 0, true
		}
		expectedType := tc.typeFromKey(expected)
		if tc.isBorrowableStringLiteral(expr, expectedType) {
			return 1, true
		}
		if tc.canMaterializeForRefString(expr, expectedType) {
			return 2, true
		}
		if !tc.isAddressableExpr(expr) {
			if info != nil {
				info.record(expr, false, borrowFailureNotAddressable)
			}
			return 0, false
		}
		return 1, true
	default:
		if tc.isReferenceType(actual) {
			return 0, true
		}
		val := tc.valueType(actual)
		if val != types.NoTypeID && !tc.isCopyType(val) {
			return 3, true
		}
		return 0, true
	}
}

func (tc *typeChecker) signatureMatchesUnary(sig *symbols.FunctionSignature, operand types.TypeID, operandKey symbols.TypeKey) bool {
	if sig == nil || operand == types.NoTypeID || len(sig.Params) == 0 {
		return false
	}
	return tc.magicParamCompatible(sig.Params[0], operand, operandKey)
}

func (tc *typeChecker) signatureMatchesBinary(sig *symbols.FunctionSignature, left, right types.TypeID, leftKey, rightKey symbols.TypeKey) bool {
	if sig == nil || left == types.NoTypeID || right == types.NoTypeID || len(sig.Params) < 2 {
		return false
	}
	return tc.magicParamCompatible(sig.Params[0], left, leftKey) &&
		tc.magicParamCompatible(sig.Params[1], right, rightKey)
}

func (tc *typeChecker) arrayMagicInvolves(sig *symbols.FunctionSignature, left, right symbols.TypeKey) bool {
	if sig == nil || len(sig.Params) < 2 {
		return false
	}
	return isArrayKey(sig.Params[0]) && isArrayKey(sig.Params[1]) && isArrayKey(left) && isArrayKey(right)
}

func (tc *typeChecker) arrayMagicCompatible(left, right types.TypeID) bool {
	lelem, llen, lfixed, lok := tc.arrayInfo(left)
	relem, rlen, rfixed, rok := tc.arrayInfo(right)
	if !lok || !rok {
		return false
	}
	if !tc.typesAssignable(lelem, relem, true) || !tc.typesAssignable(relem, lelem, true) {
		return false
	}
	if lfixed || rfixed {
		return lfixed && rfixed && llen == rlen
	}
	return true
}

func (tc *typeChecker) acceptToSignature(sig *symbols.FunctionSignature, receiver symbols.TypeKey, sym *symbols.Symbol) bool {
	ok, reason := validToSignature(sig, receiver)
	if ok {
		return true
	}
	tc.reportInvalidToSignature(sym, sig, reason)
	return false
}

func validToSignature(sig *symbols.FunctionSignature, receiver symbols.TypeKey) (ok bool, reason string) {
	if sig == nil {
		return false, "missing signature"
	}
	if receiver == "" {
		return false, "missing receiver type"
	}
	if len(sig.Params) != 2 {
		return false, "must take exactly two parameters (self, target)"
	}
	if len(sig.Variadic) == len(sig.Params) {
		for _, variadic := range sig.Variadic {
			if variadic {
				return false, "variadic parameters are not allowed on __to"
			}
		}
	}
	if !typeKeyEqual(sig.Params[0], receiver) {
		selfStr := strings.TrimSpace(string(sig.Params[0]))
		inner := ""
		switch {
		case strings.HasPrefix(selfStr, "&mut "):
			inner = strings.TrimSpace(strings.TrimPrefix(selfStr, "&mut "))
		case strings.HasPrefix(selfStr, "&"):
			inner = strings.TrimSpace(strings.TrimPrefix(selfStr, "&"))
		case strings.HasPrefix(selfStr, "own "):
			inner = strings.TrimSpace(strings.TrimPrefix(selfStr, "own "))
		}
		if inner == "" || !typeKeyEqual(symbols.TypeKey(inner), receiver) {
			return false, "first parameter must match extern receiver type"
		}
	}
	target := sig.Params[1]
	if target == "" {
		return false, "missing target type"
	}
	if sig.Result != target {
		return false, "return type must be the target type"
	}
	return true, ""
}

func (tc *typeChecker) reportInvalidToSignature(sym *symbols.Symbol, sig *symbols.FunctionSignature, reason string) {
	if sym == nil || tc.reporter == nil {
		return
	}
	self := typeKeyLabel(sym.ReceiverKey)
	target := "_"
	if sig != nil && len(sig.Params) >= 2 {
		target = typeKeyLabel(sig.Params[1])
	}
	expected := "__to(self: " + self + ", target: " + target + ") -> " + target
	msg := "__to must match fn " + expected
	if reason != "" {
		msg += ": " + reason
	}
	if b := diag.ReportError(tc.reporter, diag.SemaTypeMismatch, sym.Span, msg); b != nil {
		b.Emit()
	}
}

func typeKeyLabel(key symbols.TypeKey) string {
	if key == "" {
		return "_"
	}
	return string(key)
}
