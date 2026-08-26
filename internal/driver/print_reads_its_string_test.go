package driver

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"surge/internal/diag"
)

// `print` READS its string (core/base.sg: `print(s: &string, ...)`). Owner
// ruling 2026-08-26 (the addendum to RV2-DEBT-258): once a `for` binding only
// reads its element, a by-value `print` turned `for name in names {
// print(name); }` into an SEM3205 refusal and the corpus grew `print(clone(
// name))`; printing must not take the string, and the call site must stay
// `print(name)` - no `&`, no clone.
//
// The rows compile against the REAL core (SURGE_STDLIB = the repository), not
// a prelude that declares its own `print`: a prelude would pass whatever
// core/base.sg says. The refused row proves the same harness reaches the
// for-in analysis, so a clean row is not clean because nothing ran.
func TestPrintReadsItsString(t *testing.T) {
	t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
	dir := t.TempDir()
	cases := []struct {
		name string
		src  string
		want []string // every error code the program must report; nil means clean
	}{
		{"for_binding_printed", `fn f(names: string[]) -> nothing { for name in names { print(name); } return nothing; }`, nil},
		{"for_binding_printed_with_end", `fn f(names: string[]) -> nothing { for name in names { print(name, ""); } return nothing; }`, nil},
		{"string_printed_twice", `fn f() -> nothing { let s = "x"; print(s); print(s); return nothing; }`, nil},
		{"literal_and_temporary", `fn f(n: int) -> nothing { print("Hello world!"); print("n=" + (n to string)); return nothing; }`, nil},
		// The control row: the same harness, the same loop, a callee that
		// takes the string by value - refused at the move.
		{"for_binding_given_away", `fn take(s: string) -> nothing { return nothing; } fn f(names: string[]) -> nothing { for name in names { take(name); } return nothing; }`, []string{"SEM3205"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".sg")
			if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			res, err := DiagnoseWithOptions(
				context.Background(),
				path,
				&DiagnoseOptions{Stage: DiagnoseStageAll, MaxDiagnostics: 64, NoAlienHints: true},
			)
			if err != nil {
				t.Fatalf("diagnose: %v", err)
			}
			if res == nil || res.Bag == nil || res.Sema == nil {
				t.Fatalf("diagnose produced no semantic authority: %+v", res)
			}
			got := make([]string, 0, res.Bag.Len())
			for _, d := range res.Bag.Items() {
				if d.Severity == diag.SevError {
					got = append(got, d.Code.ID())
				}
			}
			sort.Strings(got)
			if len(tc.want) == 0 {
				if len(got) != 0 {
					t.Fatalf("expected a clean program, got %s", driverDiagnosticsSummary(res.Bag))
				}
				return
			}
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("expected %v, got %s", want, driverDiagnosticsSummary(res.Bag))
			}
		})
	}
}
