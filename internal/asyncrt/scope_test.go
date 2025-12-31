package asyncrt

import (
	"fmt"
	"testing"
)

func TestScopeExitPanicsOnLiveChildren(t *testing.T) {
	exec := NewExecutor(Config{Deterministic: true})
	owner := exec.Spawn(1, nil)
	scopeID := exec.EnterScope(owner, false)
	child := exec.Spawn(2, nil)
	exec.RegisterChild(scopeID, child)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic on scope exit with live children")
		}
		msg := fmt.Sprint(r)
		want := fmt.Sprintf("scope %d exited with live children: [%d]", scopeID, child)
		if msg != want {
			t.Fatalf("panic mismatch: want %q, got %q", want, msg)
		}
	}()

	exec.ExitScope(scopeID)
}
