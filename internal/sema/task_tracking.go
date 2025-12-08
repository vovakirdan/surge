package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
)

// TaskInfo tracks a spawned task within a scope for structured concurrency enforcement.
type TaskInfo struct {
	ID        uint32           // Unique task identifier
	SpawnExpr ast.ExprID       // The spawn expression that created this task
	Span      source.Span      // Location of the spawn
	Binding   symbols.SymbolID // If assigned to a variable (for await tracking)
	Scope     symbols.ScopeID  // The scope where this task was spawned
	Awaited   bool             // Whether .await() was called on this task
	Returned  bool             // Whether the task was returned from the scope
}

// TaskTracker manages task lifecycle within scopes for structured concurrency.
// It tracks spawned tasks and ensures they are properly awaited before scope exit.
type TaskTracker struct {
	tasks        []TaskInfo                   // All tasks, indexed by ID (0 unused)
	scopeTasks   map[symbols.ScopeID][]uint32 // taskID list per scope
	bindingTasks map[symbols.SymbolID]uint32  // binding -> taskID
	exprTasks    map[ast.ExprID]uint32        // spawn expression -> taskID
	nextID       uint32                       // Next task ID to assign
}

// NewTaskTracker creates a new TaskTracker for structured concurrency analysis.
func NewTaskTracker() *TaskTracker {
	return &TaskTracker{
		tasks:        make([]TaskInfo, 1), // index 0 unused
		scopeTasks:   make(map[symbols.ScopeID][]uint32),
		bindingTasks: make(map[symbols.SymbolID]uint32),
		exprTasks:    make(map[ast.ExprID]uint32),
		nextID:       1,
	}
}

// SpawnTask records a new task spawned in the given scope.
// Returns the task ID for later binding/tracking.
func (tt *TaskTracker) SpawnTask(expr ast.ExprID, span source.Span, scope symbols.ScopeID) uint32 {
	id := tt.nextID
	tt.nextID++

	info := TaskInfo{
		ID:        id,
		SpawnExpr: expr,
		Span:      span,
		Scope:     scope,
	}
	tt.tasks = append(tt.tasks, info)
	tt.scopeTasks[scope] = append(tt.scopeTasks[scope], id)
	tt.exprTasks[expr] = id
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

// BindTaskByExpr associates a task with a binding using the spawn expression.
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

// MarkAwaitedByExpr marks a task as awaited by its spawn expression.
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

// MarkReturnedByExpr marks a task as returned using its spawn expression.
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

// EndScope checks for task leaks when leaving a scope.
// Returns all tasks that were spawned in this scope but not awaited or returned.
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
func (tt *TaskTracker) GetTask(id uint32) *TaskInfo {
	if id == 0 || int(id) >= len(tt.tasks) {
		return nil
	}
	return &tt.tasks[id]
}

// HasTasks returns true if there are any tracked tasks.
func (tt *TaskTracker) HasTasks() bool {
	return tt.nextID > 1
}
