package main

import (
	"fmt"

	"surge/internal/driver"
	"surge/internal/parser"
)

// Turning what the user TYPED into what the driver understands.
//
// Both of these refuse rather than default: an unknown --stages or
// --directives value is a typo, and silently running "all" because the string
// did not match would hide it. The stage check also enforces the one
// dependency between flags — the emit switches need a stage that actually
// produces what they ask to see.

func resolveDiagnoseStage(stagesStr string, emitInstantiations, emitMono, emitMIR bool) (driver.DiagnoseStage, error) {
	var stage driver.DiagnoseStage
	switch stagesStr {
	case "tokenize":
		stage = driver.DiagnoseStageTokenize
	case "syntax":
		stage = driver.DiagnoseStageSyntax
	case "sema":
		stage = driver.DiagnoseStageSema
	case "all":
		stage = driver.DiagnoseStageAll
	default:
		return stage, fmt.Errorf("unknown stages value: %s", stagesStr)
	}
	semaReached := stage == driver.DiagnoseStageSema || stage == driver.DiagnoseStageAll
	if emitInstantiations && !semaReached {
		return stage, fmt.Errorf("--emit-instantiations requires --stages sema|all")
	}
	if emitMono && !semaReached {
		return stage, fmt.Errorf("--emit-mono requires --stages sema|all")
	}
	if emitMIR && !semaReached {
		return stage, fmt.Errorf("--emit-mir requires --stages sema|all")
	}
	return stage, nil
}

func resolveDirectiveMode(directivesStr string) (parser.DirectiveMode, error) {
	switch directivesStr {
	case "off":
		return parser.DirectiveModeOff, nil
	case "collect":
		return parser.DirectiveModeCollect, nil
	case "gen":
		return parser.DirectiveModeGen, nil
	case "run":
		return parser.DirectiveModeRun, nil
	default:
		return parser.DirectiveModeOff, fmt.Errorf("unknown directives value: %s", directivesStr)
	}
}
