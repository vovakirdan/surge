package mir

import (
	"surge/internal/sema"
	"surge/internal/types"
)

// buildAsyncInitialFrame describes the initial frame without creating a task.
// The caller supplies each parameter's ownership transfer: an ordinary async
// call acquires borrowed RC arguments, while a placed body receives holders
// already made private for transport. The returned tag construction precedes
// consumption of the frame literal; neither operation can suspend.
func buildAsyncInitialFrame(
	f *Func, in *types.Interner, facts *sema.Result,
	stateType, payloadType types.TypeID, start stateVariant,
	residents residentSet, startResidents []LocalID,
	parameter func(LocalID) Operand,
) (Instr, StructLit) {
	payload := addLocal(f, "__payload_init", payloadType, localFlagsFor(in, facts, payloadType))
	args := make([]Operand, 0, len(start.locals))
	for _, id := range start.locals {
		args = append(args, parameter(id))
	}
	init := Instr{Kind: InstrCall, Call: CallInstr{
		HasDst:       true,
		Dst:          Place{Local: payload},
		Callee:       Callee{Kind: CalleeSym, Sym: start.tagSym, Name: start.name},
		Args:         args,
		ArgContracts: storeArgContracts(len(args)),
	}}
	integer := in.Builtins().Int
	constant := func(value int64) Operand {
		return Operand{Kind: OperandConst, Type: integer,
			Const: Const{Kind: ConstInt, Type: integer, IntValue: value}}
	}
	frame := StructLit{TypeID: stateType, Fields: []StructLitField{
		// PACKED from birth: cancellation before the first poll must reclaim
		// the parameters, not only the frame's allocation.
		{Name: FrameStateField, Value: constant(FrameStatePacked)},
		{Name: asyncStatePcField, Value: constant(int64(start.resumeBB))},
		{Name: asyncStatePayloadField, Value: operandForLocal(f, payload)},
	}}
	// Promoted parameters arrive in their stable resident fields. Body locals
	// are deliberately absent: aggregate construction zeroes them, and their
	// first body assignment establishes their lifetime. Generic frame disposal
	// must never treat those uninitialized slots as owned values.
	for _, id := range startResidents {
		frame.Fields = append(frame.Fields, StructLitField{
			Name: residents.fields[id], Value: parameter(id),
		})
	}
	return init, frame
}
