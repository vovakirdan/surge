package mir

import (
	"fmt"

	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

type entrypointCallableTarget struct {
	outcome    sema.EntrypointCallableOutcome
	instance   symbols.SymbolID
	paramTypes []types.TypeID
}

func (b *surgeStartBuilder) loadEntrypointCallables() error {
	if b == nil || b.entryMF == nil || b.entryMF.Func == nil {
		return fmt.Errorf("entrypoint startup: missing entrypoint function")
	}
	b.fromString = make(map[uint32]entrypointCallableTarget)
	entrypoint := b.entryMF.OrigSym
	if !entrypoint.IsValid() {
		entrypoint = b.entryMF.Func.SymbolID
	}
	if !entrypoint.IsValid() {
		return fmt.Errorf("entrypoint startup: missing original entrypoint symbol")
	}

	if b.sema != nil {
		for i := range b.sema.EntrypointCallableBindings {
			binding := &b.sema.EntrypointCallableBindings[i]
			if binding.Entrypoint != entrypoint {
				continue
			}
			target := entrypointCallableTarget{
				outcome:    binding.Outcome,
				paramTypes: append([]types.TypeID(nil), binding.ParamTypes...),
			}
			switch binding.Outcome {
			case sema.EntrypointCallableBuiltin:
			case sema.EntrypointCallableUser:
				instance, ok, err := b.mm.Callables.LookupChecked(binding.Callee, binding.TemplateArgs)
				if err != nil {
					return fmt.Errorf("entrypoint startup: resolve callable %s: %w", binding.CalleeKey, err)
				}
				if !ok || !instance.IsValid() {
					return fmt.Errorf("entrypoint startup: callable %s was selected by sema but has no retained mono instance", binding.CalleeKey)
				}
				target.instance = instance
			default:
				return fmt.Errorf("entrypoint startup: callable binding has unknown outcome %d", binding.Outcome)
			}
			switch binding.Role {
			case sema.EntrypointReturnToInt:
				if b.returnToInt != nil && !entrypointCallableTargetsEqual(*b.returnToInt, target) {
					return fmt.Errorf("entrypoint startup: conflicting __to bindings")
				}
				copy := target
				b.returnToInt = &copy
			case sema.EntrypointParamFromString:
				if previous, ok := b.fromString[binding.ParamIndex]; ok && !entrypointCallableTargetsEqual(previous, target) {
					return fmt.Errorf("entrypoint startup: conflicting from_str bindings for parameter %d", binding.ParamIndex)
				}
				b.fromString[binding.ParamIndex] = target
			default:
				return fmt.Errorf("entrypoint startup: callable binding has unknown role %d", binding.Role)
			}
		}
	}

	result := b.entryMF.Func.Result
	if result != types.NoTypeID && !b.isNothingType(result) && !b.isIntType(result) && b.returnToInt == nil {
		return fmt.Errorf("entrypoint startup: non-int return type has no sema-resolved __to binding")
	}
	if b.mode == symbols.EntrypointModeArgv || b.mode == symbols.EntrypointModeStdin {
		for i, param := range b.entryMF.Func.Params {
			index := uint32(i) //nolint:gosec -- a function parameter slice cannot approach uint32 capacity
			if _, ok := b.fromString[index]; !ok {
				return fmt.Errorf("entrypoint startup: parameter %q has no sema-resolved from_str binding", param.Name)
			}
		}
	}
	return nil
}

func entrypointCallableTargetsEqual(left, right entrypointCallableTarget) bool {
	if left.outcome != right.outcome || left.instance != right.instance || len(left.paramTypes) != len(right.paramTypes) {
		return false
	}
	for i := range left.paramTypes {
		if left.paramTypes[i] != right.paramTypes[i] {
			return false
		}
	}
	return true
}

func (b *surgeStartBuilder) entrypointReceiverOperand(local LocalID, paramType, valueType types.TypeID) Operand {
	if b != nil && b.typesIn != nil && paramType != types.NoTypeID {
		if param, ok := b.typesIn.Lookup(b.resolveAlias(paramType)); ok && param.Kind == types.KindReference {
			kind := OperandAddrOf
			if param.Mutable {
				kind = OperandAddrOfMut
			}
			return Operand{Kind: kind, Type: paramType, Place: Place{Local: local}}
		}
	}
	return Operand{Kind: OperandMove, Type: valueType, Place: Place{Local: local}}
}
