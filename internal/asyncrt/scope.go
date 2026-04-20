package asyncrt

import (
	"fmt"
	"slices"
)

// ScopeExitError reports that a scope was exited with live children still attached.
type ScopeExitError struct {
	ScopeID      ScopeID
	LiveChildren []TaskID
}

func (e *ScopeExitError) Error() string {
	if e == nil {
		return "scope exited with live children"
	}
	return fmt.Sprintf("scope %d exited with live children: %v", e.ScopeID, e.LiveChildren)
}

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
	e.compactScopeChildren(scope)
	if len(scope.Children) > 0 {
		panic(&ScopeExitError{
			ScopeID:      scopeID,
			LiveChildren: slices.Clone(scope.Children),
		})
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
	task := e.tasks[child]
	if task == nil || task.ScopeRegistered {
		return
	}
	if task.Status == TaskDone {
		if task.ResultKind == TaskResultCancelled && scope.Failfast && !scope.FailfastTriggered {
			scope.FailfastTriggered = true
			e.CancelAllChildren(scopeID)
			if owner := e.tasks[scope.Owner]; owner != nil && owner.Status != TaskDone {
				e.Wake(scope.Owner)
			}
		}
		return
	}
	scope.Children = append(scope.Children, child)
	task.ParentScopeID = scopeID
	task.ScopeRegistered = true
}

// CancelAllChildren cancels all children in task order.
func (e *Executor) CancelAllChildren(scopeID ScopeID) {
	if e == nil || scopeID == 0 {
		return
	}
	scope := e.scopes[scopeID]
	if scope == nil {
		return
	}
	e.compactScopeChildren(scope)
	for _, child := range scope.Children {
		e.Cancel(child)
	}
}

// JoinAllChildrenBlocking advances join-all in task order.
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
	e.compactScopeChildren(scope)
	if len(scope.Children) > 0 {
		return false, scope.Children[0], scope.FailfastTriggered
	}
	return true, 0, scope.FailfastTriggered
}

func (e *Executor) compactScopeChildren(scope *Scope) {
	if e == nil || scope == nil || len(scope.Children) == 0 {
		return
	}
	scope.Children = slices.DeleteFunc(scope.Children, func(child TaskID) bool {
		task := e.tasks[child]
		if task == nil || task.Status == TaskDone {
			if task != nil {
				task.ParentScopeID = 0
				task.ScopeRegistered = false
			}
			return true
		}
		return false
	})
}

func (e *Executor) unregisterScopeChild(task *Task) {
	if e == nil || task == nil {
		return
	}
	scopeID := task.ParentScopeID
	if scopeID != 0 {
		if scope := e.scopes[scopeID]; scope != nil && len(scope.Children) > 0 {
			if idx := slices.Index(scope.Children, task.ID); idx >= 0 {
				scope.Children = slices.Delete(scope.Children, idx, idx+1)
			}
		}
	}
	task.ParentScopeID = 0
	task.ScopeRegistered = false
}
