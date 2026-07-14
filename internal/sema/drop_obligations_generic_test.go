package sema

import "testing"

// Moves ARE observed through generic calls now (the resolver's generic
// branch applies argument ownership like the monomorphic one), so
// generic bodies carry honest obligations: a moved-onward param drops
// nowhere in the mover, and the final owner drops it once. The original
// double free (push -> array_push both dropping one handle) stays
// pinned end to end by the string-array row in the scope-exit e2e.
func TestGenericBodiesCarryHonestDropObligations(t *testing.T) {
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
	// inner keeps its param (drops it: one obligation record); outer
	// moved v onward (no record).
	withDrop := 0
	for _, drops := range res.ScopeEndDrops {
		if len(drops) == 1 {
			withDrop++
		} else if len(drops) != 0 {
			t.Fatalf("unexpected obligation shape: %v", res.ScopeEndDrops)
		}
	}
	if withDrop != 1 {
		t.Fatalf("expected exactly inner's param obligation, got %v", res.ScopeEndDrops)
	}
}
