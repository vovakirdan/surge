package mir

import (
	"fmt"

	"surge/internal/hir"
)

func (l *funcLowerer) lowerSelectTimeout(expr *hir.Expr, data hir.CallData) (SelectArm, loweredSelectArm, error) {
	if len(data.Args) != 2 {
		return SelectArm{}, loweredSelectArm{}, fmt.Errorf("mir: select await: timeout expects 2 arguments")
	}
	task, err := l.lowerExpr(data.Args[0], false)
	if err != nil {
		return SelectArm{}, loweredSelectArm{}, err
	}
	ms, err := l.lowerExpr(data.Args[1], false)
	if err != nil {
		return SelectArm{}, loweredSelectArm{}, err
	}
	// The evaluate-once timeout survives pending polls and cancellation in its
	// own frame slot. A borrowed counted value must supply that slot a reference.
	if ms.Kind == OperandCopy && l.isRefCounted(ms.Type) {
		ms.Kind = OperandRetain
	}
	msTmp := l.newTemp(ms.Type, "select_ms", expr.Span)
	l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
		Dst: Place{Local: msTmp},
		Src: RValue{Kind: RValueUse, Use: ms},
	}})
	return SelectArm{
			Kind: SelectArmTimeout,
			Task: task,
			Ms:   l.placeOperand(Place{Local: msTmp}, ms.Type, false),
		}, loweredSelectArm{
			kind:    SelectArmTimeout,
			task:    task,
			msLocal: msTmp,
		}, nil
}
