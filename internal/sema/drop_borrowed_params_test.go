package sema

import (
	"crypto/sha256"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// These are cleanup candidates, not transfers of the caller's parameter owner.
// Concrete readonly preparation removes them before MIR; a working owner uses
// the same identity and the existing explicit-drop/rebind/exit planning.
// SEMA also records the body-end candidate after a tail return; HIR omits that
// unreachable normal-exit drop and emits only the return's cleanup list.
func TestBorrowedParameterDropObligations(t *testing.T) {
	cases := []struct {
		name, source, binding string
		normal, early, old    int
	}{
		{"readonly_counted", "fn subject(p: int) {}", "int", 1, 0, 0},
		{"readonly_generic", "fn subject<T>(p: T) {}", "generic", 1, 0, 0},
		{"early_return", `fn subject(p: int, stop: bool) -> int {
    if stop { return 0; }
    p = 2;
    return 0;
}`, "int", 1, 2, 1},
		{"conditional_rebind", `fn subject(p: int, change: bool) {
    if change { p = 2; }
}`, "int", 1, 0, 1},
		{"loop_rebind", `fn subject(p: int, rounds: uint64) {
    let mut i: uint64 = 0:uint64;
    while i < rounds {
        let inner: string = "loop";
        if i == 1:uint64 { break; }
        if i == 2:uint64 { continue; }
        p = 2;
        i = i + 1:uint64;
    }
}`, "int", 1, 0, 1},
		{"explicit_drop", "fn subject(p: int) { @drop p; }", "int", 0, 0, 0},
		{"drop_then_rebind", "fn subject(p: int) { @drop p; p = 2; }", "int", 1, 0, 0},
		{"returned_counted", "fn subject(p: int) -> int { p = p + 1; return p; }", "int", 1, 1, 1},
		// The referent overwrite is recorded; the reference itself is not owned.
		{"reference_excluded", "fn subject(p: &mut int) { *p = 2; }", "sole", 0, 0, 1},
		{"fixed_width_excluded", "fn subject(p: int64) {}", "sole", 0, 0, 0},
		{"owned_composite_preserved", "@copy type Cell = { value: int };\nfn subject(p: Cell) {}", "sole", 1, 0, 0},
		{"async_owner_preserved", "type Task<T> = { __opaque: int };\nasync fn subject(p: int) -> int { return 0; }", "int", 1, 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("source_sha256=%x", sha256.Sum256([]byte(tc.source)))
			parseBag, semaBag, result := runSemaOnSnippetResult(t, tc.source)
			requireNoSemaErrors(t, parseBag, semaBag)
			if result == nil || result.TypeInterner == nil {
				t.Fatal("missing typed SEMA result")
			}
			p := borrowedObligationBinding(t, result, tc.binding)
			if got := borrowedObligationSites(t, result.ScopeEndDrops, p); got != tc.normal {
				t.Fatalf("normal cleanup sites for parameter %d = %d, want %d: %v", p, got, tc.normal, result.ScopeEndDrops)
			}
			if got := borrowedObligationSites(t, result.EarlyExitDrops, p); got != tc.early {
				t.Fatalf("early cleanup sites for parameter %d = %d, want %d: %v", p, got, tc.early, result.EarlyExitDrops)
			}
			old := 0
			for site, sym := range result.ReassignOldDrops {
				if !site.IsValid() || !sym.IsValid() {
					t.Fatal("reassignment cleanup has no actual expression/binding identity")
				}
				if sym == p {
					old++
				}
			}
			if old != tc.old {
				t.Fatalf("overwritten parameter sites = %d, want %d: %v", old, tc.old, result.ReassignOldDrops)
			}
			if tc.name == "loop_rebind" {
				// The two abrupt loop exits release their own inner string,
				// while the counted parameter remains live until function exit.
				inner := borrowedObligationBinding(t, result, "string")
				if len(result.EarlyExitDrops) != 2 || borrowedObligationSites(t, result.EarlyExitDrops, inner) != 2 {
					t.Fatalf("break/continue lost their inner owner: %v", result.EarlyExitDrops)
				}
			}
			if tc.binding == "sole" && result.TypeInterner.IsRefCounted(result.BindingTypes[p]) {
				t.Fatal("exclusion fixture unexpectedly became a counted scalar")
			}
		})
	}
}

func borrowedObligationBinding(t *testing.T, result *Result, kind string) symbols.SymbolID {
	t.Helper()
	found := symbols.NoSymbolID
	for sym, ty := range result.BindingTypes {
		matches := kind == "sole" || kind == "int" && ty == result.TypeInterner.Builtins().Int ||
			kind == "string" && ty == result.TypeInterner.Builtins().String ||
			kind == "generic" && types.ContainsGenericParam(result.TypeInterner, ty)
		if !matches {
			continue
		}
		if !sym.IsValid() || ty == types.NoTypeID || found.IsValid() {
			t.Fatalf("fixture requires one exact %s binding, got %v", kind, result.BindingTypes)
		}
		found = sym
	}
	if !found.IsValid() {
		t.Fatalf("fixture has no %s binding: %v", kind, result.BindingTypes)
	}
	return found
}

func borrowedObligationSites(t *testing.T, sites map[ast.StmtID][]symbols.SymbolID, sym symbols.SymbolID) int {
	t.Helper()
	count := 0
	for site, drops := range sites {
		if !slices.Contains(drops, sym) {
			continue
		}
		if !site.IsValid() || !slices.Equal(drops, []symbols.SymbolID{sym}) {
			t.Fatalf("cleanup site %d must name only exact binding %d: %v", site, sym, drops)
		}
		count++
	}
	return count
}
