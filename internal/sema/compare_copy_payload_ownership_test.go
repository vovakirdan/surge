package sema

import (
	"context"
	"fmt"
	"testing"

	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestCompareCopyPayloadArmKeepsDrop(t *testing.T) {
	for _, tt := range []struct {
		name, declaration, payload string
		wantDrop, copy, composite  bool
	}{
		{"plain", "@copy type Packet = { value: int };", "Packet", true, true, true},
		{"alias_chain", "@copy type Packet = { value: int }; type Alias1 = Packet; type Alias2 = Alias1;", "Alias2", true, true, true},
		{"generic_instance", "@copy type Pair<T> = (T, T);", "Pair<int>", true, true, true},
		{"move_only_control", "type Packet = { value: string };", "Packet", false, false, true},
		{"counted_control", "", "int", true, true, false},
		{"reference_control", "", "&int64", false, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := fmt.Sprintf(compareCopyPayloadSemaSource, tt.declaration, tt.payload, tt.payload, tt.payload)
			builder, file, parseBag := parseSnippet(t, src)
			bag := diag.NewBag(32)
			requireNoSemaErrors(t, parseBag, bag)
			resolved := symbols.ResolveFile(builder, file, &symbols.ResolveOptions{
				Reporter: &diag.BagReporter{Bag: bag},
			})
			res := Check(context.Background(), builder, file, Options{
				Reporter: &diag.BagReporter{Bag: bag}, Symbols: &resolved,
			})
			requireNoSemaErrors(t, parseBag, bag)

			// Resolve the actual pattern symbol. The fallback may have its own
			// branch obligations, which cannot substitute for the payload's.
			binding := symbols.NoSymbolID
			name := builder.StringsInterner.Intern("value")
			for expr, sym := range resolved.ExprSymbols {
				ident, ok := builder.Exprs.Ident(expr)
				if !ok || ident == nil || ident.Name != name {
					continue
				}
				if binding.IsValid() && binding != sym {
					t.Fatal("value resolves to multiple symbols")
				}
				binding = sym
			}
			if !binding.IsValid() {
				t.Fatal("payload binding did not reach sema")
			}
			ty := res.BindingTypes[binding]
			if ty == types.NoTypeID || res.TypeInterner == nil {
				t.Fatal("payload binding has no resolved type")
			}
			if res.IsCopyType(ty) != tt.copy || res.TypeInterner.IsValueComposite(ty) != tt.composite {
				t.Fatalf("unexpected payload type axes: Copy=%v composite=%v", res.IsCopyType(ty), res.TypeInterner.IsValueComposite(ty))
			}
			if res.OwnsHeap(ty) != (tt.name != "reference_control") {
				t.Fatalf("unexpected payload heap ownership: %v", res.OwnsHeap(ty))
			}

			drops := 0
			for _, arm := range res.ArmDropsExpr {
				for _, sym := range arm {
					if sym == binding {
						drops++
					}
				}
			}
			want := 0
			if tt.wantDrop {
				want = 1
			}
			if drops != want {
				t.Fatalf("payload binding arm drops = %d, want %d; a copied arm result keeps the binding's owner", drops, want)
			}
		})
	}
}

const compareCopyPayloadSemaSource = `
tag Payload<T>(T);
tag Empty();
type Outcome<T> = Payload(T) | Empty();
%s

fn take(input: Outcome<%s>, fallback: %s) -> %s {
    return compare input {
        Payload(value) => value;
        Empty() => fallback;
    };
}
`
