package symbols

import (
	"testing"

	"surge/internal/ast"
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
	if _, ok := res.Declare(name, source.Span{File: file}, SymbolLet, 0, SymbolDecl{
		SourceFile: file,
	}); !ok {
		t.Fatalf("declare returned false")
	}

	if err := table.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	res.Leave(scope)

	if err := table.Validate(); err != nil {
		t.Fatalf("validate after leave: %v", err)
	}
}
