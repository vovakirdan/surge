package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// taskCreationScope is a compiler-only name for one dynamic task body. It is
// never emitted into the program: the runtime carries the corresponding
// write-once creation_scope_key. A synchronous helper keeps its caller's token;
// an async function or async block gets a fresh one when its body is analysed.
type taskCreationScope uint64

type taskProvenanceKind uint8

const (
	taskProvenanceUnknown taskProvenanceKind = iota
	taskProvenanceTask
	taskProvenanceTuple
	taskProvenanceChoice
)

type taskProvenance struct {
	kind    taskProvenanceKind
	scope   taskCreationScope
	created source.Span
	elems   []taskProvenance
}

type taskProvenanceEnv map[symbols.SymbolID]taskProvenance

type taskProvenanceAnalyzer struct {
	tc        *typeChecker
	nextScope taskCreationScope
	active    map[symbols.SymbolID]struct{}
	emitted   map[taskProvenanceDiagnostic]struct{}
}

type taskProvenanceDiagnostic struct {
	spawn   source.Span
	created source.Span
}

type taskReturnMode uint8

const (
	taskReturnIgnore taskReturnMode = iota
	taskReturnFunction
	taskReturnBlock
)

func (tc *typeChecker) checkTaskCreationScopes() {
	if tc == nil || tc.builder == nil || tc.reporter == nil {
		return
	}
	file := tc.builder.Files.Get(tc.fileID)
	if file == nil {
		return
	}
	a := &taskProvenanceAnalyzer{
		tc:      tc,
		active:  make(map[symbols.SymbolID]struct{}),
		emitted: make(map[taskProvenanceDiagnostic]struct{}),
	}
	for _, itemID := range file.Items {
		fn, ok := tc.builder.Items.Fn(itemID)
		if !ok || fn == nil || !fn.Body.IsValid() {
			continue
		}
		scope := a.freshScope()
		a.analyzeFunction(itemID, fn, scope, nil)
	}
}

func (a *taskProvenanceAnalyzer) freshScope() taskCreationScope {
	a.nextScope++
	return a.nextScope
}

func (a *taskProvenanceAnalyzer) analyzeFunction(
	itemID ast.ItemID,
	fn *ast.FnItem,
	current taskCreationScope,
	args []taskProvenance,
) taskProvenance {
	if fn == nil || !fn.Body.IsValid() {
		return taskProvenance{}
	}
	symID := a.tc.typeSymbolForItem(itemID)
	if symID.IsValid() {
		if _, recursive := a.active[symID]; recursive {
			return taskProvenance{}
		}
		a.active[symID] = struct{}{}
		defer delete(a.active, symID)
	}
	env := make(taskProvenanceEnv)
	params := a.tc.fnParamSymbols(fn, a.tc.scopeForItem(itemID))
	for i, param := range params {
		if param.IsValid() && i < len(args) {
			env[param] = args[i]
		}
	}
	returns := a.walkStmt(fn.Body, env, current, taskReturnFunction)
	return mergeTaskProvenances(returns)
}

func (a *taskProvenanceAnalyzer) reportOutsideScope(
	spawn source.Span,
	origin taskProvenance,
) {
	if origin.kind != taskProvenanceTask || origin.scope == 0 {
		return
	}
	key := taskProvenanceDiagnostic{spawn: spawn, created: origin.created}
	if _, exists := a.emitted[key]; exists {
		return
	}
	a.emitted[key] = struct{}{}
	b := diag.ReportError(a.tc.reporter, diag.SemaTaskCreatedOutsideScope, spawn,
		"cannot spawn a task created outside the current scope")
	if b == nil {
		return
	}
	b.WithNote(origin.created, "task was created here, outside this scope")
	b.WithHelp(spawn, "create the task inside this scope before spawning it")
	b.Emit()
}

func taskOrigin(scope taskCreationScope, span source.Span) taskProvenance {
	return taskProvenance{kind: taskProvenanceTask, scope: scope, created: span}
}

func tupleTaskOrigin(elems []taskProvenance) taskProvenance {
	return taskProvenance{kind: taskProvenanceTuple, elems: elems}
}

func cloneTaskEnv(src taskProvenanceEnv) taskProvenanceEnv {
	dst := make(taskProvenanceEnv, len(src))
	for sym, origin := range src {
		dst[sym] = origin
	}
	return dst
}

func mergeTaskProvenances(values []taskProvenance) taskProvenance {
	if len(values) == 0 {
		return taskProvenance{}
	}
	merged := values[0]
	for _, value := range values[1:] {
		merged = mergeTaskProvenance(merged, value)
	}
	return merged
}

func mergeTaskProvenance(left, right taskProvenance) taskProvenance {
	if left.kind == taskProvenanceUnknown {
		return right
	}
	if right.kind == taskProvenanceUnknown {
		return left
	}
	if left.kind != right.kind {
		return taskChoice(left, right)
	}
	switch left.kind {
	case taskProvenanceTask:
		if left.scope != right.scope {
			return taskChoice(left, right)
		}
		return left
	case taskProvenanceTuple:
		if len(left.elems) != len(right.elems) {
			return taskProvenance{}
		}
		elems := make([]taskProvenance, len(left.elems))
		for i := range elems {
			elems[i] = mergeTaskProvenance(left.elems[i], right.elems[i])
		}
		return tupleTaskOrigin(elems)
	case taskProvenanceChoice:
		return taskChoice(left, right)
	default:
		return taskProvenance{}
	}
}

func taskChoice(values ...taskProvenance) taskProvenance {
	choice := taskProvenance{kind: taskProvenanceChoice}
	for _, value := range values {
		if value.kind == taskProvenanceUnknown {
			continue
		}
		if value.kind == taskProvenanceChoice {
			choice.elems = append(choice.elems, value.elems...)
			continue
		}
		choice.elems = append(choice.elems, value)
	}
	if len(choice.elems) == 1 {
		return choice.elems[0]
	}
	return choice
}

func outsideTaskOrigin(origin taskProvenance, current taskCreationScope) (taskProvenance, bool) {
	switch origin.kind {
	case taskProvenanceTask:
		return origin, origin.scope != current
	case taskProvenanceChoice:
		for _, candidate := range origin.elems {
			if outside, found := outsideTaskOrigin(candidate, current); found {
				return outside, true
			}
		}
	}
	return taskProvenance{}, false
}

func mergeTaskEnvs(base, left, right taskProvenanceEnv) taskProvenanceEnv {
	merged := cloneTaskEnv(base)
	for sym := range merged {
		merged[sym] = mergeTaskProvenance(left[sym], right[sym])
	}
	for sym, origin := range left {
		if _, exists := merged[sym]; exists {
			continue
		}
		merged[sym] = mergeTaskProvenance(origin, right[sym])
	}
	return merged
}
