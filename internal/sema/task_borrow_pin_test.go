package sema

import (
	"context"
	"sort"
	"testing"

	"surge/internal/diag"
	"surge/internal/symbols"
)

// stableActivationPlaceNames runs parse + sema over the placement prelude plus
// src and returns, per callable name, the bindings that callable must keep at a
// fixed address. Names rather than ids, so the assertion reads as the question it
// asks. It reuses onCrossingPrelude for the same reason that file gives: these
// are unit tests and must not depend on the real stdlib prelude.
func stableActivationPlaceNames(t *testing.T, src string) map[string][]string {
	t.Helper()
	builder, fileID, parseBag := parseSource(t, onCrossingPrelude+src)
	if parseBag.Len() != 0 {
		for _, d := range parseBag.Items() {
			t.Logf("parse diag: %v %s", d.Code, d.Message)
		}
		t.Fatalf("snippet did not parse cleanly")
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(64)
	result := Check(context.Background(), builder, fileID, Options{
		Reporter:   &diag.BagReporter{Bag: semaBag},
		Symbols:    symRes,
		ModulePath: builder.StringsInterner.Intern("core"),
	})

	out := make(map[string][]string, len(result.StableActivationPlaces))
	for owner, places := range result.StableActivationPlaces {
		names := make([]string, 0, len(places))
		for _, place := range places {
			names = append(names, stableTestSymbolName(symRes, place))
		}
		sort.Strings(names)
		out[stableTestSymbolName(symRes, owner)] = names
	}
	return out
}

func stableTestSymbolName(symRes *symbols.Result, id symbols.SymbolID) string {
	if symRes == nil || symRes.Table == nil || symRes.Table.Symbols == nil || symRes.Table.Strings == nil {
		return ""
	}
	sym := symRes.Table.Symbols.Get(id)
	if sym == nil {
		return ""
	}
	return symRes.Table.Strings.MustLookup(sym.Name)
}

// The analysis must name the borrowed place and nothing else: a spawn that
// captures nothing borrowed constrains no storage, a place borrowed by two
// children is named once, and two borrowed places are both named. It is asked of
// the SPAWN, not of the join, because the capture set is what decides which
// storage may not move.
func TestStableActivationPlacesNameOnlyBorrowedCaptures(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want map[string][]string
	}{
		{
			name: "inline borrow in the spawn argument",
			src: `
async fn worker(x: &int) -> int {
	return *x + 1;
}

fn host() {
	let mut v: int = 0;
	let t = spawn worker(&v);
}
`,
			want: map[string][]string{"host": {"v"}},
		},
		{
			name: "a spawn that borrows nothing constrains nothing",
			src: `
async fn plain(x: int) -> int {
	return x + 1;
}

fn host() {
	let v: int = 0;
	let t = spawn plain(v);
}
`,
			want: map[string][]string{},
		},
		{
			name: "two children borrowing the same place name it once",
			src: `
async fn worker(x: &int) -> int {
	return *x + 1;
}

fn host() {
	let mut v: int = 0;
	let a = spawn worker(&v);
	let b = spawn worker(&v);
}
`,
			want: map[string][]string{"host": {"v"}},
		},
		{
			name: "two borrowed places are both named",
			src: `
async fn worker2(x: &int, y: &int) -> int {
	return *x + *y;
}

fn host() {
	let mut v: int = 0;
	let mut w: int = 0;
	let t = spawn worker2(&v, &w);
}
`,
			want: map[string][]string{"host": {"v", "w"}},
		},
		{
			name: "the constraint is per callable, not per file",
			src: `
async fn worker(x: &int) -> int {
	return *x + 1;
}

fn borrower() {
	let mut v: int = 0;
	let t = spawn worker(&v);
}

fn bystander() {
	let mut v: int = 0;
	v = 1;
}
`,
			want: map[string][]string{"borrower": {"v"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stableActivationPlaceNames(t, tc.src)
			if len(got) != len(tc.want) {
				t.Fatalf("callables constrained: got %v, want %v", got, tc.want)
			}
			for owner, wantNames := range tc.want {
				gotNames, ok := got[owner]
				if !ok {
					t.Fatalf("%q constrains nothing; got %v", owner, got)
				}
				if len(gotNames) != len(wantNames) {
					t.Fatalf("%q: got %v, want %v", owner, gotNames, wantNames)
				}
				for i := range wantNames {
					if gotNames[i] != wantNames[i] {
						t.Fatalf("%q: got %v, want %v", owner, gotNames, wantNames)
					}
				}
			}
		})
	}
}
