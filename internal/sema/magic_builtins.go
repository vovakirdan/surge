package sema

import "surge/internal/symbols"

type builtinMagicSpec struct {
	receiver string
	name     string
	params   []string
	result   string
}

var builtinMagic = []builtinMagicSpec{
	{receiver: "int", name: "__add", params: []string{"int", "int"}, result: "int"},
	{receiver: "int", name: "__sub", params: []string{"int", "int"}, result: "int"},
	{receiver: "int", name: "__mul", params: []string{"int", "int"}, result: "int"},
	{receiver: "int", name: "__div", params: []string{"int", "int"}, result: "int"},
	{receiver: "int", name: "__mod", params: []string{"int", "int"}, result: "int"},
	{receiver: "int", name: "__bit_and", params: []string{"int", "int"}, result: "int"},
	{receiver: "int", name: "__bit_or", params: []string{"int", "int"}, result: "int"},
	{receiver: "int", name: "__bit_xor", params: []string{"int", "int"}, result: "int"},
	{receiver: "int", name: "__shl", params: []string{"int", "int"}, result: "int"},
	{receiver: "int", name: "__shr", params: []string{"int", "int"}, result: "int"},
	{receiver: "int", name: "__lt", params: []string{"int", "int"}, result: "bool"},
	{receiver: "int", name: "__le", params: []string{"int", "int"}, result: "bool"},
	{receiver: "int", name: "__eq", params: []string{"int", "int"}, result: "bool"},
	{receiver: "int", name: "__ne", params: []string{"int", "int"}, result: "bool"},
	{receiver: "int", name: "__ge", params: []string{"int", "int"}, result: "bool"},
	{receiver: "int", name: "__gt", params: []string{"int", "int"}, result: "bool"},
	{receiver: "int", name: "__pos", params: []string{"int"}, result: "int"},
	{receiver: "int", name: "__neg", params: []string{"int"}, result: "int"},
	{receiver: "int", name: "__to_string", params: []string{"int"}, result: "string"},
	{receiver: "int", name: "__to", params: []string{"int", "string"}, result: "string"},
	{receiver: "int", name: "__to", params: []string{"int", "float"}, result: "float"},
	{receiver: "int", name: "__to", params: []string{"int", "uint"}, result: "uint"},

	{receiver: "uint", name: "__add", params: []string{"uint", "uint"}, result: "uint"},
	{receiver: "uint", name: "__sub", params: []string{"uint", "uint"}, result: "uint"},
	{receiver: "uint", name: "__mul", params: []string{"uint", "uint"}, result: "uint"},
	{receiver: "uint", name: "__div", params: []string{"uint", "uint"}, result: "uint"},
	{receiver: "uint", name: "__mod", params: []string{"uint", "uint"}, result: "uint"},
	{receiver: "uint", name: "__bit_and", params: []string{"uint", "uint"}, result: "uint"},
	{receiver: "uint", name: "__bit_or", params: []string{"uint", "uint"}, result: "uint"},
	{receiver: "uint", name: "__bit_xor", params: []string{"uint", "uint"}, result: "uint"},
	{receiver: "uint", name: "__shl", params: []string{"uint", "uint"}, result: "uint"},
	{receiver: "uint", name: "__shr", params: []string{"uint", "uint"}, result: "uint"},
	{receiver: "uint", name: "__lt", params: []string{"uint", "uint"}, result: "bool"},
	{receiver: "uint", name: "__le", params: []string{"uint", "uint"}, result: "bool"},
	{receiver: "uint", name: "__eq", params: []string{"uint", "uint"}, result: "bool"},
	{receiver: "uint", name: "__ne", params: []string{"uint", "uint"}, result: "bool"},
	{receiver: "uint", name: "__ge", params: []string{"uint", "uint"}, result: "bool"},
	{receiver: "uint", name: "__gt", params: []string{"uint", "uint"}, result: "bool"},
	{receiver: "uint", name: "__pos", params: []string{"uint"}, result: "uint"},
	{receiver: "uint", name: "__to_string", params: []string{"uint"}, result: "string"},
	{receiver: "uint", name: "__to", params: []string{"uint", "string"}, result: "string"},
	{receiver: "uint", name: "__to", params: []string{"uint", "int"}, result: "int"},
	{receiver: "uint", name: "__to", params: []string{"uint", "float"}, result: "float"},

	{receiver: "float", name: "__add", params: []string{"float", "float"}, result: "float"},
	{receiver: "float", name: "__sub", params: []string{"float", "float"}, result: "float"},
	{receiver: "float", name: "__mul", params: []string{"float", "float"}, result: "float"},
	{receiver: "float", name: "__div", params: []string{"float", "float"}, result: "float"},
	{receiver: "float", name: "__lt", params: []string{"float", "float"}, result: "bool"},
	{receiver: "float", name: "__le", params: []string{"float", "float"}, result: "bool"},
	{receiver: "float", name: "__eq", params: []string{"float", "float"}, result: "bool"},
	{receiver: "float", name: "__ne", params: []string{"float", "float"}, result: "bool"},
	{receiver: "float", name: "__ge", params: []string{"float", "float"}, result: "bool"},
	{receiver: "float", name: "__gt", params: []string{"float", "float"}, result: "bool"},
	{receiver: "float", name: "__pos", params: []string{"float"}, result: "float"},
	{receiver: "float", name: "__neg", params: []string{"float"}, result: "float"},
	{receiver: "float", name: "__to_string", params: []string{"float"}, result: "string"},
	{receiver: "float", name: "__to", params: []string{"float", "string"}, result: "string"},
	{receiver: "float", name: "__to", params: []string{"float", "int"}, result: "int"},
	{receiver: "float", name: "__to", params: []string{"float", "uint"}, result: "uint"},

	{receiver: "string", name: "__add", params: []string{"string", "string"}, result: "string"},
	{receiver: "string", name: "__mul", params: []string{"string", "int"}, result: "string"},
	{receiver: "string", name: "__eq", params: []string{"string", "string"}, result: "bool"},
	{receiver: "string", name: "__ne", params: []string{"string", "string"}, result: "bool"},
	{receiver: "string", name: "__to_string", params: []string{"string"}, result: "string"},
	{receiver: "string", name: "__to", params: []string{"string", "int"}, result: "int"},
	{receiver: "string", name: "__to", params: []string{"string", "uint"}, result: "uint"},
	{receiver: "string", name: "__to", params: []string{"string", "float"}, result: "float"},

	{receiver: "bool", name: "__eq", params: []string{"bool", "bool"}, result: "bool"},
	{receiver: "bool", name: "__ne", params: []string{"bool", "bool"}, result: "bool"},
	{receiver: "bool", name: "__to_string", params: []string{"bool"}, result: "string"},
	{receiver: "bool", name: "__not", params: []string{"bool"}, result: "bool"},
	{receiver: "bool", name: "__to", params: []string{"bool", "string"}, result: "string"},
}

func (tc *typeChecker) ensureBuiltinMagic() {
	for _, spec := range builtinMagic {
		if spec.receiver == "" || spec.name == "" {
			continue
		}
		recv := symbols.TypeKey(spec.receiver)
		if tc.hasMagicSignature(recv, spec.name, spec.params, spec.result) {
			continue
		}
		sig := &symbols.FunctionSignature{
			Params: make([]symbols.TypeKey, len(spec.params)),
			Result: symbols.TypeKey(spec.result),
		}
		for i, param := range spec.params {
			sig.Params[i] = symbols.TypeKey(param)
		}
		tc.addMagicEntry(recv, spec.name, sig)
	}
}

func (tc *typeChecker) hasMagicSignature(receiver symbols.TypeKey, name string, params []string, result string) bool {
	methods := tc.lookupMagicMethods(receiver, name)
	for _, sig := range methods {
		if sig == nil || len(sig.Params) != len(params) {
			continue
		}
		if string(sig.Result) != result {
			continue
		}
		match := true
		for i, param := range params {
			if string(sig.Params[i]) != param {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
