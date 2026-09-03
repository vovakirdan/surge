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
	// refusalIsSwallowed: the refusal never reaches the generated code AS a
	// refusal — the entry point drops it inside itself and answers a NUMBER. No
	// test the emitter could write would see this one: the answer is a legal
	// value, not a null. It is a class of its own because it is neither
	// reported nor testable, and calling it either would be a false row.
	refusalIsSwallowed
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
			"emitCheckedFrameAlloc writes it and tests the answer; it reaches rt_alloc at the width "+
				"its descriptor states and reports nothing itself, because the sentence names the TYPE "+
				"and only the caller has one (runtime/native/rt_frame.c: rt_frame_alloc)",
			"rt_frame_alloc"),
		classified(refusalIsTested,
			"emitCheckedRangeNew for the bounded form and emitRuntimeAnswerTest for the open-ended ones, "+
				"which are reached as ordinary calls to a runtime symbol; all four share alloc_range "+
				"(runtime/native/rt_range.c)",
			"rt_range_int_new", "rt_range_int_from_start", "rt_range_int_to_end", "rt_range_int_full"),

		classified(refusalIsReported,
			"the limb block is taken with an error out-parameter, and a refused one is reported as the "+
				"numeric size limit through bignum_panic_err (runtime/native/rt_bignum_int.c: bi_alloc, "+
				"rt_bignum_uint_core.c: bu_alloc, rt_bignum_panic.c); the NULL these answer beside it is "+
				"the tagged encoding of zero, not a refusal",
			"rt_bigint_from_i64", "rt_bigint_from_literal", "rt_bigint_from_u64",
			"rt_biguint_from_literal", "rt_biguint_from_u64", "rt_biguint_to_bigint",
			"rt_bigfloat_abs", "rt_bigfloat_add", "rt_bigfloat_clone", "rt_bigfloat_div",
			"rt_bigfloat_from_f64", "rt_bigfloat_from_i64", "rt_bigfloat_from_literal",
			"rt_bigfloat_from_u64", "rt_bigfloat_mod", "rt_bigfloat_mul", "rt_bigfloat_neg",
			"rt_bigfloat_sub", "rt_bigfloat_to_bigint", "rt_bigfloat_to_biguint"),
		classified(refusalIsSwallowed,
			"promotes a tagged operand with NO error out-parameter — bi_promote calls "+
				"bi_from_i64(fixi_value(w), NULL) and bu_promote calls bu_from_u64(fixu_value(w), NULL) "+
				"(runtime/native/rt_bignum_api.c: bi_promote, bu_promote), and bi_alloc/bu_alloc only "+
				"record BN_ERR_MAX_LIMBS when they are given somewhere to record it. A refused promotion "+
				"therefore yields a NULL operand that is indistinguishable from the tagged zero, and "+
				"bi_add(NULL, b, &err) answers bi_clone(b) with err still BN_OK: `a + b` returns `b`. "+
				"This is ordinary int arithmetic, not a wide-number corner. The repair is the error "+
				"out-parameter these two helpers do not pass, and it belongs to the bignum lane",
			"rt_bigint_abs", "rt_bigint_add", "rt_bigint_bit_and", "rt_bigint_bit_or", "rt_bigint_bit_xor",
			"rt_bigint_div", "rt_bigint_mod", "rt_bigint_mul", "rt_bigint_neg", "rt_bigint_shl",
			"rt_bigint_shr", "rt_bigint_sub", "rt_bigint_to_bigfloat",
			"rt_biguint_add", "rt_biguint_bit_and", "rt_biguint_bit_or", "rt_biguint_bit_xor",
			"rt_biguint_div", "rt_biguint_mod", "rt_biguint_mul", "rt_biguint_shl", "rt_biguint_shr",
			"rt_biguint_sub", "rt_biguint_to_bigfloat"),
		classified(refusalIsSwallowed,
			"clones the magnitude with no error out-parameter — bu_clone(bi_as_uint(src), NULL) "+
				"(runtime/native/rt_bignum_api.c: rt_bigint_to_biguint) — so a refused clone is handed to "+
				"bu_finish as NULL and the conversion answers zero for a number that was not zero",
			"rt_bigint_to_biguint"),
		classified(refusalIsReported,
			"reports through its own panic before returning: array_panic / concat_panic / map_panic "+
				"(runtime/native/rt_array.c, rt_array_concat.c, rt_map.c)",
			"rt_array_concat", "rt_array_slice", "rt_array_slice_fixed", "rt_map_new", "rt_map_keys"),
		classified(refusalIsReported,
			"panic_msg on a refused task, scope, job or channel block (runtime/native/rt_async_task.c, "+
				"rt_async_scope.c, rt_async_blocking.c, rt_async_channel.c); the NULL beside it answers an "+
				"executor that ensure_exec returns a static for and never fails to give",
			"__task_create", "__task_create_affine", "__task_state", "checkpoint", "rt_sleep",
			"rt_blocking_submit", "rt_scope_enter", "rt_channel_new"),
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

		classified(refusalIsReported,
			"a refused FsResult block stops the process in rt_tag_alloc_or_report "+
				"(runtime/native/rt_fs_result.c: fs_make_error, fs_make_success_*, through "+
				"FS_RESULT_ALLOC); the generated code stores the answer into the FsResult slot "+
				"untested, and since Wave F F2 that answer is never NULL",
			"rt_fs_open", "rt_fs_read", "rt_fs_write", "rt_fs_seek", "rt_fs_close", "rt_fs_flush",
			"rt_fs_read_file", "rt_fs_write_file", "rt_fs_cwd", "rt_fs_metadata", "rt_fs_mkdir",
			"rt_fs_read_dir", "rt_fs_remove_dir", "rt_fs_remove_file", "rt_fs_file_metadata",
			"rt_fs_file_name", "rt_fs_file_type"),
		classified(refusalIsReported,
			"a refused NetResult block, or the refused byte-array header behind one, stops the "+
				"process in rt_tag_alloc_or_report / rt_alloc_or_report (runtime/native/"+
				"rt_net_result.c: net_make_error, net_make_success_*, through NET_RESULT_ALLOC); "+
				"the generated code stores the answer untested, and since Wave F F2 it is never NULL",
			"rt_net_accept", "rt_net_connect", "rt_net_listen", "rt_net_read", "rt_net_read_bytes",
			"rt_net_write", "rt_net_write_bytes", "rt_net_close_conn", "rt_net_close_listener"),
		classified(refusalIsReported,
			"a refused block stops the process in rt_alloc_or_report / rt_tag_alloc_or_report "+
				"(runtime/native/rt_entropy.c: entropy_make_*, rt_io.c: rt_argv, rt_alloc.c: "+
				"rt_heap_stats, which also reports an accounting snapshot it cannot take); the "+
				"generated code stores the answer untested, and since Wave F F2 it is never NULL",
			"rt_entropy_bytes", "rt_argv", "rt_heap_stats"),
		classified(nullIsNotARefusal,
			"a refused view block stops the process in rt_alloc_or_report (runtime/native/rt_string.c: "+
				"rt_string_bytes_view); the NULL it still answers is for a string handle that is not "+
				"there, which is the language's own answer and not a refusal",
			"rt_string_bytes_view"),
		classified(refusalIsReported,
			"a refused event block, at any of the levels an event is built from, stops the process "+
				"in rt_tag_alloc_or_report / rt_alloc_or_report (runtime/native/rt_term.c: "+
				"term_make_key, term_make_key_event, term_make_event_key, term_make_event_resize, "+
				"term_make_event_eof, through TERM_EVENT_ALLOC); the generated code stores the answer "+
				"untested, and since Wave F F2 it is never NULL",
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
	"emit_task_result.go":        "__task_create, __task_create_affine",
	"emit_intrinsics_fs.go":      "rt_fs_close, rt_fs_flush, rt_fs_file_name, rt_fs_file_type, rt_fs_file_metadata",
	"emit_intrinsics_net.go":     "rt_net_close_listener, rt_net_close_conn",
	"emit_intrinsics_runtime.go": "rt_string_from_bytes, rt_string_from_utf16",
	"emit_iter.go":               "rt_bigint_from_i64, rt_biguint_from_u64, rt_bigint_add, rt_biguint_add",
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
