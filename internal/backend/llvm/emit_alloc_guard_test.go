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

// runtimeAnswerClass says what a pointer-answering runtime entry point does when
// the allocator refuses it.
type runtimeAnswerClass int

const (
	// refusalIsReported: the entry point stops the process itself. Generated
	// code never sees the refusal.
	refusalIsReported runtimeAnswerClass = iota
	// nullIsNotARefusal: it can answer NULL, but never because it was refused.
	nullIsNotARefusal
	// refusalIsTested: it answers NULL on refusal and the generated code tests
	// that answer before storing through it.
	refusalIsTested
	// refusalIsUntested: it answers NULL on refusal and the generated code does
	// NOT test it. A live hole, recorded here so this census reports it instead
	// of reporting coverage it does not have.
	refusalIsUntested
)

type runtimeAnswer struct {
	class  runtimeAnswerClass
	reason string
}

func classified(class runtimeAnswerClass, reason string, names ...string) map[string]runtimeAnswer {
	out := make(map[string]runtimeAnswer, len(names))
	for _, name := range names {
		out[name] = runtimeAnswer{class: class, reason: reason}
	}
	return out
}

// runtimePointerAnswers classifies every runtime entry point that hands a
// pointer to generated code. The reasons are what a reader needs to disagree
// with the classification; each was read out of the named runtime source.
func runtimePointerAnswers() map[string]runtimeAnswer {
	groups := []map[string]runtimeAnswer{
		classified(refusalIsTested,
			"emitCheckedAlloc writes it and tests the answer; the memory intrinsic is excused per file, "+
				"because there the nullable answer is the language's own",
			"rt_alloc"),
		classified(refusalIsTested,
			"emitCheckedRealloc writes it and reports BEFORE the header records the answer, "+
				"because a refused reallocation releases nothing (runtime/native/rt_alloc.c: rt_realloc)",
			"rt_realloc"),
		classified(refusalIsTested,
			"emitCheckedRangeNew for the bounded form and emitRuntimeAnswerTest for the open-ended ones, "+
				"which are reached as ordinary calls to a runtime symbol; all four share alloc_range "+
				"(runtime/native/rt_range.c)",
			"rt_range_int_new", "rt_range_int_from_start", "rt_range_int_to_end", "rt_range_int_full"),

		classified(refusalIsReported,
			"a refused limb block is reported as the numeric size limit and stops the process through "+
				"bignum_panic_err (runtime/native/rt_bignum_uint_core.c: bu_alloc, rt_bignum_panic.c); "+
				"a NULL answer from these is the tagged encoding of zero, not a refusal",
			"rt_bigint_abs", "rt_bigint_add", "rt_bigint_bit_and", "rt_bigint_bit_or", "rt_bigint_bit_xor",
			"rt_bigint_div", "rt_bigint_from_i64", "rt_bigint_from_literal", "rt_bigint_from_u64",
			"rt_bigint_mod", "rt_bigint_mul", "rt_bigint_neg", "rt_bigint_shl", "rt_bigint_shr",
			"rt_bigint_sub", "rt_bigint_to_bigfloat", "rt_bigint_to_biguint",
			"rt_biguint_add", "rt_biguint_bit_and", "rt_biguint_bit_or", "rt_biguint_bit_xor",
			"rt_biguint_div", "rt_biguint_from_literal", "rt_biguint_from_u64", "rt_biguint_mod",
			"rt_biguint_mul", "rt_biguint_shl", "rt_biguint_shr", "rt_biguint_sub",
			"rt_biguint_to_bigfloat", "rt_biguint_to_bigint",
			"rt_bigfloat_abs", "rt_bigfloat_add", "rt_bigfloat_clone", "rt_bigfloat_div",
			"rt_bigfloat_from_f64", "rt_bigfloat_from_i64", "rt_bigfloat_from_literal",
			"rt_bigfloat_from_u64", "rt_bigfloat_mod", "rt_bigfloat_mul", "rt_bigfloat_neg",
			"rt_bigfloat_sub", "rt_bigfloat_to_bigint", "rt_bigfloat_to_biguint"),
		classified(refusalIsReported,
			"reports through its own panic before returning: array_panic / concat_panic / map_panic "+
				"(runtime/native/rt_array.c, rt_array_concat.c, rt_map.c)",
			"rt_array_concat", "rt_array_slice", "rt_array_slice_fixed", "rt_map_new", "rt_map_keys"),
		classified(refusalIsReported,
			"panic_msg on a refused task, scope, job or channel block (runtime/native/rt_async_task.c, "+
				"rt_async_scope.c, rt_async_blocking.c, rt_async_channel.c); the NULL beside it answers an "+
				"executor that ensure_exec returns a static for and never fails to give",
			"__task_create", "__task_state", "checkpoint", "rt_sleep", "rt_blocking_submit",
			"rt_scope_enter", "rt_channel_new"),
		classified(refusalIsReported,
			"tests its own answer and reports (runtime/native/rt_io.c: rt_readline, rt_term.c: rt_term_size)",
			"rt_readline", "rt_term_size"),
		classified(refusalIsReported,
			"a refused string block stops the process in string_alloc_or_report (runtime/native/rt_string.c); "+
				"every one of these reaches its storage through it",
			"rt_string_from_bytes", "rt_string_concat", "rt_string_repeat", "rt_string_clone",
			"rt_string_slice", "rt_string_from_int", "rt_string_from_uint", "rt_string_from_float",
			"rt_string_from_bigint", "rt_string_from_biguint", "rt_string_from_bigfloat",
			"rt_stdin_read_all"),

		classified(nullIsNotARefusal,
			"borrows the bytes of a live string and allocates nothing; its NULL answers a handle that is "+
				"not there (runtime/native/rt_string.c: rt_string_ptr)",
			"rt_string_ptr"),
		classified(nullIsNotARefusal,
			"adds a handle reference and allocates nothing; its NULL answers a task handle that is not "+
				"there (runtime/native/rt_async_task.c: rt_task_clone)",
			"rt_task_clone"),
		classified(nullIsNotARefusal,
			"no definition exists in runtime/native, so no call to it answers anything: a native program "+
				"that reaches this lowering does not link. Recorded rather than left blank",
			"rt_string_from_utf16"),
		classified(nullIsNotARefusal,
			"answers the address of a static hash generated into the runtime "+
				"(internal/abimanifest/render_c.go); it allocates nothing",
			"rt_typed_carrier_abi_manifest_hash"),

		classified(refusalIsUntested,
			"answers NULL when the tagged result block is refused (runtime/native/rt_fs_result.c: "+
				"fs_make_error, fs_make_success_*); the generated code stores it into the FsResult slot "+
				"and the match reads the discriminant through it",
			"rt_fs_open", "rt_fs_read", "rt_fs_write", "rt_fs_seek", "rt_fs_close", "rt_fs_flush",
			"rt_fs_read_file", "rt_fs_write_file", "rt_fs_cwd", "rt_fs_metadata", "rt_fs_mkdir",
			"rt_fs_read_dir", "rt_fs_remove_dir", "rt_fs_remove_file", "rt_fs_file_metadata",
			"rt_fs_file_name", "rt_fs_file_type"),
		classified(refusalIsUntested,
			"answers NULL when the tagged result block is refused (runtime/native/rt_net_result.c: "+
				"net_make_error, net_make_success_*); the generated code stores it into the NetResult slot "+
				"and the match reads the discriminant through it",
			"rt_net_accept", "rt_net_connect", "rt_net_listen", "rt_net_read", "rt_net_read_bytes",
			"rt_net_write", "rt_net_write_bytes", "rt_net_close_conn", "rt_net_close_listener"),
		classified(refusalIsUntested,
			"answers NULL when its own block is refused (runtime/native/rt_entropy.c: entropy_make_*, "+
				"rt_io.c: rt_argv, rt_alloc.c: rt_heap_stats, rt_string.c: rt_string_bytes_view)",
			"rt_entropy_bytes", "rt_argv", "rt_heap_stats", "rt_string_bytes_view"),
		classified(refusalIsUntested,
			"answers NULL when the tagged event block is refused (runtime/native/rt_term.c: "+
				"term_make_event_key, term_make_event_resize)",
			"rt_term_read_event"),
	}
	out := map[string]runtimeAnswer{}
	for _, group := range groups {
		for name, answer := range group {
			out[name] = answer
		}
	}
	return out
}

// untestedRuntimeAnswers is the pinned size of the open surface: entry points
// that answer NULL on refusal with nothing in front of them. It is written as a
// number so that adding one is a deliberate edit somebody reviews, and so that
// a lane which closes a family moves it DOWN and says which family.
//
// 31 on 2026-08-29: the filesystem result (17), the socket result (9), and five
// blocks that carry their own answer. The string family left this count on the
// same day it was measured, by reporting at the allocation instead.
const untestedRuntimeAnswers = 31

// TestEveryRuntimePointerAnswerIsClassified is the census.
//
// It runs over the ABI roster rather than over the emitters, because that is the
// only list a new runtime entry point cannot be added behind: a function the
// generated code can call is declared there or it does not link.
func TestEveryRuntimePointerAnswerIsClassified(t *testing.T) {
	answers := runtimePointerAnswers()
	declared := map[string]bool{}
	untested := []string{}
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
		if answer.class == refusalIsUntested {
			untested = append(untested, decl.name)
		}
	}
	for name := range answers {
		if !declared[name] {
			t.Errorf("%s is classified but is not a pointer-answering runtime declaration any more; "+
				"the classification has rotted", name)
		}
	}
	if len(untested) != untestedRuntimeAnswers {
		sort.Strings(untested)
		t.Errorf("%d runtime answers are refused-and-untested, the pin says %d:\n  %s",
			len(untested), untestedRuntimeAnswers, strings.Join(untested, "\n  "))
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
	// The async ref-parameter box writes its own test and traps. It stops the
	// process rather than faulting, which is the half this lane is about; it
	// reports nothing, which is the half it does not fix, because the stand that
	// pins it belongs to the async lane.
	"emit_func.go": "the async ref box tests its own allocation and traps",
}

// indirectPointerCallEmitters write a pointer-answering call whose callee is a
// parameter, so the name is not in the text this census reads. Each is recorded
// with the entry points it can name; all of them are classified above.
var indirectPointerCallEmitters = map[string]string{
	"emit_intrinsics_fs.go":      "rt_fs_close, rt_fs_flush, rt_fs_file_name, rt_fs_file_type, rt_fs_file_metadata",
	"emit_intrinsics_net.go":     "rt_net_close_listener, rt_net_close_conn",
	"emit_intrinsics_runtime.go": "rt_string_from_bytes, rt_string_from_utf16",
	"emit_iter.go":               "rt_bigint_from_i64, rt_biguint_from_u64, rt_bigint_add, rt_biguint_add",
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

// allocGuardArrayProgram reaches every shape the guard writes: the literal's two
// allocations, both iterator cursors, the growth path a push takes, and the two
// Range constructors — the bounded one an emitter writes and the open-ended one
// that is an ordinary call to a runtime symbol.
// One array literal, because the negative control aims at a SITE and a second
// literal would make the same site refuse twice.
const allocGuardArrayProgram = `@entrypoint
fn main() -> int {
    let mut a: int[] = [1, 2, 3];
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
    a.push(total);
    let tail: int[] = a[[1..]];
    return a[3] + tail[0];
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
// call, the test against null, and a reporter that names the type. Every entry
// point classified as tested is held to it, so a call added to the guard cannot
// be added without its test.
//
// The message is checked as the bytes the module actually carries, because that
// is what a person reads on stderr; asserting the Go string would pass even if
// the constant were emitted at the wrong length.
func TestAGuardedAllocationIsTestedAndReportsItsType(t *testing.T) {
	ir := emitAllocGuardProgram(t)
	answers := runtimePointerAnswers()
	lines := strings.Split(ir, "\n")
	callRe := regexp.MustCompile(`^\s*(%t\d+) = call ptr @(rt_[a-z0-9_]+)\(`)
	tested := map[string]int{}
	for i, line := range lines {
		m := callRe.FindStringSubmatch(line)
		if m == nil || answers[m[2]].class != refusalIsTested {
			continue
		}
		want := fmt.Sprintf("= icmp eq ptr %s, null", m[1])
		if i+1 >= len(lines) || !strings.Contains(lines[i+1], want) {
			t.Fatalf("the %s at line %d is not tested; next line is %q", m[2], i+1, lines[i+1])
		}
		if i+4 >= len(lines) || !strings.Contains(lines[i+4], "call void @rt_panic(") {
			t.Fatalf("the refusal block for %s does not report; line is %q", m[1], lines[i+4])
		}
		if !strings.Contains(lines[i+5], "unreachable") {
			t.Fatalf("the refusal block for %s returns; line is %q", m[1], lines[i+5])
		}
		tested[m[2]]++
	}
	for _, want := range []string{"rt_alloc", "rt_realloc", "rt_range_int_new", "rt_range_int_from_start"} {
		if tested[want] == 0 {
			t.Fatalf("the program emitted no %s at all, so nothing was proven about it", want)
		}
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
	for _, site := range []allocSite{
		allocSiteArrayElements, allocSiteArrayHeader, allocSiteArrayIter,
		allocSiteRangeIter, allocSiteArrayGrowPush,
	} {
		t.Run(string(site), func(t *testing.T) {
			t.Setenv(allocRefusalEnvVar, string(site))
			if n := strings.Count(emitAllocGuardProgram(t), "i64 "+allocRefusalSize); n != 1 {
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
