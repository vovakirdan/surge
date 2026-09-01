package llvm

import (
	"fmt"
	"strings"
	"surge/internal/valueops"
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
	fmt.Fprintf(out, "@llvm.used = appending global [3 x ptr] [ptr @%s, ptr @__surge_require_typed_carrier_abi, ptr @%s], section \"llvm.metadata\"\n",
		typedCarrierSentinelSymbol, valueops.PlanCrossUnavailableStub)
	out.WriteString("@llvm.global_ctors = appending global [1 x { i32, ptr, ptr }] [{ i32, ptr, ptr } { i32 0, ptr @__surge_require_typed_carrier_abi, ptr null }]\n\n")
	out.WriteString("define internal void @__surge_require_typed_carrier_abi() noinline {\nentry:\n")
	fmt.Fprintf(out, "  call void @%s()\n", typedCarrierSentinelSymbol)
	out.WriteString("  ret void\n}\n\n")
	emitPlanCrossUnavailableStub(out)
}

// emitPlanCrossUnavailableStub defines the module-local symbol every descriptor
// without a crossing capability binds into rt_value_ops.plan_cross.
//
// It does not return a status, and that is the point. A descriptor whose own
// flags admit neither cross mode has no legal `mode` argument, so reaching this
// body means the caller chose an operation the descriptor excludes — a protocol
// violation, not an outcome. Returning any rt_carrier_status would assert the
// call was legal and merely declined, and none of the seven values means "this
// type cannot cross" anyway.
//
// It is module-local rather than a runtime symbol because the runtime may never
// call it: declaring it in the frozen manifest would extend the ABI, move the
// hash and the link sentinel, and buy no capability. It is kept in llvm.used so
// that being unreferenced — which it is until descriptors are emitted — does not
// let it be dropped before the slot rule that names it can be honoured.
// This is the model's proved-unreachable bare-trap exemption: valid source
// cannot request a crossing mode when the descriptor advertises neither one.
// The terminality probe deliberately bypasses that source/descriptor validity
// rule; dispatch from that negative probe does not make the stub program-reachable.
func emitPlanCrossUnavailableStub(out *strings.Builder) {
	fmt.Fprintf(out, "define internal zeroext i32 @%s(ptr %%src, i8 zeroext %%mode, ptr %%out) noinline {\nentry:\n",
		valueops.PlanCrossUnavailableStub)
	if emitDescriptorDefect == defectReturningPlanCrossStub {
		// The negative control for the stub's terminality: a body that hands
		// back a status instead of trapping. It travels the ordinary emission
		// path so the test that catches it is testing the real writer.
		out.WriteString("  ret i32 0\n}\n\n")
		return
	}
	out.WriteString("  call void @llvm.trap()\n")
	out.WriteString("  unreachable\n}\n\n")
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
