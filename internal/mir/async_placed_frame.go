package mir

import (
	"fmt"

	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

type placedFramePlan struct {
	stateType, payloadType types.TypeID
	poll                   FuncID
	start                  stateVariant
	residents              residentSet
	startResidents         []LocalID
}

// A containing function's liveness must see the final transported frame. Lower
// lexical crossing dependencies first; function allocation order is irrelevant.
func lowerAsyncFunctions(m *Module, in *types.Interner, facts *sema.Result, table *symbols.Table, constrained map[symbols.SymbolID]struct{}) error {
	plans := make(map[FuncID]*placedFramePlan)
	state := make(map[FuncID]uint8)
	var lower func(FuncID) error
	lower = func(id FuncID) error {
		f := m.Funcs[id]
		if f == nil || state[id] == 2 {
			return nil
		}
		if state[id] == 1 {
			return fmt.Errorf("mir: cyclic placed-body dependency in %s", f.Name)
		}
		state[id] = 1
		for bi := range f.Blocks {
			for ii := range f.Blocks[bi].Instrs {
				ins := &f.Blocks[bi].Instrs[ii]
				if ins.Kind == InstrCrossing && placedBodyCrossing(ins.Crossing.Kind) {
					if err := lower(ins.Crossing.BodyFuncID); err != nil {
						return err
					}
				}
			}
		}
		if err := rewritePlacedInitialFrames(f, plans, in, facts); err != nil {
			return err
		}
		if f.IsAsync {
			plan, err := lowerAsyncStateMachineFunc(m, f, in, facts, table, constrained)
			if err != nil {
				return err
			}
			if plan != nil {
				plans[id] = plan
			}
		}
		state[id] = 2
		return nil
	}
	for _, id := range m.SortedFuncIDs() {
		if err := lower(id); err != nil {
			return err
		}
	}
	// These were body templates, not local Task constructors. Every crossing
	// now names the generated poll and creates its initial frame directly.
	for id := range plans {
		delete(m.Funcs, id)
	}
	return nil
}

func placedBodyCrossing(kind sema.CrossingLoweringKind) bool {
	return kind == sema.CrossingLoweringSpawnOn || kind == sema.CrossingLoweringOnPlacement
}

func rewritePlacedInitialFrames(f *Func, plans map[FuncID]*placedFramePlan, in *types.Interner, facts *sema.Result) error {
	for bi := range f.Blocks {
		var instructions []Instr
		for i := range f.Blocks[bi].Instrs {
			ins := f.Blocks[bi].Instrs[i]
			if ins.Kind != InstrCrossing || !placedBodyCrossing(ins.Crossing.Kind) || ins.Crossing.CapturesPacked {
				instructions = append(instructions, ins)
				continue
			}
			crossing := &ins.Crossing
			plan := plans[crossing.BodyFuncID]
			if plan == nil {
				return fmt.Errorf("mir: crossing in %s has no placed frame plan for fn%d", f.Name, crossing.BodyFuncID)
			}
			inputs := make([]Operand, len(crossing.Captures))
			for i := range inputs {
				if i+1 >= len(crossing.State.Fields) {
					return fmt.Errorf("mir: placed capture %d missing from initial state", i)
				}
				inputs[i] = crossing.State.Fields[i+1].Value
				// LowerModule checked the unshare act before any async split.
				// Packing must transfer that same private holder, never retain it.
				if needsRelinquishWalkIn(in, crossing.Captures[i].Type) && inputs[i].Kind != OperandMove && inputs[i].Kind != OperandConst {
					return fmt.Errorf("mir: placed capture %d is not a private transfer", i)
				}
			}
			for _, locals := range [][]LocalID{plan.start.locals, plan.startResidents} {
				for _, id := range locals {
					if id < 0 || int(id) >= len(inputs) {
						return fmt.Errorf("mir: placed frame parameter L%d has no capture", id)
					}
				}
			}
			init, frame := buildAsyncInitialFrame(f, in, facts, plan.stateType, plan.payloadType,
				plan.start, plan.residents, plan.startResidents, func(id LocalID) Operand { return inputs[id] })
			instructions = append(instructions, init)
			crossing.State = frame
			crossing.BodyFuncID = plan.poll
			crossing.CapturesPacked = true
			for i := range crossing.Captures {
				crossing.Captures[i].Value = Operand{Type: crossing.Captures[i].Type}
			}
			instructions = append(instructions, ins)
		}
		f.Blocks[bi].Instrs = instructions
	}
	return nil
}
