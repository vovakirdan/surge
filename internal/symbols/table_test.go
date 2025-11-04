package symbols

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
)

func TestTableFileRootReuse(t *testing.T) {
	table := NewTable(Hints{}, nil)
	file := source.FileID(1)
	span := source.Span{File: file}

	first := table.FileRoot(file, span)
	second := table.FileRoot(file, span)

	if !first.IsValid() {
		t.Fatalf("expected valid scope ID")
	}
	if first != second {
		t.Fatalf("expected FileRoot to reuse existing scope, got %v and %v", first, second)
	}

	if err := table.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestResolverLifecycle(t *testing.T) {
	table := NewTable(Hints{}, nil)
	file := source.FileID(10)
	root := table.FileRoot(file, source.Span{File: file})

	res := NewResolver(table, root, ResolverOptions{})
	scope := res.Enter(ScopeFunction, ScopeOwner{
		Kind:       ScopeOwnerItem,
		SourceFile: file,
		Item:       ast.ItemID(42),
	}, source.Span{File: file})

	name := table.Strings.Intern("value")
	id, ok := res.Declare(name, source.Span{File: file}, SymbolLet, 0, SymbolDecl{
		SourceFile: file,
	})
	if !ok {
		t.Fatalf("declare returned false")
	}

	if got, ok := res.Lookup(name); !ok || got != id {
		t.Fatalf("lookup mismatch: got %v, ok=%v", got, ok)
	}

	if err := table.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	res.Leave(scope)

	if err := table.Validate(); err != nil {
		t.Fatalf("validate after leave: %v", err)
	}
}

func TestResolverDuplicateDiagnostics(t *testing.T) {
	table := NewTable(Hints{}, nil)
	file := source.FileID(20)
	root := table.FileRoot(file, source.Span{File: file})
	bag := diag.NewBag(4)
	res := NewResolver(table, root, ResolverOptions{Reporter: &diag.BagReporter{Bag: bag}})

	name := table.Strings.Intern("dupe")
	span1 := source.Span{File: file, Start: 1, End: 2}
	span2 := source.Span{File: file, Start: 5, End: 6}

	if _, ok := res.Declare(name, span1, SymbolLet, 0, SymbolDecl{SourceFile: file}); !ok {
		t.Fatalf("first declaration rejected")
	}

	if _, ok := res.Declare(name, span2, SymbolLet, 0, SymbolDecl{SourceFile: file}); ok {
		t.Fatalf("expected duplicate declaration to be rejected")
	}

	if bag.Len() != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", bag.Len())
	}
	if bag.Items()[0].Code != diag.SemaDuplicateSymbol {
		t.Fatalf("unexpected code: %v", bag.Items()[0].Code)
	}
}

func TestResolverFunctionOverloadsAllowed(t *testing.T) {
	table := NewTable(Hints{}, nil)
	file := source.FileID(30)
	root := table.FileRoot(file, source.Span{File: file})
	res := NewResolver(table, root, ResolverOptions{})

	name := table.Strings.Intern("overload")
	if _, ok := res.Declare(name, source.Span{File: file, Start: 1, End: 2}, SymbolFunction, 0, SymbolDecl{SourceFile: file}); !ok {
		t.Fatalf("first function overload rejected")
	}
	if _, ok := res.Declare(name, source.Span{File: file, Start: 3, End: 4}, SymbolFunction, 0, SymbolDecl{SourceFile: file}); !ok {
		t.Fatalf("second function overload rejected")
	}

	candidates := res.LookupAll(name, SymbolFunction.Mask())
	if len(candidates) != 2 {
		t.Fatalf("expected 2 overloads, got %d", len(candidates))
	}
	if err := table.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestResolverScopeMismatchWarning(t *testing.T) {
	table := NewTable(Hints{}, nil)
	file := source.FileID(40)
	root := table.FileRoot(file, source.Span{File: file})
	bag := diag.NewBag(4)
	res := NewResolver(table, root, ResolverOptions{Reporter: &diag.BagReporter{Bag: bag}})

	res.Enter(ScopeBlock, ScopeOwner{SourceFile: file}, source.Span{File: file, Start: 10, End: 20})
	res.Leave(root) // mismatch on purpose

	if cur := res.CurrentScope(); cur != root {
		t.Fatalf("expected current scope to be root after mismatch, got %v", cur)
	}
	if bag.Len() != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", bag.Len())
	}
	d := bag.Items()[0]
	if d.Code != diag.SemaScopeMismatch {
		t.Fatalf("unexpected code: %v", d.Code)
	}
	if d.Primary != (source.Span{File: file, Start: 10, End: 20}) {
		t.Fatalf("unexpected primary span: %+v", d.Primary)
	}
	if err := table.Validate(); err != nil {
		t.Fatalf("validate after mismatch: %v", err)
	}
}
