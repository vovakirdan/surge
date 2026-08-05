package mir

import (
	"fmt"

	"surge/internal/sema"
)

func (b *surgeStartBuilder) emitFromArgvCall(dst, stringLocal LocalID, paramIndex uint32) {
	arg := Operand{Kind: OperandAddrOf, Type: b.refType(b.stringType(), false), Place: Place{Local: stringLocal}}
	target, ok := b.fromArgv[paramIndex]
	if !ok {
		b.setEntrypointParseError("missing from_str binding for parameter %d", paramIndex)
		return
	}
	if target.outcome == sema.EntrypointCallableBuiltin {
		b.emitCallIntrinsic(dst, "from_str", []Operand{arg}, borrowArgContracts(1))
		return
	}
	if target.outcome != sema.EntrypointCallableUser || !target.instance.IsValid() || len(target.paramTypes) != 1 {
		b.setEntrypointParseError("invalid from_str binding for parameter %d", paramIndex)
		return
	}
	arg.Type = target.paramTypes[0]
	b.emitCall(dst, target.instance, "from_str", []Operand{arg}, borrowArgContracts(1))
}

func (b *surgeStartBuilder) emitFromStdinCall(dst, stringLocal LocalID) {
	if b.fromStdin == nil {
		b.setEntrypointParseError("missing from_stdin binding")
		return
	}
	target := *b.fromStdin
	arg := Operand{Kind: OperandMove, Type: b.stringType(), Place: Place{Local: stringLocal}}
	contracts := []ArgContract{ArgContractTransferOwned}
	if target.outcome == sema.EntrypointCallableBuiltin {
		b.emitCallIntrinsic(dst, "from_stdin", []Operand{arg}, contracts)
		return
	}
	if target.outcome != sema.EntrypointCallableUser || !target.instance.IsValid() || len(target.paramTypes) != 1 {
		b.setEntrypointParseError("invalid from_stdin binding")
		return
	}
	arg.Type = target.paramTypes[0]
	b.emitCall(dst, target.instance, "from_stdin", []Operand{arg}, contracts)
}

func (b *surgeStartBuilder) setEntrypointParseError(format string, args ...any) {
	if b.err == nil {
		b.err = fmt.Errorf("entrypoint startup: "+format, args...)
	}
}
