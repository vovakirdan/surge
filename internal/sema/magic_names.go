package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) buildMagicIndex() {
	tc.magic = make(map[symbols.TypeKey]map[string]*symbols.FunctionSignature)
	if tc.symbols != nil && tc.symbols.Table != nil && tc.symbols.Table.Symbols != nil {
		if data := tc.symbols.Table.Symbols.Data(); data != nil {
			for i := range data {
				sym := data[i]
				if sym.Kind != symbols.SymbolFunction || sym.ReceiverKey == "" || sym.Signature == nil {
					continue
				}
				name := tc.symbolName(sym.Name)
				tc.addMagicEntry(sym.ReceiverKey, name, sym.Signature)
			}
		}
	}
	for _, exp := range tc.exports {
		if exp == nil {
			continue
		}
		for _, list := range exp.Symbols {
			for _, sym := range list {
				if sym.Kind != symbols.SymbolFunction || sym.ReceiverKey == "" || sym.Signature == nil || sym.Name == "" {
					continue
				}
				tc.addMagicEntry(sym.ReceiverKey, sym.Name, sym.Signature)
			}
		}
	}
}

func (tc *typeChecker) addMagicEntry(receiver symbols.TypeKey, name string, sig *symbols.FunctionSignature) {
	if receiver == "" || name == "" || sig == nil {
		return
	}
	if tc.magic == nil {
		tc.magic = make(map[symbols.TypeKey]map[string]*symbols.FunctionSignature)
	}
	methods := tc.magic[receiver]
	if methods == nil {
		methods = make(map[string]*symbols.FunctionSignature)
		tc.magic[receiver] = methods
	}
	methods[name] = sig
}

func (tc *typeChecker) magicResultForUnary(operand types.TypeID, op ast.ExprUnaryOp) types.TypeID {
	name := magicNameForUnaryOp(op)
	if name == "" {
		return types.NoTypeID
	}
	key := tc.typeKeyForType(operand)
	if key == "" {
		return types.NoTypeID
	}
	if sig := tc.lookupMagicMethod(key, name); sig != nil && tc.signatureMatchesUnary(sig, key) {
		return tc.typeFromKey(sig.Result)
	}
	return types.NoTypeID
}

func (tc *typeChecker) magicResultForBinary(left, right types.TypeID, op ast.ExprBinaryOp) types.TypeID {
	name := magicNameForBinaryOp(op)
	if name == "" {
		return types.NoTypeID
	}
	leftKey := tc.typeKeyForType(left)
	rightKey := tc.typeKeyForType(right)
	if leftKey == "" || rightKey == "" {
		return types.NoTypeID
	}
	if sig := tc.lookupMagicMethod(leftKey, name); sig != nil && tc.signatureMatchesBinary(sig, leftKey, rightKey) {
		return tc.typeFromKey(sig.Result)
	}
	return types.NoTypeID
}

func (tc *typeChecker) lookupMagicMethod(receiver symbols.TypeKey, name string) *symbols.FunctionSignature {
	if receiver == "" || name == "" {
		return nil
	}
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
	default:
		return ""
	}
}

func (tc *typeChecker) signatureMatchesUnary(sig *symbols.FunctionSignature, operand symbols.TypeKey) bool {
	if sig == nil || operand == "" || len(sig.Params) == 0 {
		return false
	}
	return sig.Params[0] == operand
}

func (tc *typeChecker) signatureMatchesBinary(sig *symbols.FunctionSignature, left, right symbols.TypeKey) bool {
	if sig == nil || left == "" || right == "" {
		return false
	}
	if len(sig.Params) < 2 {
		return false
	}
	return sig.Params[0] == left && sig.Params[1] == right
}
