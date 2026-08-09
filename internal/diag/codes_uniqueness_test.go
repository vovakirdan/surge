package diag

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestEveryDiagnosticCodeNumberIsClaimedOnce reads the code constants out of the
// package's own sources instead of out of codeDescription, because a duplicate
// number is invisible to anything the running program can look at: two names
// with the same value are the same map key, so the table holds one entry and
// the collision leaves no trace.
//
// The number is not decoration. It is what the author reads in `SEM3187`, what
// Title() looks a description up by, and what fix.MakeFixID derives a quick-fix
// identity from. A second claimant therefore answers for the first at all three
// places at once, and which of them wins depends on package initialisation
// order rather than on anything either author wrote.
//
// The codes are spread over one file per subject area, which is what makes this
// reachable: a lane adding a file cannot see the numbers another lane is taking
// in a file that does not exist on its branch yet.
func TestEveryDiagnosticCodeNumberIsClaimedOnce(t *testing.T) {
	claims := readCodeClaims(t, ".")
	if len(claims) == 0 {
		t.Fatal("no diagnostic code constants found; the reader stopped matching the declarations it is supposed to guard")
	}

	byNumber := make(map[int64][]codeClaim, len(claims))
	for _, c := range claims {
		byNumber[c.number] = append(byNumber[c.number], c)
	}

	numbers := make([]int64, 0, len(byNumber))
	for n, cs := range byNumber {
		if len(cs) > 1 {
			numbers = append(numbers, n)
		}
	}
	if len(numbers) == 0 {
		return
	}
	sort.Slice(numbers, func(i, j int) bool { return numbers[i] < numbers[j] })

	var b strings.Builder
	fmt.Fprintf(&b, "%d diagnostic number(s) claimed more than once:\n", len(numbers))
	for _, n := range numbers {
		cs := byNumber[n]
		sort.Slice(cs, func(i, j int) bool { return cs[i].position < cs[j].position })
		fmt.Fprintf(&b, "  %d is claimed by:\n", n)
		for _, c := range cs {
			fmt.Fprintf(&b, "    %s at %s\n", c.name, c.position)
		}
	}
	b.WriteString("give each of them its own number; the highest number in use is ")
	fmt.Fprintf(&b, "%d", highestClaim(claims))
	t.Fatal(b.String())
}

// TestEveryDiagnosticCodeHasADescriptionEntry is what makes the compiler the
// real guard rather than this file. Duplicate constant keys in a map literal
// are a compile error, so codeDescription refuses two codes with one number —
// but only for codes it lists. A constant with no entry is invisible to that
// check, and a whole subject area used to be invisible at once, because the
// sibling files merged their descriptions in with init() instead of writing
// them into the literal. Requiring an entry per code is what keeps the literal
// exhaustive, and an exhaustive literal is what makes a collision unbuildable.
func TestEveryDiagnosticCodeHasADescriptionEntry(t *testing.T) {
	claims := readCodeClaims(t, ".")
	described := readDescribedCodeNames(t, filepath.Join(".", "codes.go"))
	if len(described) == 0 {
		t.Fatal("no codeDescription entries found; the reader stopped matching the table it is supposed to guard")
	}

	var missing []codeClaim
	for _, c := range claims {
		if _, ok := described[c.name]; !ok {
			missing = append(missing, c)
		}
	}
	if len(missing) == 0 {
		return
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].number < missing[j].number })

	var b strings.Builder
	fmt.Fprintf(&b, "%d diagnostic code(s) carry no codeDescription entry, so nothing would refuse a second claimant of their number:\n", len(missing))
	for _, c := range missing {
		fmt.Fprintf(&b, "    %s = %d at %s\n", c.name, c.number, c.position)
	}
	b.WriteString("add each one to the codeDescription literal in codes.go; do not merge it in from another file with init()")
	t.Fatal(b.String())
}

// readDescribedCodeNames returns the constant names used as keys of the
// codeDescription literal.
func readDescribedCodeNames(t *testing.T, path string) map[string]struct{} {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	names := make(map[string]struct{})
	ast.Inspect(file, func(n ast.Node) bool {
		value, ok := n.(*ast.ValueSpec)
		if !ok || len(value.Names) != 1 || value.Names[0].Name != "codeDescription" || len(value.Values) != 1 {
			return true
		}
		lit, ok := value.Values[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range lit.Elts {
			entry, isEntry := elt.(*ast.KeyValueExpr)
			if !isEntry {
				continue
			}
			if key, isName := entry.Key.(*ast.Ident); isName {
				names[key.Name] = struct{}{}
			}
		}
		return false
	})
	return names
}

type codeClaim struct {
	name     string
	number   int64
	position string
}

func highestClaim(claims []codeClaim) int64 {
	var highest int64
	for _, c := range claims {
		if c.number > highest {
			highest = c.number
		}
	}
	return highest
}

// readCodeClaims collects every `Name Code = <int>` constant declared in the
// non-test sources of dir. It reads the syntax rather than the values because
// the values are exactly what collides.
func readCodeClaims(t *testing.T, dir string) []codeClaim {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fset := token.NewFileSet()
	var claims []codeClaim
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		claims = append(claims, codeClaimsInFile(fset, file)...)
	}
	return claims
}

func codeClaimsInFile(fset *token.FileSet, file *ast.File) []codeClaim {
	var claims []codeClaim
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			named, isNamed := value.Type.(*ast.Ident)
			if !isNamed || named.Name != "Code" {
				continue
			}
			if len(value.Names) != 1 || len(value.Values) != 1 {
				continue
			}
			lit, ok := value.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.INT {
				continue
			}
			number, err := strconv.ParseInt(lit.Value, 0, 64)
			if err != nil {
				continue
			}
			claims = append(claims, codeClaim{
				name:     value.Names[0].Name,
				number:   number,
				position: fset.Position(value.Names[0].Pos()).String(),
			})
		}
	}
	return claims
}
