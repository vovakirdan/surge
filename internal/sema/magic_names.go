package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

func canonicalTypeKey(key symbols.TypeKey) symbols.TypeKey {
	if key == "" {
		return ""
	}
	s := string(key)
	if _, ok := arrayKeyInner(s); ok {
		return symbols.TypeKey("[]")
	}
	return key
}

func typeKeyEqual(a, b symbols.TypeKey) bool {
	return canonicalTypeKey(a) == canonicalTypeKey(b)
}

func normalizeSignatureForReceiver(sig *symbols.FunctionSignature, receiver symbols.TypeKey) *symbols.FunctionSignature {
	if sig == nil || receiver == "" || len(sig.Params) == 0 {
		return sig
	}
	recv := canonicalTypeKey(receiver)
	if typeKeyEqual(sig.Params[0], recv) {
		return sig
	}
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
				name := tc.symbolName(sym.Name)
				if name == "__to" && !tc.acceptToSignature(sym.Signature, sym.ReceiverKey, sym) {
					continue
				}
				tc.addMagicEntry(sym.ReceiverKey, name, normalizeSignatureForReceiver(sym.Signature, sym.ReceiverKey))
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
				tc.addMagicEntry(sym.ReceiverKey, sym.Name, normalizeSignatureForReceiver(sym.Signature, sym.ReceiverKey))
			}
		}
	}
}

func (tc *typeChecker) addMagicEntry(receiver symbols.TypeKey, name string, sig *symbols.FunctionSignature) {
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
}

func (tc *typeChecker) magicResultForUnary(operand types.TypeID, op ast.ExprUnaryOp) types.TypeID {
	name := magicNameForUnaryOp(op)
	if name == "" {
		return types.NoTypeID
	}
	for _, cand := range tc.typeKeyCandidates(operand) {
		if cand.key == "" {
			continue
		}
		for _, sig := range tc.lookupMagicMethods(cand.key, name) {
			if sig == nil || !tc.signatureMatchesUnary(sig, cand.key) {
				continue
			}
			res := tc.typeFromKey(sig.Result)
			return tc.adjustAliasUnaryResult(res, cand)
		}
	}
	return types.NoTypeID
}

func (tc *typeChecker) magicResultForBinary(left, right types.TypeID, op ast.ExprBinaryOp) types.TypeID {
	name := magicNameForBinaryOp(op)
	if name == "" {
		return types.NoTypeID
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
				if !tc.signatureMatchesBinary(sig, lc.key, rc.key) {
					continue
				}
				if lc.alias != types.NoTypeID || rc.alias != types.NoTypeID {
					if !compatibleAliasFallback(lc, rc) {
						continue
					}
				}
				res := tc.typeFromKey(sig.Result)
				return tc.adjustAliasBinaryResult(res, lc, rc)
			}
		}
	}
	return types.NoTypeID
}

func (tc *typeChecker) magicResultForCast(source, target types.TypeID) types.TypeID {
	if source == types.NoTypeID || target == types.NoTypeID {
		return types.NoTypeID
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

func (tc *typeChecker) magicResultForIndex(container, index types.TypeID) types.TypeID {
	if container == types.NoTypeID {
		return types.NoTypeID
	}
	for _, recv := range tc.typeKeyCandidates(container) {
		if recv.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(recv.key, "__index")
		for _, sig := range methods {
			if sig == nil || len(sig.Params) < 2 || !typeKeyEqual(sig.Params[0], recv.key) {
				continue
			}
			if !tc.methodParamMatches(sig.Params[1], index) {
				continue
			}
			res := tc.typeFromKey(sig.Result)
			if res == types.NoTypeID {
				if elem, ok := tc.elementType(recv.base); ok {
					return elem
				}
				continue
			}
			return res
		}
	}
	return types.NoTypeID
}

func (tc *typeChecker) hasIndexSetter(container, index, value types.TypeID) bool {
	if container == types.NoTypeID || value == types.NoTypeID {
		return false
	}
	for _, recv := range tc.typeKeyCandidates(container) {
		if recv.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(recv.key, "__index_set")
		for _, sig := range methods {
			if sig == nil || len(sig.Params) < 3 || !typeKeyEqual(sig.Params[0], recv.key) {
				continue
			}
			if !tc.methodParamMatches(sig.Params[1], index) {
				continue
			}
			if !tc.methodParamMatches(sig.Params[2], value) {
				continue
			}
			return true
		}
	}
	return false
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
	return nil
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

func (tc *typeChecker) signatureMatchesUnary(sig *symbols.FunctionSignature, operand symbols.TypeKey) bool {
	if sig == nil || operand == "" || len(sig.Params) == 0 {
		return false
	}
	return typeKeyEqual(sig.Params[0], operand)
}

func (tc *typeChecker) signatureMatchesBinary(sig *symbols.FunctionSignature, left, right symbols.TypeKey) bool {
	if sig == nil || left == "" || right == "" || len(sig.Params) < 2 {
		return false
	}
	return typeKeyEqual(sig.Params[0], left) && typeKeyEqual(sig.Params[1], right)
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
	if sig.Params[0] != receiver {
		return false, "first parameter must match extern receiver type"
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
