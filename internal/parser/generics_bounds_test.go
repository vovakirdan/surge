package parser

import (
	"testing"

	"surge/internal/ast"
)

func TestParseFnTypeParamBounds(t *testing.T) {
	src := `fn f<T: FooLike + Serializable<T>>(t: T) -> int;`
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}

	file := builder.Files.Get(fileID)
	if len(file.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(file.Items))
	}
	fnItem, ok := builder.Items.Fn(file.Items[0])
	if !ok {
		t.Fatalf("expected fn item, got %v", builder.Items.Get(file.Items[0]).Kind)
	}

	typeParamIDs := builder.Items.GetFnTypeParamIDs(fnItem)
	if len(typeParamIDs) != 1 {
		t.Fatalf("expected 1 type param, got %d", len(typeParamIDs))
	}
	tp := builder.Items.TypeParam(typeParamIDs[0])
	if tp == nil {
		t.Fatal("type param not found")
	}
	if tp.BoundsNum != 2 {
		t.Fatalf("expected 2 bounds, got %d", tp.BoundsNum)
	}

	first := builder.Items.TypeParamBound(tp.Bounds)
	if first == nil {
		t.Fatal("first bound missing")
	}
	if got := lookupNameOr(builder, first.Name, ""); got != "FooLike" {
		t.Fatalf("unexpected first bound name: %q", got)
	}
	if len(first.TypeArgs) != 0 {
		t.Fatalf("expected no type args on first bound, got %d", len(first.TypeArgs))
	}

	second := builder.Items.TypeParamBound(ast.TypeParamBoundID(uint32(tp.Bounds) + 1))
	if second == nil {
		t.Fatal("second bound missing")
	}
	if got := lookupNameOr(builder, second.Name, ""); got != "Serializable" {
		t.Fatalf("unexpected second bound name: %q", got)
	}
	if len(second.TypeArgs) != 1 {
		t.Fatalf("expected one type arg on second bound, got %d", len(second.TypeArgs))
	}
}

func TestParseTypeDeclBounds(t *testing.T) {
	src := `type List<T: Iterable<T>> = {};`
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
	file := builder.Files.Get(fileID)
	if len(file.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(file.Items))
	}
	typeItem, ok := builder.Items.Type(file.Items[0])
	if !ok {
		t.Fatalf("expected type item, got %v", builder.Items.Get(file.Items[0]).Kind)
	}
	params := builder.Items.GetTypeParamIDs(typeItem.TypeParamsStart, typeItem.TypeParamsCount)
	if len(params) != 1 {
		t.Fatalf("expected 1 type param, got %d", len(params))
	}
	tp := builder.Items.TypeParam(params[0])
	if tp == nil {
		t.Fatal("type param missing")
	}
	if tp.BoundsNum != 1 {
		t.Fatalf("expected 1 bound, got %d", tp.BoundsNum)
	}
	bound := builder.Items.TypeParamBound(tp.Bounds)
	if bound == nil {
		t.Fatal("bound missing")
	}
	if got := lookupNameOr(builder, bound.Name, ""); got != "Iterable" {
		t.Fatalf("unexpected bound name: %q", got)
	}
	if len(bound.TypeArgs) != 1 {
		t.Fatalf("expected 1 type arg, got %d", len(bound.TypeArgs))
	}
}

func TestParseMultipleTypeParamBounds(t *testing.T) {
	src := `fn k<T: X + Y<T> + Z<T, U>, U>();`
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}

	file := builder.Files.Get(fileID)
	fnItem, ok := builder.Items.Fn(file.Items[0])
	if !ok {
		t.Fatal("expected fn item")
	}
	params := builder.Items.GetFnTypeParamIDs(fnItem)
	if len(params) != 2 {
		t.Fatalf("expected 2 type params, got %d", len(params))
	}
	first := builder.Items.TypeParam(params[0])
	if first.BoundsNum != 3 {
		t.Fatalf("expected 3 bounds on first param, got %d", first.BoundsNum)
	}
	second := builder.Items.TypeParam(params[1])
	if second.BoundsNum != 0 {
		t.Fatalf("expected no bounds on second param, got %d", second.BoundsNum)
	}
}

func TestParseBoundsError(t *testing.T) {
	src := `fn bad<T: FooLike + >(t: T);`
	_, _, bag := parseSource(t, src)
	if !bag.HasErrors() {
		t.Fatal("expected diagnostics for malformed bounds")
	}
}
