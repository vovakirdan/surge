package asyncrt

import "fmt"

// ScopeID identifies an async scope.
type ScopeID uint64

// Scope tracks structured-concurrency membership for an async body.
type Scope struct {
	ID                ScopeID
	Owner             TaskID
	Children          []TaskID
	Failfast          bool
	FailfastTriggered bool
}

// EnterScope registers a new scope owned by the given task.
func (e *Executor) EnterScope(owner TaskID, failfast bool) ScopeID {
	if e == nil {
		return 0
	}
	if e.nextScopeID == 0 {
		e.nextScopeID = 1
	}
	id := ScopeID(e.nextScopeID)
	e.nextScopeID++
	if e.scopes == nil {
		e.scopes = make(map[ScopeID]*Scope)
	}
	scope := &Scope{
		ID:       id,
		Owner:    owner,
		Failfast: failfast,
	}
	e.scopes[id] = scope
	if task := e.tasks[owner]; task != nil {
		task.ScopeID = id
	}
	return id
}

// ExitScope validates that all children completed and removes the scope.
func (e *Executor) ExitScope(scopeID ScopeID) {
	if e == nil || scopeID == 0 {
		return
	}
	scope := e.scopes[scopeID]
	if scope == nil {
		return
	}
	live := make([]TaskID, 0, len(scope.Children))
	for _, child := range scope.Children {
		task := e.tasks[child]
		if task == nil || task.Status == TaskDone {
			continue
		}
		live = append(live, child)
	}
	if len(live) > 0 {
		panic(fmt.Sprintf("scope %d exited with live children: %v", scopeID, live))
	}
	delete(e.scopes, scopeID)
	if task := e.tasks[scope.Owner]; task != nil && task.ScopeID == scopeID {
		task.ScopeID = 0
	}
}

// RegisterChild records a child task in the scope.
func (e *Executor) RegisterChild(scopeID ScopeID, child TaskID) {
	if e == nil || scopeID == 0 {
		return
	}
	scope := e.scopes[scopeID]
	if scope == nil {
		return
	}
	scope.Children = append(scope.Children, child)
	if task := e.tasks[child]; task != nil {
		task.ParentScopeID = scopeID
	}
}

// CancelAllChildren cancels all children in spawn order.
func (e *Executor) CancelAllChildren(scopeID ScopeID) {
	if e == nil || scopeID == 0 {
		return
	}
	scope := e.scopes[scopeID]
	if scope == nil {
		return
	}
	for _, child := range scope.Children {
		e.Cancel(child)
	}
}

// JoinAllChildrenBlocking advances join-all in spawn order.
// Returns done=false with the first pending child to wait on, or done=true
// when all children are complete, along with the failfast outcome.
func (e *Executor) JoinAllChildrenBlocking(scopeID ScopeID) (done bool, pending TaskID, failfast bool) {
	if e == nil || scopeID == 0 {
		return true, 0, false
	}
	scope := e.scopes[scopeID]
	if scope == nil {
		return true, 0, false
	}
	for _, child := range scope.Children {
		task := e.tasks[child]
		if task == nil || task.Status == TaskDone {
			continue
		}
		return false, child, scope.FailfastTriggered
	}
	return true, 0, scope.FailfastTriggered
}
