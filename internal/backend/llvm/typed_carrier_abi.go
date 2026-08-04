package llvm

import (
	"fmt"
	"strings"
)

func typedCarrierRuntimeDecls() []builtinDecl {
	decls := make([]builtinDecl, 0, len(typedCarrierLLVMRuntimeFunctions)+1)
	for index := range typedCarrierLLVMRuntimeFunctions {
		function := &typedCarrierLLVMRuntimeFunctions[index]
		decl := builtinDecl{
			name:       function.name,
			ret:        resolveTypedCarrierLLVMType(function.result),
			retAttrs:   append([]string(nil), function.resultAttributes...),
			params:     make([]string, 0, len(function.parameters)),
			paramAttrs: make([][]string, 0, len(function.parameters)),
		}
		for _, parameter := range function.parameters {
			decl.params = append(decl.params, resolveTypedCarrierLLVMType(parameter.ty))
			decl.paramAttrs = append(decl.paramAttrs, append([]string(nil), parameter.attributes...))
		}
		decls = append(decls, decl)
	}
	decls = append(decls, builtinDecl{name: typedCarrierSentinelSymbol, ret: "void"})
	return decls
}

func emitTypedCarrierABI(out *strings.Builder) {
	for _, record := range typedCarrierLLVMRecords {
		fields := make([]string, 0, len(record.fields))
		for _, field := range record.fields {
			fields = append(fields, resolveTypedCarrierLLVMType(field))
		}
		fmt.Fprintf(out, "%%struct.%s = type { %s }\n", record.name, strings.Join(fields, ", "))
	}
	out.WriteString("\n")
	fmt.Fprintf(out, "@llvm.used = appending global [2 x ptr] [ptr @%s, ptr @__surge_require_typed_carrier_abi], section \"llvm.metadata\"\n", typedCarrierSentinelSymbol)
	out.WriteString("@llvm.global_ctors = appending global [1 x { i32, ptr, ptr }] [{ i32, ptr, ptr } { i32 0, ptr @__surge_require_typed_carrier_abi, ptr null }]\n\n")
	out.WriteString("define internal void @__surge_require_typed_carrier_abi() noinline {\nentry:\n")
	fmt.Fprintf(out, "  call void @%s()\n", typedCarrierSentinelSymbol)
	out.WriteString("  ret void\n}\n\n")
}

func resolveTypedCarrierLLVMType(ty string) string {
	if ty == "intptr" {
		// The current LLVM target is x86_64. The manifest itself deliberately
		// carries logical usize/uintptr rather than this target selection.
		return "i64"
	}
	return ty
}

func formatRuntimeDecl(decl *builtinDecl) string {
	result := decl.ret
	if len(decl.retAttrs) > 0 {
		result = strings.Join(decl.retAttrs, " ") + " " + result
	}
	parameters := make([]string, len(decl.params))
	for index, parameterType := range decl.params {
		parameters[index] = parameterType
		if index < len(decl.paramAttrs) && len(decl.paramAttrs[index]) > 0 {
			parameters[index] += " " + strings.Join(decl.paramAttrs[index], " ")
		}
	}
	return fmt.Sprintf("declare %s @%s(%s)", result, decl.name, strings.Join(parameters, ", "))
}
