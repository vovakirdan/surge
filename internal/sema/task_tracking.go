package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
)

// TaskInfo tracks a task within a scope for structured concurrency enforcement.
type TaskInfo struct {
	ID           uint32           // Unique task identifier
	SpawnExpr    ast.ExprID       // The task expression that created this task
	Span         source.Span      // Location of the task keyword
	Binding      symbols.SymbolID // If assigned to a variable (for await tracking)
	Scope        symbols.ScopeID  // The scope where this task was created
	Awaited      bool             // Whether .await() was called on this task
	Returned     bool             // Whether the task was returned from the scope
	InAsyncBlock bool             // Whether task was created inside async block (for error differentiation)
}

// TaskTracker manages task lifecycle within scopes for structured concurrency.
// It tracks tasks and ensures they are properly awaited before scope exit.
type TaskTracker struct {
	tasks         []TaskInfo                   // All tasks, indexed by ID (0 unused)
	scopeTasks    map[symbols.ScopeID][]uint32 // taskID list per scope
	bindingTasks  map[symbols.SymbolID]uint32  // binding -> taskID
	exprTasks     map[ast.ExprID]uint32        // task expression -> taskID
	pendingPassed map[ast.ExprID]struct{}      // task expressions marked passed before SpawnTask
	nextID        uint32                       // Next task ID to assign
}

// NewTaskTracker creates a new TaskTracker for structured concurrency analysis.
func NewTaskTracker() *TaskTracker {
	return &TaskTracker{
		tasks:         make([]TaskInfo, 1), // index 0 unused
		scopeTasks:    make(map[symbols.ScopeID][]uint32),
		bindingTasks:  make(map[symbols.SymbolID]uint32),
		exprTasks:     make(map[ast.ExprID]uint32),
		pendingPassed: make(map[ast.ExprID]struct{}),
		nextID:        1,
	}
}

// SpawnTask records a new task in the given scope.
// inAsyncBlock indicates whether the task was created inside an async block (for error differentiation).
// Returns the task ID for later binding/tracking.
func (tt *TaskTracker) SpawnTask(expr ast.ExprID, span source.Span, scope symbols.ScopeID, inAsyncBlock bool) uint32 {
	id := tt.nextID
	tt.nextID++

	info := TaskInfo{
		ID:           id,
		SpawnExpr:    expr,
		Span:         span,
		Scope:        scope,
		InAsyncBlock: inAsyncBlock,
	}
	tt.tasks = append(tt.tasks, info)
	tt.scopeTasks[scope] = append(tt.scopeTasks[scope], id)
	tt.exprTasks[expr] = id
	if _, ok := tt.pendingPassed[expr]; ok {
		tt.tasks[id].Returned = true
		delete(tt.pendingPassed, expr)
	}
	return id
}

// BindTask associates a task with a variable binding for await tracking.
func (tt *TaskTracker) BindTask(taskID uint32, binding symbols.SymbolID) {
	if taskID == 0 || !binding.IsValid() {
		return
	}
	if int(taskID) >= len(tt.tasks) {
		return
	}
	tt.tasks[taskID].Binding = binding
	tt.bindingTasks[binding] = taskID
}

// BindTaskByExpr associates a task with a binding using the task expression.
func (tt *TaskTracker) BindTaskByExpr(expr ast.ExprID, binding symbols.SymbolID) {
	if !expr.IsValid() || !binding.IsValid() {
		return
	}
	if taskID, ok := tt.exprTasks[expr]; ok {
		tt.BindTask(taskID, binding)
	}
}

// MarkAwaited marks a task as awaited by its binding.
func (tt *TaskTracker) MarkAwaited(binding symbols.SymbolID) {
	if !binding.IsValid() {
		return
	}
	if taskID, ok := tt.bindingTasks[binding]; ok && taskID != 0 {
		if int(taskID) < len(tt.tasks) {
			tt.tasks[taskID].Awaited = true
		}
	}
}

// MarkAwaitedByExpr marks a task as awaited by its task expression.
func (tt *TaskTracker) MarkAwaitedByExpr(expr ast.ExprID) {
	if !expr.IsValid() {
		return
	}
	if taskID, ok := tt.exprTasks[expr]; ok && taskID != 0 {
		if int(taskID) < len(tt.tasks) {
			tt.tasks[taskID].Awaited = true
		}
	}
}

// MarkReturned marks a task as returned from its scope (ownership transfer).
func (tt *TaskTracker) MarkReturned(binding symbols.SymbolID) {
	if !binding.IsValid() {
		return
	}
	if taskID, ok := tt.bindingTasks[binding]; ok && taskID != 0 {
		if int(taskID) < len(tt.tasks) {
			tt.tasks[taskID].Returned = true
		}
	}
}

// MarkReturnedByExpr marks a task as returned using its task expression.
func (tt *TaskTracker) MarkReturnedByExpr(expr ast.ExprID) {
	if !expr.IsValid() {
		return
	}
	if taskID, ok := tt.exprTasks[expr]; ok && taskID != 0 {
		if int(taskID) < len(tt.tasks) {
			tt.tasks[taskID].Returned = true
		}
	}
}

// MarkPassed marks a task as passed to another function (ownership transfer).
// Semantically equivalent to MarkReturned - the callee is now responsible for awaiting.
func (tt *TaskTracker) MarkPassed(binding symbols.SymbolID) {
	tt.MarkReturned(binding)
}

// MarkPassedByExpr marks a task as passed using its task expression.
func (tt *TaskTracker) MarkPassedByExpr(expr ast.ExprID) {
	if !expr.IsValid() {
		return
	}
	if taskID, ok := tt.exprTasks[expr]; ok && taskID != 0 {
		if int(taskID) < len(tt.tasks) {
			tt.tasks[taskID].Returned = true
		}
		return
	}
	if tt.pendingPassed == nil {
		tt.pendingPassed = make(map[ast.ExprID]struct{})
	}
	tt.pendingPassed[expr] = struct{}{}
}

// EndScope checks for task leaks when leaving a scope.
// Returns all tasks that were created in this scope but not awaited or returned.
func (tt *TaskTracker) EndScope(scope symbols.ScopeID) []TaskInfo {
	taskIDs, ok := tt.scopeTasks[scope]
	if !ok {
		return nil
	}

	var leaks []TaskInfo
	for _, taskID := range taskIDs {
		if int(taskID) >= len(tt.tasks) {
			continue
		}
		info := tt.tasks[taskID]
		if !info.Awaited && !info.Returned {
			leaks = append(leaks, info)
		}
	}

	delete(tt.scopeTasks, scope)
	return leaks
}

// GetTask retrieves task info by ID.
// Returns a copy of the TaskInfo to avoid pointer invalidation if the tasks slice grows.
func (tt *TaskTracker) GetTask(id uint32) (TaskInfo, bool) {
	if id == 0 || int(id) >= len(tt.tasks) {
		return TaskInfo{}, false
	}
	return tt.tasks[id], true
}

// HasTasks returns true if there are any tracked tasks.
func (tt *TaskTracker) HasTasks() bool {
	return tt.nextID > 1
}
