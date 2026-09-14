package sema

import (
	"context"

	"surge/internal/source"
	"surge/internal/symbols"
)

type returnOriginExitKind uint8

const (
	returnOriginFunctionReturn returnOriginExitKind = iota
	returnOriginBlockResult
	returnOriginBreak
	returnOriginContinue
)

// Site keeps distinct source operations distinct while branch joins accumulate
// their possible values. Targets distinguish nested block and loop exits.
type returnOriginExit struct {
	kind   returnOriginExitKind
	target symbols.ScopeID
	site   source.Span
}

type returnOriginOutcome struct {
	env   returnOriginEnv
	value returnOriginValue
}

type returnOriginFlow struct {
	normal returnOriginEnv
	exits  map[returnOriginExit]returnOriginOutcome
}

func (f returnOriginFlow) clone() returnOriginFlow {
	out := returnOriginFlow{normal: f.normal.clone(), exits: make(map[returnOriginExit]returnOriginOutcome, len(f.exits))}
	for key, exit := range f.exits {
		out.exits[key] = returnOriginOutcome{env: exit.env.clone(), value: exit.value.clone()}
	}
	return out
}

func (f returnOriginFlow) join(other returnOriginFlow) returnOriginFlow {
	out := f.clone()
	out.normal = out.normal.join(other.normal)
	for key, exit := range other.exits {
		if old, ok := out.exits[key]; ok {
			exit = returnOriginOutcome{env: old.env.join(exit.env), value: old.value.join(exit.value)}
		}
		out.exits[key] = returnOriginOutcome{env: exit.env.clone(), value: exit.value.clone()}
	}
	return out
}

func (f returnOriginFlow) end(key returnOriginExit, value returnOriginValue) returnOriginFlow {
	out := f.clone()
	if out.normal.reachable && value.normal {
		exit := returnOriginOutcome{env: out.normal, value: value.clone()}
		if old, ok := out.exits[key]; ok {
			exit = returnOriginOutcome{env: old.env.join(exit.env), value: old.value.join(exit.value)}
		}
		out.exits[key] = exit
	}
	out.normal = returnOriginEnv{}
	return out
}

func (f returnOriginFlow) then(step func(returnOriginEnv) (returnOriginFlow, error)) (returnOriginFlow, error) {
	if step == nil {
		panic("return origins: missing sequential transfer")
	}
	if !f.normal.reachable {
		return f.clone(), nil
	}
	next, err := step(f.normal.clone())
	if err != nil {
		return returnOriginFlow{}, err
	}
	prior := f.clone()
	prior.normal = returnOriginEnv{}
	return prior.join(next), nil
}

type returnOriginLoopStep struct {
	// Done is the condition's normal exit, after its own side effects. A
	// definitely-true condition leaves it unreachable, not reachable empty.
	done returnOriginEnv
	body returnOriginFlow
}

// The transfer must be monotone over a finite set of source roots, and must
// close body-local scopes before returning its edges. Only normal completion
// and continues to THIS loop feed the header; other exits remain outgoing.
func solveReturnOriginLoop(ctx context.Context, entry returnOriginEnv, target symbols.ScopeID, step func(returnOriginEnv) (returnOriginLoopStep, error)) (returnOriginFlow, error) {
	if ctx == nil || step == nil {
		panic("return origins: missing loop context or transfer")
	}
	header := entry.clone()
	if !header.reachable {
		return returnOriginFlow{}, nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return returnOriginFlow{}, err
		}
		iteration, err := step(header.clone())
		if err != nil {
			return returnOriginFlow{}, err
		}
		backedge := iteration.body.normal.clone()
		out := returnOriginFlow{normal: iteration.done.clone(), exits: make(map[returnOriginExit]returnOriginOutcome)}
		for key, exit := range iteration.body.exits {
			switch {
			case key.kind == returnOriginContinue && key.target == target:
				backedge = backedge.join(exit.env)
			case key.kind == returnOriginBreak && key.target == target:
				out.normal = out.normal.join(exit.env)
			default:
				out.exits[key] = returnOriginOutcome{env: exit.env.clone(), value: exit.value.clone()}
			}
		}
		next := header.join(entry).join(backedge)
		if next.equal(header) {
			return out, nil
		}
		header = next
	}
}
