package sema

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

// A join written where some path to the next statement does not evaluate it --
// the right operand of `&&`/`||`, or a `for` post clause, which has not run when
// the body first does -- releases nothing for that path. The write below it
// must therefore still see the child's pin. No clone is involved: this pins
// the pin lattice itself.
func TestTaskBorrowPinSurvivesASkippedJoin(t *testing.T) {
	rows := []struct {
		name   string
		body   string
		errors map[string]int
		write  int // offset of `l = 6` in body
		borrow int // offset of `&l` in body, where the pin note points
	}{
		// The `return 0` also leaves the child running on the skipped path.
		{"W1_and_right_operand",
			`async fn f(cond: bool) -> int { let mut l: int = 5; let t = spawn worker(&l); let _ = cond && done(t.await()); l = 6; return 0; }`,
			map[string]int{"SEM3019": 1, "SEM3021": 1}, 111, 73},
		{"W2_for_post_clause",
			`async fn f(n: int) -> int { let mut l: int = 5; let t = spawn worker(&l); for (let mut i: int = 0; i < n; i = step(i, t.clone().await())) { l = 6; } let _ = t.await(); return 0; }`,
			map[string]int{"SEM3019": 1}, 140, 69},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			c := checkTaskCloneBorrow(t, row.body)
			if got := c.errorCodes(); !equalCodeCounts(got, row.errors) {
				t.Fatalf("errors: got %v, want %v", got, row.errors)
			}
			if !strings.HasPrefix(row.body[row.write:], "l = 6") || row.body[row.borrow:row.borrow+2] != "&l" {
				t.Fatalf("row offsets do not name `l = 6` and `&l`")
			}
			for _, d := range c.diags {
				if d.Code != diag.SemaBorrowMutation {
					continue
				}
				if int(d.Primary.Start) != c.base+row.write {
					t.Fatalf("SEM3019 at %d, want %d", int(d.Primary.Start)-c.base, row.write)
				}
				if len(d.Notes) == 0 || int(d.Notes[0].Span.Start) != c.base+row.borrow {
					t.Fatalf("SEM3019 note does not point at the spawn's `&l`: %v", d.Notes)
				}
				return
			}
			t.Fatalf("no SEM3019 reported")
		})
	}
}
