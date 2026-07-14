package sema

import "testing"

// Moves are not observed through generic calls yet, so generic-typed
// bindings must never carry drop obligations: a chain like
// push -> array_push -> intrinsic would otherwise drop the same handle
// in two callee scopes (a real double free caught end to end).
func TestGenericTypedBindingsCarryNoDropObligations(t *testing.T) {
	parseBag, semaBag, res := runSemaOnSnippetResult(t, `
fn inner<T>(v: T) -> nothing {
}

fn outer<T>(v: T) -> nothing {
    inner(v);
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil {
		t.Fatal("no sema result")
	}
	for _, drops := range res.ScopeEndDrops {
		if len(drops) != 0 {
			t.Fatalf("generic-typed bindings must not drop in vertical 1: %v", res.ScopeEndDrops)
		}
	}
}
