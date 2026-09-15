package llvm

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The allocation-refusal guard, proven at three levels.
//
// The census below is the first, and its predicate is the lesson of the first
// writing: that census looked for the text `call ptr @rt_alloc(`, which does not
// match `call ptr @rt_realloc(`, so `a.push(7)` — an emitted call that answers
// NULL and is stored into the array header untested — passed it silently. A
// census that cannot see the defect it exists to prevent is worse than none,
// because it reports coverage.
//
// So the roster is not the emitters: it is builtins.go, the ABI declaration of
// every runtime entry point a module can call. Everything there that answers a
// pointer is classified here, and an entry point reached through the ordinary
// call path — which is written by no emitter and was therefore invisible to any
// scan of emitter text — is on it like every other.
//
// The second level reads the emitted IR and lives in emit_alloc_guard_ir_test.go,
// because text and IR answer different questions and neither can stand in for
// the other: text is where a hole in the roster shows, IR is where a hole in the
// LOWERING shows. The third is the end-to-end negative control in
// internal/vm/runtime_v2_alloc_refusal_test.go.

// untestedRuntimeAnswers is the pinned size of the open surface: entry points
// that answer NULL on refusal with nothing in front of them. It is written as a
// number so that adding one is a deliberate edit somebody reviews, and so that
// a lane which closes a family moves it DOWN and says which family.
//
// 31 on 2026-08-29: the filesystem result (17), the socket result (9), and five
// blocks that carry their own answer. The string family left this count on the
// same day it was measured, by reporting at the allocation instead.
//
// 0 on 2026-09-03 (Wave F, F2, RV2-DEBT-309): every one of the 31 reports at
// the allocation through rt_alloc_or_report / rt_tag_alloc_or_report, the way
// the string family already did, so generated code that stores their answer
// untested stores a block or nothing at all. A lane that moves this UP is
// adding an entry point that answers NULL untested, and must say why.
const untestedRuntimeAnswers = 0

// swallowedRuntimeAnswers pins the other open surface, which no test in the
// generated code can close because the refusal is not a null by the time it
// gets there. Pinned separately from the count above so that closing one family
// cannot be paid for out of the other.
//
// 25 on 2026-08-29: the tagged int arithmetic that promotes through bi_promote
// (13, counting rt_bigint_to_bigfloat), its uint twin through bu_promote (11),
// and rt_bigint_to_biguint's bu_clone. They were all recorded as reported until
// this count existed, on a reason that is true of the RESULT block and not of
// the promotion in front of it.
const swallowedRuntimeAnswers = 25

// TestEveryRuntimePointerAnswerIsClassified is the census.
//
// It runs over the ABI roster rather than over the emitters, because that is the
// only list a new runtime entry point cannot be added behind: a function the
// generated code can call is declared there or it does not link.
func TestEveryRuntimePointerAnswerIsClassified(t *testing.T) {
	answers := runtimePointerAnswers()
	declared := map[string]bool{}
	open := map[runtimeAnswerClass][]string{}
	for _, decl := range runtimeDecls() {
		if decl.ret != "ptr" {
			continue
		}
		declared[decl.name] = true
		answer, known := answers[decl.name]
		if !known {
			t.Errorf("%s answers a pointer and is not classified in runtimePointerAnswers; "+
				"say whether a refused allocation reaches the generated code as NULL, and what happens then",
				decl.name)
			continue
		}
		if answer.class == refusalIsUntested || answer.class == refusalIsSwallowed {
			open[answer.class] = append(open[answer.class], decl.name)
		}
	}
	for name := range answers {
		if !declared[name] {
			t.Errorf("%s is classified but is not a pointer-answering runtime declaration any more; "+
				"the classification has rotted", name)
		}
	}
	for _, pin := range []struct {
		class runtimeAnswerClass
		want  int
		what  string
	}{
		{refusalIsUntested, untestedRuntimeAnswers, "refused-and-untested"},
		{refusalIsSwallowed, swallowedRuntimeAnswers, "refused-and-answered-as-a-number"},
	} {
		got := open[pin.class]
		if len(got) != pin.want {
			sort.Strings(got)
			t.Errorf("%d runtime answers are %s, the pin says %d:\n  %s",
				len(got), pin.what, pin.want, strings.Join(got, "\n  "))
		}
	}
}

// allocEmittersOutsideTheGuard are the files that write a tested-class call
// themselves, with the reason each is not a hole.
var allocEmittersOutsideTheGuard = map[string]string{
	// The user's own rt_alloc call. Its nullable answer is the language's, not
	// the emitter's: section 5 of the storage-model contract keeps rt_alloc's
	// nullable C ABI, and a program that calls it is holding a *byte it must
	// test itself. Panicking here would take that answer away.
	"emit_intrinsics_memory.go": "the rt_alloc intrinsic hands the program the allocator's own answer",
}

// indirectPointerCallEmitters write a pointer-answering call whose callee is a
// parameter, so the name is not in the text this census reads. Each is recorded
// with the entry points it can name; all of them are classified above.
var indirectPointerCallEmitters = map[string]string{
	"emit_task_result.go":         "__task_create, __task_create_affine",
	"emit_intrinsics_fs.go":       "rt_fs_close, rt_fs_flush, rt_fs_file_name, rt_fs_file_type, rt_fs_file_metadata",
	"emit_intrinsics_net.go":      "rt_net_close_listener, rt_net_close_conn",
	"emit_intrinsics_runtime.go":  "rt_string_from_bytes, rt_string_from_utf16",
	"emit_numeric_fixnum_fast.go": "rt_bigint_add, rt_bigint_sub",
	"emit_iter_bounds_step.go": "rt_bigint_from_i64, rt_biguint_from_u64, rt_bigfloat_from_i64, " +
		"rt_bigint_add, rt_biguint_add, rt_bigfloat_add",
}

// genericCallPathEmitters write a call statement whose callee AND result type
// are both format operands, so no entry point is spelled in their text at all
// and the text census below can read NOTHING of them. That blindness is the
// gap, named here rather than left implicit: a census that cannot see a whole
// emission path reports coverage it does not have. What covers this path is
// runtimeAnswersTestedAtTheCallSite, and what keeps that map complete is
// TestATestedAnswerIsGuardedOnEveryPathThatReachesIt.
var genericCallPathEmitters = map[string]string{
	"emit_call_site.go": "emitCallSite lowers every call the language makes, runtime symbol or not; " +
		"emitRuntimeAnswerTest is the test it writes",
}

// allocGuardFile is where the tested calls are written, so it is the one file
// this census reads past.
const allocGuardFile = "emit_alloc_guard.go"

var emittedPointerCallRe = regexp.MustCompile(`call ptr @(rt_[a-z0-9_]+)\(`)

// pointerCallFindings reports what is wrong with one emitter source: a call to
// an unclassified entry point, or a tested-class call written somewhere the
// negative control and the guard cannot reach it.
//
// It takes the text rather than reading the file so the stand below can hand it
// the defect this census was written to catch and watch it answer.
func pointerCallFindings(name, source string) []string {
	if name == allocGuardFile {
		return nil
	}
	answers := runtimePointerAnswers()
	atTheCallSite := runtimeAnswersTestedAtTheCallSite()
	var out []string
	for i, line := range strings.Split(source, "\n") {
		if strings.Contains(line, `"call %s %s(`) {
			if _, recorded := genericCallPathEmitters[name]; !recorded {
				out = append(out, fmt.Sprintf("%s:%d writes the generic call statement, so this census "+
					"can read no entry point out of it at all; record it in genericCallPathEmitters and "+
					"say what tests the answers it can hand back", name, i+1))
			}
			continue
		}
		if strings.Contains(line, "call ptr @%s(") {
			if _, recorded := indirectPointerCallEmitters[name]; !recorded {
				out = append(out, fmt.Sprintf("%s:%d writes a pointer-answering call whose callee is a "+
					"parameter; record it in indirectPointerCallEmitters with the entry points it can name", name, i+1))
			}
			continue
		}
		m := emittedPointerCallRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		answer, known := answers[m[1]]
		if !known {
			out = append(out, fmt.Sprintf("%s:%d calls %s, which is not classified in runtimePointerAnswers",
				name, i+1, m[1]))
			continue
		}
		if answer.class != refusalIsTested || atTheCallSite[m[1]] {
			continue
		}
		if _, excused := allocEmittersOutsideTheGuard[name]; excused {
			continue
		}
		out = append(out, fmt.Sprintf("%s:%d writes an allocation the generated code never tests:\n  %s\n"+
			"  route it through the guard in emit_alloc_guard.go, or record in allocEmittersOutsideTheGuard "+
			"why a refusal there is not a store through NULL", name, i+1, strings.TrimSpace(line)))
	}
	return out
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

// TestEveryEmittedAllocationGoesThroughTheRefusalTest keeps the tested calls in
// the one file the guard and the negative control can both see.
func TestEveryEmittedAllocationGoesThroughTheRefusalTest(t *testing.T) {
	for _, name := range emitterSourceFiles(t) {
		raw, err := os.ReadFile(name) // #nosec G304 -- package-owned path
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, finding := range pointerCallFindings(name, string(raw)) {
			t.Error(finding)
		}
	}
}

// TestTheCensusSeesTheCallItOnceMissed breaks the census deliberately, because
// this one was green over two live holes for a whole review cycle. The text is
// the IR the committed tree emitted for `a.push(7)`.
func TestTheCensusSeesTheCallItOnceMissed(t *testing.T) {
	const pushed = "\tfmt.Fprintf(&fe.emitter.buf, \"  %s = call ptr @rt_realloc(ptr %s, i64 %s, i64 %s, i64 %d)\\n\")\n"
	findings := pointerCallFindings("emit_intrinsics_array.go", pushed)
	if len(findings) != 1 {
		t.Fatalf("the census answered %d findings for an untested rt_realloc, want 1: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "never tests") {
		t.Fatalf("the census reported %q, which does not say the answer is untested", findings[0])
	}
	if got := pointerCallFindings(allocGuardFile, pushed); len(got) != 0 {
		t.Fatalf("the guard's own file was reported: %v", got)
	}
}

// TestTheCensusSaysWhenItCanSeeNothingOfAFile is the second blindness, one
// layer along from the first. A file that writes the generic call statement
// spells no entry point at all, so the census reads nothing of it and reported
// nothing about it — silence that looked exactly like coverage. The text is the
// statement emit_call_site.go builds.
func TestTheCensusSaysWhenItCanSeeNothingOfAFile(t *testing.T) {
	const generic = "\tcallStmt := fmt.Sprintf(\"call %s %s(%s)\", lowered.ret, target.callee, args)\n"
	findings := pointerCallFindings("emit_some_new_call_path.go", generic)
	if len(findings) != 1 {
		t.Fatalf("the census answered %d findings for an unrecorded generic call emitter, want 1: %v",
			len(findings), findings)
	}
	if !strings.Contains(findings[0], "genericCallPathEmitters") {
		t.Fatalf("the census reported %q, which does not say where to record the path", findings[0])
	}
	if got := pointerCallFindings("emit_call_site.go", generic); len(got) != 0 {
		t.Fatalf("the recorded generic call emitter was reported: %v", got)
	}
}

var intrinsicCaseRe = regexp.MustCompile(`"(rt_[a-z0-9_]+)"`)

// intrinsicsInterceptedBeforeTheCallPath reads the dispatch: a runtime symbol
// named in a `case` of an intrinsic emitter is lowered by that emitter and never
// reaches emitCallSite. Read out of the switches themselves rather than listed
// here, so the two cannot drift.
func intrinsicsInterceptedBeforeTheCallPath(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, name := range emitterSourceFiles(t) {
		if !strings.HasPrefix(name, "emit_intrinsics_") {
			continue
		}
		raw, err := os.ReadFile(name) // #nosec G304 -- package-owned path
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "case ") {
				continue
			}
			for _, m := range intrinsicCaseRe.FindAllStringSubmatch(line, -1) {
				out[m[1]] = name
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no intrinsic dispatch was found; the switches moved and this check now excuses everything")
	}
	return out
}

// TestATestedAnswerIsGuardedOnEveryPathThatReachesIt is the check the census
// could not make, and its absence is how a constructor came to be tested on one
// of its two paths and called untested on the other.
//
// A runtime symbol the language can name arrives one of two ways: an intrinsic
// emitter claims it in a `case`, or it falls through to emitCallSite. The second
// path spells no name in any source text, so nothing in the emitters says which
// entry points travel it — the ABI roster is the only list, and every tested
// entry point on it that is NOT claimed by a `case` reaches the generic path.
// There it is tested by runtimeAnswersTestedAtTheCallSite or by nothing.
func TestATestedAnswerIsGuardedOnEveryPathThatReachesIt(t *testing.T) {
	intercepted := intrinsicsInterceptedBeforeTheCallPath(t)
	atTheCallSite := runtimeAnswersTestedAtTheCallSite()
	emitterOnly := emitterOnlyPointerAnswers()
	for name, answer := range runtimePointerAnswers() {
		if answer.class != refusalIsTested {
			continue
		}
		if _, only := emitterOnly[name]; only {
			if atTheCallSite[name] {
				t.Errorf("%s is listed as unnameable and also tested at the call site; "+
					"one of the two is dead, and a dead guard is a guard nobody notices losing", name)
			}
			if where, claimed := intercepted[name]; claimed {
				t.Errorf("%s is listed as unnameable and %s claims it in a case; "+
					"a symbol an intrinsic emitter lowers is one the language can name", name, where)
			}
			continue
		}
		if where, claimed := intercepted[name]; claimed {
			if atTheCallSite[name] {
				t.Errorf("%s is claimed by %s and also listed in runtimeAnswersTestedAtTheCallSite; "+
					"one of the two is dead, and a dead guard is a guard nobody notices losing", name, where)
			}
			continue
		}
		if !atTheCallSite[name] {
			t.Errorf("%s answers NULL on refusal, no intrinsic emitter claims it, and it is not in "+
				"runtimeAnswersTestedAtTheCallSite: every call to it goes through emitCallSite untested. "+
				"Add it there, or reclassify it", name)
		}
	}
	for name := range atTheCallSite {
		if runtimePointerAnswers()[name].class != refusalIsTested {
			t.Errorf("%s is tested at the call site and is not classified as tested; "+
				"the guard and the census disagree about it", name)
		}
	}
}

// emitterOnlyPointerAnswers are pointer-answering entry points the LANGUAGE
// cannot name.
//
// The two ways in TestATestedAnswerIsGuardedOnEveryPathThatReachesIt — an
// intrinsic emitter's `case`, or the generic call path — both start from a
// program writing the symbol's name, and a program can only write a name
// `core/intrinsics.sg` declares. An entry point that is not declared there is
// reached from one emitter and from nothing else, so the test beside that
// emitter's call is the whole surface.
//
// Membership is not taken on trust. TestAnEmitterOnlyAnswerIsNotCallable reads
// the intrinsic declarations and fails if one of these appears among them,
// because the day a symbol becomes callable is the day the generic call path
// can reach it untested.
func emitterOnlyPointerAnswers() map[string]string {
	return map[string]string{
		"rt_frame_alloc": "a suspension frame is reserved by emitFrameStorage and by nothing a program can write",
		"rt_range_bounds_new": "the operator spelling `a..b` is lowered by emitBinary, which is the only " +
			"caller that knows the bound KIND; the four names a program can write are the range-literal " +
			"spelling, whose bounds the type checker holds to `int`",
	}
}

// intrinsicDeclRe reads one `@intrinsic fn NAME(` declaration.
var intrinsicDeclRe = regexp.MustCompile(`@intrinsic\s+(?:pub\s+)?fn\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

// TestAnEmitterOnlyAnswerIsNotCallable checks the claim above against the
// language's own roster of runtime symbols.
func TestAnEmitterOnlyAnswerIsNotCallable(t *testing.T) {
	path := filepath.Join(repoRootFromLLVMTest(t), "core", "intrinsics.sg")
	raw, err := os.ReadFile(path) // #nosec G304 -- repository-owned path
	if err != nil {
		t.Fatalf("read the intrinsic roster: %v", err)
	}
	declared := map[string]bool{}
	for _, m := range intrinsicDeclRe.FindAllStringSubmatch(string(raw), -1) {
		declared[m[1]] = true
	}
	// A roster that stopped parsing would excuse every entry below, which is
	// the failure this whole file is written against.
	if !declared["rt_alloc"] {
		t.Fatalf("%s no longer declares rt_alloc; the roster moved and this check reads nothing", path)
	}
	for name, why := range emitterOnlyPointerAnswers() {
		if declared[name] {
			t.Errorf("%s is declared in %s, so a program can call it and the generic call path "+
				"reaches it untested; the reason recorded for it was %q", name, path, why)
		}
	}
}

// TestTheGuardedSiteRosterMatchesTheEmitterCallSites keeps the roster honest in
// both directions: a site the negative control can aim at but nothing emits, and
// a site something emits that the control cannot aim at, are both failures.
func TestTheGuardedSiteRosterMatchesTheEmitterCallSites(t *testing.T) {
	values := allocSiteConstantValues(t)
	used := map[allocSite]string{}
	callRe := regexp.MustCompile(`emitChecked\w+\(\s*(allocSite\w+)`)
	for _, name := range emitterSourceFiles(t) {
		raw, err := os.ReadFile(name) // #nosec G304 -- package-owned path
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, m := range callRe.FindAllStringSubmatch(string(raw), -1) {
			value, known := values[m[1]]
			if !known {
				t.Errorf("%s calls the guard with %s, which is not a declared site", name, m[1])
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

// TestTheGuardIsWhereTheReportedFileSaysItIs keeps the ledger row that excuses
// this raise pointed at the raise. The fatal-surface census keys its rows on
// file and function, and a row whose key has moved is reported as renumbered
// rather than as covered; this fails first, where the reason is legible.
func TestTheGuardIsWhereTheReportedFileSaysItIs(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("emit_alloc_guard.go")) // #nosec G304 -- package-owned path
	if err != nil {
		t.Fatalf("read the guard: %v", err)
	}
	if !strings.Contains(string(raw), "func (fe *funcEmitter) emitAllocRefusalFatal(") {
		t.Fatal("emitAllocRefusalFatal moved; update internal/panicgate/testdata/allowlist.json with it")
	}
}
