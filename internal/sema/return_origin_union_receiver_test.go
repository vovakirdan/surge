package sema

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"

	"surge/internal/symbols"
	"surge/internal/types"
)

// A generic union keeps its declared parameters on its type symbol, so a method
// in its extern block proves T from that symbol, as a struct receiver does from
// its struct info. A foreign union or a receiver that is not the template prefix
// proves nothing.
const returnOriginUnionReceiverSource = `pragma no_std;
tag Some<T>(T);
type Maybe<T> = Some(T) | nothing;
tag Wrap<T>(T);
type Other<T> = Wrap(T) | nothing;
type Box<T> = { v: T };
extern<Maybe<T>> {
    fn keep(self: Maybe<T>, fallback: T) -> T {
        return compare self { Some(v) => v; nothing => fallback; };
    }
    fn pick<U>(self: Maybe<T>, fallback: T, extra: U) -> T {
        return compare self { Some(v) => v; nothing => fallback; };
    }
}
extern<Box<T>> {
    fn same(self: Box<T>, fallback: T) -> T { return fallback; }
}
fn probe(m: Maybe<&string>, b: &string) -> &string {
    return m.keep(b);
}
`

func TestReturnOriginUnionReceiverAuthority(t *testing.T) {
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(returnOriginUnionReceiverSource))); got != "bc7de0e66d2cf512acdfb2cc87aba64bff2355929eb0f4c30714cb303819edc1" {
		t.Fatalf("PRECONDITION: frozen union receiver source changed: %s", got)
	}
	a := returnOriginConditionAnalyzer(t, returnOriginUnionReceiverSource)
	functions := make(map[string]*returnOriginFunction)
	for _, fn := range a.functions {
		functions[fn.name] = fn
	}
	keep, pick, same, probe := functions["keep"], functions["pick"], functions["same"], functions["probe"]
	if keep == nil || pick == nil || same == nil || probe == nil || len(a.report.Diagnostics) != 0 {
		t.Fatalf("PRECONDITION: admitted bodies are missing or diagnosed: %+v", a.report.Diagnostics)
	}
	settled := func(t *testing.T, fn *returnOriginFunction) {
		t.Helper()
		for _, p := range a.report.Pending {
			if p.Span.File == fn.item.Span.File && p.Span.Start >= fn.item.Span.Start && p.Span.End <= fn.item.Span.End {
				t.Errorf("%s left unfinished: %s at %d:%d", fn.name, p.Reason, p.Span.Start, p.Span.End)
			}
		}
	}
	slot := func(t *testing.T, fn *returnOriginFunction, index int, want bool) {
		t.Helper()
		if len(fn.candidate.TemplateParams) <= index {
			t.Fatalf("PRECONDITION: %s has no template parameter %d", fn.name, index)
		}
		if got, valid := fn.templateSlot(fn.candidate.TemplateParams[index]); valid != want || valid && got != index {
			t.Errorf("%s template slot %d = (%d, %v), want valid=%v", fn.name, index, got, valid, want)
		}
	}
	// A control rewrites the real receiver in place and restores it afterwards.
	receiver := func(t *testing.T, id types.TypeID) {
		t.Helper()
		old := keep.candidate.ReceiverType
		keep.candidate.ReceiverType = id
		t.Cleanup(func() { keep.candidate.ReceiverType = old })
	}
	t.Run("generic_body", func(t *testing.T) {
		settled(t, keep)
		slot(t, keep, 0, true)
	})
	t.Run("own_generic_method", func(t *testing.T) {
		if len(pick.candidate.TemplateParams) != 2 || pick.candidate.ReceiverTemplateArity != 1 {
			t.Fatalf("PRECONDITION: pick is not a receiver T plus its own U: %+v", pick.candidate)
		}
		settled(t, pick)
		slot(t, pick, 0, true)
		slot(t, pick, 1, true)
	})
	t.Run("caller", func(t *testing.T) {
		settled(t, probe)
		found := false
		for _, summary := range a.report.Summaries {
			if summary.BodyKey == probe.key {
				found = true
				if !slices.Equal(summary.ParamSlots, []uint32{0, 1}) || summary.Unknown || summary.NoNormalReturn {
					t.Errorf("probe summary = %+v, want param slots [0 1]", summary)
				}
			}
		}
		if !found {
			t.Error("probe has no published summary")
		}
	})
	t.Run("struct_receiver_control", func(t *testing.T) {
		settled(t, same)
		slot(t, same, 0, true)
	})
	t.Run("receiver_args_not_prefix_control", func(t *testing.T) {
		concrete := probe.info.Params[0]
		if info, ok := a.units[0].Sema.TypeInterner.UnionInfo(concrete); !ok || info == nil || slices.Equal(info.TypeArgs, keep.candidate.TemplateParams) {
			t.Fatal("PRECONDITION: probe's Maybe<&string> is not a distinct union instance")
		}
		receiver(t, concrete)
		slot(t, keep, 0, false)
	})
	// Interning Other<T> outlives the cleanup, so this control runs last.
	t.Run("foreign_union_same_args_control", func(t *testing.T) {
		u := a.units[0]
		var other *types.UnionInfo
		for _, sym := range u.Symbols.Table.Symbols.Data() {
			if name, _ := u.Builder.StringsInterner.Lookup(sym.Name); sym.Kind == symbols.SymbolType && name == "Other" {
				other, _ = u.Sema.TypeInterner.UnionInfo(sym.Type)
			}
		}
		if other == nil {
			t.Fatal("PRECONDITION: Other has no union info")
		}
		receiver(t, u.Sema.TypeInterner.RegisterUnionInstance(other.Name, other.Decl, keep.candidate.TemplateParams))
		slot(t, keep, 0, false)
	})
}
