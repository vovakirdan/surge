package mir

import (
	"fmt"

	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Startup includes user-defined argument parsers, default expressions and exit
// conversions. They run on the root carrier as well; the external adapter only
// waits for a fixed-width exit code and terminates the process.
func wrapRootStartup(m *Module, root *Func, in *types.Interner, facts *sema.Result) (*Func, error) {
	root.Name = "__surge_root$startup"
	root.IsAsync = true
	root.Result = in.Builtins().Int64
	root.AsyncTaskType, _ = rootTaskTypes(in, root.Result)
	root.ParamCount = 0
	root.ScopeLocal = addLocal(root, "__scope", in.Builtins().Uint64, LocalFlagCopy)
	for bi := range root.Blocks {
		for ii := range root.Blocks[bi].Instrs {
			ins := root.Blocks[bi].Instrs[ii]
			if ins.Kind != InstrCall || ins.Call.Callee.Sym.IsValid() ||
				(ins.Call.Callee.Name != "rt_exit" && ins.Call.Callee.Name != "exit") {
				continue
			}
			if len(ins.Call.Args) != 1 {
				return nil, fmt.Errorf("mir: root exit requires one argument")
			}
			code := addLocal(root, "__root_exit_code", in.Builtins().Int64, LocalFlagCopy)
			var release *Instr
			if ins.Call.Callee.Name == "exit" {
				ins.Call.Callee.Name = "__entry_error_code"
				ins.Call.HasDst, ins.Call.Dst = true, Place{Local: code}
			} else {
				value := ins.Call.Args[0]
				if value.Type == types.NoTypeID && value.Place.Kind == PlaceLocal && len(value.Place.Proj) == 0 {
					value.Type = root.Locals[value.Place.Local].Type
				}
				ins = Instr{Kind: InstrAssign, Assign: AssignInstr{
					Dst: Place{Local: code}, Src: RValue{Kind: RValueCast, Cast: CastOp{Value: value, TargetTy: in.Builtins().Int64}},
				}}
				if value.Kind != OperandConst && sema.OwnsHeapIn(in, value.Type) {
					release = &Instr{Kind: InstrDrop, Drop: DropInstr{Place: value.Place}}
				}
			}
			root.Blocks[bi].Instrs = append(root.Blocks[bi].Instrs[:ii], ins)
			if release != nil {
				root.Blocks[bi].Instrs = append(root.Blocks[bi].Instrs, *release)
			}
			root.Blocks[bi].Term = Terminator{Kind: TermReturn, Return: ReturnTerm{HasValue: true, Value: operandForLocal(root, code)}}
			break
		}
	}
	id, err := allocAsyncPollFunc(m)
	if err != nil {
		return nil, err
	}
	adapter := &Func{ID: id, Sym: symbols.NoSymbolID, Name: "__surge_start", Result: types.NoTypeID, ScopeLocal: NoLocalID, Span: root.Span}
	code := addLocal(adapter, "entry_ret", root.Result, LocalFlagCopy)
	adapter.Entry = newBlock(adapter)
	call := CallInstr{HasDst: true, Dst: Place{Local: code}, Callee: Callee{Kind: CalleeValue, Name: root.Name}}
	appendInstr(adapter, adapter.Entry, Instr{Kind: InstrCall, Call: call})
	appendInstr(adapter, adapter.Entry, Instr{Kind: InstrCall, Call: CallInstr{
		Callee: Callee{Kind: CalleeValue, Name: "rt_exit"}, Args: []Operand{operandForLocal(adapter, code)}, ArgContracts: borrowArgContracts(1),
	}})
	setBlockTerm(adapter, adapter.Entry, Terminator{Kind: TermReturn})
	taskType, resultType := rootTaskTypes(in, root.Result)
	if err := replaceRootCall(adapter, 0, 0, &call, root, taskType, resultType, in, facts); err != nil {
		return nil, err
	}
	return adapter, nil
}
