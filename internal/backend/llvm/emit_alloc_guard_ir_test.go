package llvm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The allocation-refusal guard read out of the emitted module.
//
// The census next door reads emitter TEXT, and text cannot see the generic call
// path: emit_call_site.go writes its callee through a format operand, so no
// entry point is spelled there at all. This file reads the IR instead, where a
// call is a call however it was written, and asks two things of every one whose
// answer is classified as tested: that the next instruction tests it, and that
// the block it branches to reports the SENTENCE. The second half is here
// because a shape check is content-blind — the bounded Range site passed it for
// a whole review cycle while reporting `panic: Range<int>`, a bare type name
// with no reason in it.

// allocGuardArrayProgram reaches every shape the guard writes: the literal's two
// allocations, both iterator cursors, the growth path a push takes, and the
// bounded Range constructor as the BINARY OPERATOR spells it.
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

// allocGuardRangeProgram reaches the same bounded constructor through the OTHER
// spelling. `[1..3]` is not a binary operator: internal/hir/lower_expr_range.go
// lowers it to an ordinary call to rt_range_int_new, which arrives at
// emitCallSite like any other runtime symbol and is tested there or nowhere.
// The loop is what makes the omission fatal rather than merely untested — the
// cursor reads the kind byte out of the object before it allocates anything, so
// an untested refusal is a load through NULL.
//
// It is a second program rather than three more lines in the first because the
// negative control aims at one SITE, and a second range loop over there would
// make arming the range cursor refuse twice.
const allocGuardRangeProgram = `@entrypoint
fn main() -> int {
    let a: int[] = [1, 2, 3];
    let mut total: int = 0;
    let bracketed = [1..3];
    for j in bracketed {
        total = total + j;
    }
    let head: int[] = a[[..2]];
    let whole: int[] = a[[..]];
    return total + head[0] + whole[0];
}
`

// allocGuardProgram is one program together with the entry points it must
// reach. The list is asserted, so a program that stops reaching a constructor —
// because a lowering changed — fails here instead of passing vacuously over a
// site nothing emits any more.
type allocGuardProgram struct {
	name    string
	source  string
	reaches []string
}

func allocGuardPrograms() []allocGuardProgram {
	return []allocGuardProgram{
		{
			name:    "operator_range_and_array_growth",
			source:  allocGuardArrayProgram,
			reaches: []string{"rt_alloc", "rt_realloc", "rt_range_int_new", "rt_range_int_from_start"},
		},
		{
			name:    "bracketed_range_literals",
			source:  allocGuardRangeProgram,
			reaches: []string{"rt_alloc", "rt_range_int_new", "rt_range_int_to_end", "rt_range_int_full"},
		},
	}
}

func emitAllocGuardProgram(t *testing.T, source string) string {
	t.Helper()
	mirMod, result := lowerMIRFromSource(t, source)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	return ir
}

var (
	emittedGuardCallRe = regexp.MustCompile(`^\s*(%t\d+) = call ptr @(rt_[a-z0-9_]+)\(`)
	panicMessageRefRe  = regexp.MustCompile(`ptr @(\.allocmsg\.\d+),`)
	messageConstRe     = regexp.MustCompile(`^@(\.allocmsg\.\d+) = private unnamed_addr constant \[\d+ x i8\] c"([^"]*)"`)
)

// allocMessageConstants decodes the module's per-type refusal sentences. The
// bytes are read back out of the constant rather than compared as a Go string,
// because what a person sees on stderr is the bytes and a constant emitted at
// the wrong length would still carry the right prefix.
func allocMessageConstants(t *testing.T, ir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, line := range strings.Split(ir, "\n") {
		m := messageConstRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var sb strings.Builder
		for _, esc := range strings.Split(m[2], `\`) {
			if len(esc) < 2 {
				continue
			}
			b, err := strconv.ParseUint(esc[:2], 16, 8)
			if err != nil {
				t.Fatalf("constant %s carries %q, which is not the hex escaping this emitter writes", m[1], esc)
			}
			sb.WriteByte(byte(b))
			sb.WriteString(esc[2:])
		}
		out[m[1]] = sb.String()
	}
	return out
}

// TestAGuardedAllocationIsTestedAndReportsItsType reads the emitted shape: the
// call, the test against null, and a reporter that names the type.
//
// Every entry point classified as tested is held to it on every program that
// reaches it, and the classification's whole tested class must be reached by
// some program, so a name cannot be classified tested and proven by nothing.
func TestAGuardedAllocationIsTestedAndReportsItsType(t *testing.T) {
	answers := runtimePointerAnswers()
	seen := map[string]bool{}
	for _, prog := range allocGuardPrograms() {
		t.Run(prog.name, func(t *testing.T) {
			ir := emitAllocGuardProgram(t, prog.source)
			messages := allocMessageConstants(t, ir)
			lines := strings.Split(ir, "\n")
			tested := map[string]int{}
			for i, line := range lines {
				m := emittedGuardCallRe.FindStringSubmatch(line)
				if m == nil || answers[m[2]].class != refusalIsTested {
					continue
				}
				assertRefusalShape(t, lines, messages, i, m[1], m[2])
				tested[m[2]]++
			}
			for _, want := range prog.reaches {
				if tested[want] == 0 {
					t.Fatalf("the program emitted no %s at all, so nothing was proven about it", want)
				}
			}
		})
		for _, name := range prog.reaches {
			seen[name] = true
		}
	}
	for name, answer := range answers {
		if answer.class == refusalIsTested && !seen[name] {
			t.Errorf("%s is classified as tested and no program here reaches it; "+
				"add it to the reaches list of a program that emits it, or the class is a claim nothing checks", name)
		}
	}
}

// assertRefusalShape holds one emitted call to the whole guard: the test, the
// branch, the report, and the sentence the report carries.
func assertRefusalShape(t *testing.T, lines []string, messages map[string]string, i int, tmp, callee string) {
	t.Helper()
	want := fmt.Sprintf("= icmp eq ptr %s, null", tmp)
	if i+1 >= len(lines) || !strings.Contains(lines[i+1], want) {
		t.Fatalf("the %s at line %d is not tested; next line is %q", callee, i+1, lines[i+1])
	}
	if i+4 >= len(lines) || !strings.Contains(lines[i+4], "call void @rt_panic(") {
		t.Fatalf("the refusal block for %s does not report; line is %q", tmp, lines[i+4])
	}
	if !strings.Contains(lines[i+5], "unreachable") {
		t.Fatalf("the refusal block for %s returns; line is %q", tmp, lines[i+5])
	}
	ref := panicMessageRefRe.FindStringSubmatch(lines[i+4])
	if ref == nil {
		t.Fatalf("the refusal block for %s does not report through a message constant; line is %q", callee, lines[i+4])
	}
	text, ok := messages[ref[1]]
	if !ok {
		t.Fatalf("the refusal block for %s names @%s, which the module does not define", callee, ref[1])
	}
	const prefix = "out of memory: could not allocate "
	if !strings.HasPrefix(text, prefix) {
		t.Fatalf("a refused %s reports %q; every refusal reads %q plus the type", callee, text, prefix)
	}
	if strings.TrimPrefix(text, prefix) == "" {
		t.Fatalf("a refused %s reports the sentence with no type in it: %q", callee, text)
	}
}

// TestTheRefusalMessageIsNotInTheTraceStringTable is the mistake this guard made
// on its first writing: the per-type sentence went into the lazily filled table
// that ALSO backs the backtrace maps, where the walker indexes rows by position.
// A message there is a row nothing names and the runtime can still reach.
func TestTheRefusalMessageIsNotInTheTraceStringTable(t *testing.T) {
	ir := emitAllocGuardProgram(t, allocGuardArrayProgram)
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
	if n := strings.Count(emitAllocGuardProgram(t, allocGuardArrayProgram), "i64 "+allocRefusalSize); n != 0 {
		t.Fatalf("an unarmed build already asks for the refusal size %d times", n)
	}
	for _, site := range []allocSite{
		allocSiteArrayElements, allocSiteArrayHeader, allocSiteArrayIter,
		allocSiteRangeIter, allocSiteArrayGrowPush,
	} {
		t.Run(string(site), func(t *testing.T) {
			t.Setenv(allocRefusalEnvVar, string(site))
			if n := strings.Count(emitAllocGuardProgram(t, allocGuardArrayProgram), "i64 "+allocRefusalSize); n != 1 {
				t.Fatalf("arming %q made %d allocations refuse, want 1", site, n)
			}
		})
	}
}
