package sema

import (
	"context"
	"testing"

	"surge/internal/diag"
	"surge/internal/symbols"
)

func TestFunctionCrossingEffectInference(t *testing.T) {
	src := onCrossingPrelude + `
fn direct_on() -> TaskResult<int> {
	return on pool { ret 1; };
}

fn direct_spawn_on() -> far Task<int> {
	return spawn on distributed { ret 2; };
}

fn direct_await(t: far Task<int>) -> TaskResult<int> {
	return t.await();
}

fn direct_cancel(t: far Task<int>) -> TaskResult<nothing> {
	return t.cancel();
}

fn local_only(x: int) -> int {
	return x;
}

fn far_type_only(t: far Task<int>) -> far Task<int> {
	return t;
}

fn wrapper() -> TaskResult<int> {
	return direct_on();
}

fn second_wrapper() -> TaskResult<int> {
	return wrapper();
}
`

	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(64)
	res := Check(context.Background(), builder, fileID, Options{
		Reporter:   &diag.BagReporter{Bag: semaBag},
		Symbols:    symRes,
		ModulePath: builder.StringsInterner.Intern("core"),
	})
	if semaBag.HasErrors() {
		t.Fatalf("sema diagnostics: %s", diagnosticsSummary(semaBag))
	}

	wantCross := []string{"direct_on", "direct_spawn_on", "direct_await", "direct_cancel", "wrapper", "second_wrapper"}
	for _, name := range wantCross {
		id := requireFunctionSymbolID(t, symRes, name)
		if !res.FunctionEffects[id].MayCross {
			t.Fatalf("%s: expected inferred MayCross", name)
		}
	}

	wantLocal := []string{"local_only", "far_type_only"}
	for _, name := range wantLocal {
		id := requireFunctionSymbolID(t, symRes, name)
		if res.FunctionEffects[id].MayCross {
			t.Fatalf("%s: did not expect inferred MayCross", name)
		}
	}
}

func requireFunctionSymbolID(t *testing.T, res *symbols.Result, name string) symbols.SymbolID {
	t.Helper()
	if res == nil || res.Table == nil || res.Table.Symbols == nil || res.Table.Strings == nil {
		t.Fatal("missing symbol result")
	}
	for i := 1; i <= res.Table.Symbols.Len(); i++ {
		id := symbols.SymbolID(i)
		sym := res.Table.Symbols.Get(id)
		if sym == nil || sym.Kind != symbols.SymbolFunction {
			continue
		}
		text, ok := res.Table.Strings.Lookup(sym.Name)
		if ok && text == name {
			return id
		}
	}
	t.Fatalf("missing function symbol %q", name)
	return symbols.NoSymbolID
}
