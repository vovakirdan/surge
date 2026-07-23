package mir

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Direct `range <x>.Funcs` walks are what made the compiler
// nondeterministic: Go randomises map iteration, and several passes walk
// the function map while ALLOCATING identifiers — async lowering interns
// state types, the LLVM backend assigns drop-function ids — so the ids
// depended on iteration order. The same input then produced different
// (still correct) output run to run, which defeats golden comparison, IR
// diffing, and bisecting a codegen regression.
//
// Module.SortedFuncIDs and MonoModule.SortedFuncKeys exist so every such
// walk has a stable order. This gate keeps them from being bypassed: the
// failure it prevents is silent and only shows up as an intermittent
// golden diff, which is easy to dismiss as flakiness.
var directFuncsRangeRE = regexp.MustCompile(`for\s+[^\n{]*:=\s*range\s+[\w.]+\.Funcs\b`)

func TestCompilerWalksFuncsInDeterministicOrder(t *testing.T) {
	roots := []string{".", filepath.Join("..", "backend", "llvm"), filepath.Join("..", "mono")}

	var offenders []string
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("read %s: %v", root, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(root, name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			currentFunc := ""
			for i, line := range strings.Split(string(data), "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "func ") {
					currentFunc = trimmed
				}
				if strings.HasPrefix(trimmed, "//") {
					continue
				}
				// The Sorted* helpers are the one place that must walk the
				// map: collecting its keys is exactly their job.
				if strings.Contains(currentFunc, ") Sorted") {
					continue
				}
				// hir.Module.Funcs is a SLICE, so walking it is already
				// ordered; only the map-backed Funcs fields matter here.
				if strings.Contains(line, ".mod.Funcs") || strings.Contains(line, "Source.Funcs") {
					continue
				}
				if directFuncsRangeRE.MatchString(line) {
					offenders = append(offenders, filepath.ToSlash(path)+":"+itoa(i+1)+": "+trimmed)
				}
			}
		}
	}

	if len(offenders) > 0 {
		t.Fatalf(
			"found %d direct map walk(s) over .Funcs; iterate SortedFuncIDs() (or SortedFuncKeys() for a MonoModule) "+
				"and index the map, so allocated identifiers do not depend on Go's randomised map order:\n\t%s",
			len(offenders), strings.Join(offenders, "\n\t"),
		)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
