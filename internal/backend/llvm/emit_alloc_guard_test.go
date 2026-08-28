package llvm

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The allocation-refusal guard, proven at three levels.
//
// A defect that survives this lane is a NEW emitted allocation with no test in
// front of it, so the first stand below is a census of the emitter's own source
// rather than of one program's output: a program-driven check only ever sees the
// sites that program reaches.

// allocEmittersOutsideTheGuard are the emissions that write
// `call ptr @rt_alloc` themselves, with the reason each is not a hole.
//
// Anything else that writes one is a hole by construction, which is what
// TestEveryEmittedAllocationGoesThroughTheRefusalTest refuses.
var allocEmittersOutsideTheGuard = map[string]string{
	// The user's own rt_alloc call. Its nullable answer is the language's, not
	// the emitter's: section 5 of the storage-model contract keeps rt_alloc's
	// nullable C ABI, and a program that calls it is holding a *byte it must
	// test itself. Panicking here would take that answer away.
	"emit_intrinsics_memory.go": "the rt_alloc intrinsic hands the program the allocator's own answer",
	// The async ref-parameter box writes its own test and traps. It stops the
	// process rather than faulting, which is the half this lane is about; it
	// reports nothing, which is the half it does not fix, because the stand that
	// pins it belongs to the async lane.
	"emit_func.go": "the async ref box tests its own allocation and traps",
}

func emitterSourceFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the emitter package: %v", err)
	}
	var out []string
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		out = append(out, name)
	}
	return out
}

// TestEveryEmittedAllocationGoesThroughTheRefusalTest is the census.
//
// Before this lane, seven emitters wrote `call ptr @rt_alloc` and tested
// nothing; a refused allocation was a store through NULL. The fix is only worth
// as much as the guarantee that the next emitter cannot reopen it, and that
// guarantee is here rather than in a program: a program proves the sites it
// reaches, this proves the sites that exist.
func TestEveryEmittedAllocationGoesThroughTheRefusalTest(t *testing.T) {
	const guard = "emit_alloc_guard.go"
	for _, name := range emitterSourceFiles(t) {
		if name == guard {
			continue
		}
		raw, err := os.ReadFile(name) // #nosec G304 -- package-owned path
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(line, "call ptr @rt_alloc(") {
				continue
			}
			if _, excused := allocEmittersOutsideTheGuard[name]; excused {
				continue
			}
			t.Errorf("%s:%d writes an allocation the generated code never tests:\n  %s\n"+
				"  route it through emitCheckedAlloc, or record in allocEmittersOutsideTheGuard why "+
				"a refusal there is not a store through NULL",
				name, i+1, strings.TrimSpace(line))
		}
	}
}

// TestTheGuardedSiteRosterMatchesTheEmitterCallSites keeps the roster honest in
// both directions: a site the negative control can aim at but nothing emits, and
// a site something emits that the control cannot aim at, are both failures.
func TestTheGuardedSiteRosterMatchesTheEmitterCallSites(t *testing.T) {
	values := allocSiteConstantValues(t)
	used := map[allocSite]string{}
	callRe := regexp.MustCompile(`emitCheckedAlloc\(\s*(allocSite\w+)`)
	for _, name := range emitterSourceFiles(t) {
		raw, err := os.ReadFile(name) // #nosec G304 -- package-owned path
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, m := range callRe.FindAllStringSubmatch(string(raw), -1) {
			value, known := values[m[1]]
			if !known {
				t.Errorf("%s calls emitCheckedAlloc with %s, which is not a declared site", name, m[1])
				continue
			}
			used[value] = name
		}
	}
	roster := map[allocSite]bool{}
	for _, site := range allocGuardedSites() {
		roster[site] = true
		if _, ok := used[site]; !ok {
			t.Errorf("site %q is on the roster and nothing emits it; "+
				"the negative control has nothing to aim at", site)
		}
	}
	for site, where := range used {
		if !roster[site] {
			t.Errorf("%s emits site %q, which is not on the roster in allocGuardedSites", where, site)
		}
	}
}

// allocSiteConstantValues reads the site names out of their own declaration, so
// this file is not a second place the names are written down.
func allocSiteConstantValues(t *testing.T) map[string]allocSite {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "emit_alloc_guard.go", nil, 0)
	if err != nil {
		t.Fatalf("parse the guard: %v", err)
	}
	out := map[string]allocSite{}
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Values) != len(vs.Names) {
				continue
			}
			for i, name := range vs.Names {
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				out[name.Name] = allocSite(strings.Trim(lit.Value, `"`))
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no site constants were found; the declaration moved")
	}
	return out
}

const allocGuardArrayProgram = `@entrypoint
fn main() -> int {
    let a: int[] = [1, 2, 3];
    let mut total: int = 0;
    for x in a {
        total = total + x;
    }
    // Bound to a name first: a range walked straight out of its literal is
    // lowered without a Range object, and then nothing allocates a cursor.
    let r = 0..2;
    for i in r {
        total = total + i;
    }
    return total;
}
`

func emitAllocGuardProgram(t *testing.T) string {
	t.Helper()
	mirMod, result := lowerMIRFromSource(t, allocGuardArrayProgram)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	return ir
}

// TestAGuardedAllocationIsTestedAndReportsItsType reads the emitted shape: the
// allocation, the test against null, and a reporter that names the type.
//
// The message is checked as the bytes the module actually carries, because that
// is what a person reads on stderr; asserting the Go string would pass even if
// the constant were emitted at the wrong length.
func TestAGuardedAllocationIsTestedAndReportsItsType(t *testing.T) {
	ir := emitAllocGuardProgram(t)
	lines := strings.Split(ir, "\n")
	allocRe := regexp.MustCompile(`^\s*(%t\d+) = call ptr @rt_alloc\(`)
	tested := 0
	for i, line := range lines {
		m := allocRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		want := fmt.Sprintf("= icmp eq ptr %s, null", m[1])
		if i+1 >= len(lines) || !strings.Contains(lines[i+1], want) {
			t.Fatalf("the allocation at line %d is not tested; next line is %q", i+1, lines[i+1])
		}
		if i+4 >= len(lines) || !strings.Contains(lines[i+4], "call void @rt_panic(") {
			t.Fatalf("the refusal block for %s does not report; line is %q", m[1], lines[i+4])
		}
		if !strings.Contains(lines[i+5], "unreachable") {
			t.Fatalf("the refusal block for %s returns; line is %q", m[1], lines[i+5])
		}
		tested++
	}
	if tested == 0 {
		t.Fatal("the program emitted no allocation at all, so nothing was proven")
	}

	message := "out of memory: could not allocate Array<int>"
	lit := formatLLVMBytes([]byte(message), len(message))
	want := fmt.Sprintf("= private unnamed_addr constant [%d x i8] %s", len(message), lit)
	if !strings.Contains(ir, want) {
		t.Fatalf("no emitted constant carries %q", message)
	}
}

// TestTheRefusalMessageIsNotInTheTraceStringTable is the mistake this guard made
// on its first writing: the per-type sentence went into the lazily filled table
// that ALSO backs the backtrace maps, where the walker indexes rows by position.
// A message there is a row nothing names and the runtime can still reach.
func TestTheRefusalMessageIsNotInTheTraceStringTable(t *testing.T) {
	ir := emitAllocGuardProgram(t)
	table := ""
	for _, line := range strings.Split(ir, "\n") {
		if strings.HasPrefix(line, "@surge_trace_strings = ") {
			table = line
			break
		}
	}
	if table == "" {
		t.Fatal("the module emitted no trace string table")
	}
	if strings.Contains(table, "@.allocmsg.") {
		t.Fatalf("an allocation message is a row of the trace string table:\n%s", table)
	}
}

// TestTheNegativeControlAimsAtOneSite is the control's own control.
//
// A build flag that refused every allocation would prove only that the first one
// in a program is guarded, and one that refused none would make the stand green
// for the wrong reason. Both are checked here against the emitted text.
func TestTheNegativeControlAimsAtOneSite(t *testing.T) {
	if n := strings.Count(emitAllocGuardProgram(t), "i64 "+allocRefusalSize); n != 0 {
		t.Fatalf("an unarmed build already asks for the refusal size %d times", n)
	}
	for _, site := range []allocSite{allocSiteArrayElements, allocSiteArrayHeader, allocSiteArrayIter, allocSiteRangeIter} {
		t.Run(string(site), func(t *testing.T) {
			t.Setenv(allocRefusalEnvVar, string(site))
			ir := emitAllocGuardProgram(t)
			if n := strings.Count(ir, "call ptr @rt_alloc(i64 "+allocRefusalSize); n != 1 {
				t.Fatalf("arming %q made %d allocations refuse, want 1", site, n)
			}
		})
	}
}

// TestTheGuardIsWhereTheReportedFileSaysItIs keeps the ledger row that excuses
// this raise pointed at the raise. The panic-surface census keys its rows on
// file and function, and a row whose key has moved is reported as renumbered
// rather than as covered; this fails first, where the reason is legible.
func TestTheGuardIsWhereTheReportedFileSaysItIs(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("emit_alloc_guard.go")) // #nosec G304 -- package-owned path
	if err != nil {
		t.Fatalf("read the guard: %v", err)
	}
	if !strings.Contains(string(raw), "func (fe *funcEmitter) emitAllocRefusalPanic(") {
		t.Fatal("emitAllocRefusalPanic moved; update internal/panicgate/testdata/allowlist.json with it")
	}
}
