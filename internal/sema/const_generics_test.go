package sema

import (
	"context"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
	"surge/internal/symbols"
)

func TestConstGenericArrayLengthUsesParam(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
        type Marker<const N:int> = { data: int[N]; }

        fn use(x: Marker<4>) {}
    `)

	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}
	if semaBag.Len() != 0 {
		t.Fatalf("unexpected semantic diagnostics: %d", semaBag.Len())
	}
}

func TestConstGenericIdentityByValue(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
        const SIZE = 3;

        type Maze<const N:int, const M:int> = {};

        fn use(maze: Maze<SIZE, SIZE>) {
            let other: Maze<3, 3> = maze;
        }
    `)

	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}
	if semaBag.Len() != 0 {
		t.Fatalf("unexpected semantic diagnostics: %d", semaBag.Len())
	}
}

func TestExternSeesReceiverGenerics(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
        extern<Array<T>> {
            fn len(self: Array<T>) -> int;
        }

        extern<ArrayFixed<T, N>> {
            fn first(self: ArrayFixed<T, N>) -> T;
        }
    `)

	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}
	if semaBag.Len() != 0 {
		t.Fatalf("unexpected semantic diagnostics: %d", semaBag.Len())
	}
}

func TestConstGenericRejectsNonConstArgs(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
        type Maze<const N:int> = {};

        fn bad(n: int) {
            let maze: Maze<n> = {};
        }
    `)

	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}
	if semaBag.Len() == 0 {
		t.Fatalf("expected semantic diagnostics for non-const argument")
	}
	if !bagContainsCode(semaBag, diag.SemaTypeMismatch) {
		t.Fatalf("expected type mismatch diagnostic, got %v", semaBag.Items())
	}
}

func runSemaOnSnippet(t *testing.T, src string) (*diag.Bag, *diag.Bag) {
	t.Helper()
	builder, fileID, parseBag := parseSnippet(t, src)
	semaBag := diag.NewBag(32)
	if parseBag.Len() != 0 {
		return parseBag, semaBag
	}
	symRes := symbols.ResolveFile(builder, fileID, &symbols.ResolveOptions{
		Reporter: &diag.BagReporter{Bag: semaBag},
	})
	Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  &symRes,
	})
	return parseBag, semaBag
}

func parseSnippet(t *testing.T, src string) (*ast.Builder, ast.FileID, *diag.Bag) {
	t.Helper()
	fs := source.NewFileSetWithBase("")
	fileID := fs.AddVirtual("snippet.sg", []byte(src))
	file := fs.Get(fileID)

	bag := diag.NewBag(32)

	lx := lexer.New(file, lexer.Options{})
	builder := ast.NewBuilder(ast.Hints{}, nil)

	opts := parser.Options{
		Reporter:  &diag.BagReporter{Bag: bag},
		MaxErrors: uint(bag.Cap()),
	}
	result := parser.ParseFile(context.Background(), fs, lx, builder, opts)

	return builder, result.File, bag
}

func bagContainsCode(bag *diag.Bag, code diag.Code) bool {
	for _, item := range bag.Items() {
		if item.Code == code {
			return true
		}
	}
	return false
}
