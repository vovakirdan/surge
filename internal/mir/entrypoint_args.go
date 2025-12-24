package mir

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/types"
)

func (b *surgeStartBuilder) prepareArgsNone() []Operand {
	params := b.entryMF.Func.Params
	if len(params) == 0 {
		return nil
	}
	args := make([]Operand, 0, len(params))
	for i, param := range params {
		argLocal := b.newLocal(param.Name, param.Type, b.localFlags(param.Type))
		b.registerParamLocal(param, argLocal)
		if !param.HasDefault || param.Default == nil {
			b.emitExitWithMessage(fmt.Sprintf("missing default for parameter %q", param.Name), 1)
			break
		}
		op, err := b.lowerDefaultExpr(param.Default)
		if err != nil {
			b.emitExitWithMessage(fmt.Sprintf("failed to lower default for parameter %q", param.Name), 1)
			break
		}
		b.emitAssign(argLocal, &RValue{Kind: RValueUse, Use: op})
		args = append(args, Operand{Kind: OperandMove, Place: Place{Local: argLocal}})
		if i < len(params)-1 && b.curBlock().Terminated() {
			break
		}
	}
	return args
}

func (b *surgeStartBuilder) prepareArgsArgv() []Operand {
	params := b.entryMF.Func.Params
	if len(params) == 0 {
		return nil
	}

	// L_argv = call rt_argv()
	argvType := b.stringArrayType()
	argvLocal := b.newLocal("argv", argvType, LocalFlags(0))
	b.emitCallIntrinsic(argvLocal, "rt_argv", nil)

	argvLenLocal := b.newLocal("argv_len", b.uintType(), LocalFlagCopy)
	b.emitCallIntrinsic(argvLenLocal, "__len", []Operand{
		{Kind: OperandCopy, Place: Place{Local: argvLocal}},
	})

	args := make([]Operand, 0, len(params))
	for i, param := range params {
		argIdx, err := safecast.Conv[uint64](i)
		if err != nil {
			b.emitExitWithMessage("argv index overflow", 1)
			break
		}

		argLocal := b.newLocal(param.Name, param.Type, b.localFlags(param.Type))
		b.registerParamLocal(param, argLocal)

		erringType := b.erringType(param.Type)
		if erringType == types.NoTypeID {
			b.emitExitWithMessage("missing Erring type for entrypoint parsing", 1)
			break
		}

		condLocal := b.newLocal(fmt.Sprintf("has_arg%d", i), b.boolType(), LocalFlagCopy)
		b.emitAssign(condLocal, &RValue{
			Kind: RValueBinaryOp,
			Binary: BinaryOp{
				Op: ast.ExprBinaryLess,
				Left: Operand{
					Kind: OperandConst,
					Type: b.uintType(),
					Const: Const{
						Kind:      ConstUint,
						Type:      b.uintType(),
						UintValue: argIdx,
					},
				},
				Right: Operand{Kind: OperandCopy, Place: Place{Local: argvLenLocal}},
			},
		})

		hasArgBB := b.newBlock()
		noArgBB := b.newBlock()
		nextBB := b.newBlock()
		b.setTerm(&Terminator{
			Kind: TermIf,
			If: IfTerm{
				Cond: Operand{Kind: OperandCopy, Place: Place{Local: condLocal}},
				Then: hasArgBB,
				Else: noArgBB,
			},
		})

		b.startBlock(hasArgBB)
		argStrLocal := b.newLocal(fmt.Sprintf("arg_str%d", i), b.stringType(), LocalFlags(0))
		b.emitIndex(argStrLocal, argvLocal, i)
		parseLocal := b.newLocal(fmt.Sprintf("arg_parsed%d", i), erringType, LocalFlags(0))
		b.emitFromStrCall(parseLocal, argStrLocal, param.Type)

		okLocal := b.newLocal(fmt.Sprintf("arg_ok%d", i), b.boolType(), LocalFlagCopy)
		b.emitTagTest(okLocal, parseLocal, "Success")

		okBB := b.newBlock()
		errBB := b.newBlock()
		b.setTerm(&Terminator{
			Kind: TermIf,
			If: IfTerm{
				Cond: Operand{Kind: OperandCopy, Place: Place{Local: okLocal}},
				Then: okBB,
				Else: errBB,
			},
		})

		b.startBlock(okBB)
		b.emitTagPayload(argLocal, parseLocal, "Success", 0)
		b.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: nextBB}})

		b.startBlock(errBB)
		b.emitCallIntrinsic(NoLocalID, "exit", []Operand{{Kind: OperandMove, Place: Place{Local: parseLocal}}})
		b.setTerm(&Terminator{Kind: TermReturn})

		b.startBlock(noArgBB)
		if param.HasDefault && param.Default != nil {
			op, err := b.lowerDefaultExpr(param.Default)
			if err != nil {
				b.emitExitWithMessage(fmt.Sprintf("failed to lower default for parameter %q", param.Name), 1)
			} else {
				b.emitAssign(argLocal, &RValue{Kind: RValueUse, Use: op})
			}
		} else {
			b.emitExitWithMessage(fmt.Sprintf("missing argv argument %q", param.Name), 1)
		}
		if !b.curBlock().Terminated() {
			b.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: nextBB}})
		}

		b.startBlock(nextBB)
		args = append(args, Operand{Kind: OperandMove, Place: Place{Local: argLocal}})
	}

	return args
}

func (b *surgeStartBuilder) prepareArgsStdin() []Operand {
	params := b.entryMF.Func.Params
	if len(params) == 0 {
		return nil
	}
	if len(params) > 1 {
		b.emitExitWithMessage("multiple stdin parameters are not supported yet", 7001)
		return nil
	}

	// L_stdin = call rt_stdin_read_all()
	stdinLocal := b.newLocal("stdin", b.stringType(), LocalFlags(0))
	b.emitCallIntrinsic(stdinLocal, "rt_stdin_read_all", nil)

	param := params[0]
	argLocal := b.newLocal(param.Name, param.Type, b.localFlags(param.Type))
	b.registerParamLocal(param, argLocal)

	erringType := b.erringType(param.Type)
	if erringType == types.NoTypeID {
		b.emitExitWithMessage("missing Erring type for entrypoint parsing", 1)
		return nil
	}

	parseLocal := b.newLocal("stdin_parsed", erringType, LocalFlags(0))
	b.emitFromStrCall(parseLocal, stdinLocal, param.Type)

	okLocal := b.newLocal("stdin_ok", b.boolType(), LocalFlagCopy)
	b.emitTagTest(okLocal, parseLocal, "Success")

	okBB := b.newBlock()
	errBB := b.newBlock()
	nextBB := b.newBlock()
	b.setTerm(&Terminator{
		Kind: TermIf,
		If: IfTerm{
			Cond: Operand{Kind: OperandCopy, Place: Place{Local: okLocal}},
			Then: okBB,
			Else: errBB,
		},
	})

	b.startBlock(okBB)
	b.emitTagPayload(argLocal, parseLocal, "Success", 0)
	b.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: nextBB}})

	b.startBlock(errBB)
	b.emitCallIntrinsic(NoLocalID, "exit", []Operand{{Kind: OperandMove, Place: Place{Local: parseLocal}}})
	b.setTerm(&Terminator{Kind: TermReturn})

	b.startBlock(nextBB)
	return []Operand{{Kind: OperandMove, Place: Place{Local: argLocal}}}
}
