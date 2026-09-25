package llvm

import (
	"regexp"
	"strings"
	"testing"
)

// A `for` over a Range and an explicit `.next()` load the range's kind byte
// first, and a Range's default is NULL (emitDefaultValue). Before the owner's
// 2026-09-25 ruling that load went through NULL and the process died of
// SIGSEGV with no message; now the emitter calls rt_range_require on the handle
// first, which refuses a null range with the VM's words (`panic VM1203: null
// range handle`). The D2 return-origin gate refuses every program that reaches
// a Range default at this base, so this row reads the IR of a program with LIVE
// ranges: the call must be there, on the same handle, right before each load of
// a handed-in range's kind byte. The loop's own cursor, which iter_init
// allocates, is never null and takes no call.
func TestEmitRangeIterationRequiresALiveRange(t *testing.T) {
	sourceCode := `@entrypoint
fn main() -> int {
    let r = 0..3;
    let mut total = 0;
    for i in r {
        total = total + i;
    }
    let mut s = 5..7;
    let first = s.next();
    let add = compare first { Some(v) => v; nothing => 100; };
    return total + add;
}
`
	ir := emitLLVMFromSource(t, sourceCode)
	if !strings.Contains(ir, "declare void @rt_range_require(ptr)") {
		t.Fatalf("rt_range_require is not declared:\n%s", ir)
	}
	lines := strings.Split(ir, "\n")
	require := regexp.MustCompile(`^\s*call void @rt_range_require\(ptr (%[\w.]+)\)$`)
	kindLoad := regexp.MustCompile(`^\s*%[\w.]+ = getelementptr inbounds i8, ptr (%[\w.]+), i64 19$`)
	calls := 0
	for i, line := range lines {
		m := require.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		calls++
		if i+1 >= len(lines) {
			t.Fatalf("rt_range_require is the last line of the module")
		}
		next := kindLoad.FindStringSubmatch(lines[i+1])
		if next == nil || next[1] != m[1] {
			t.Fatalf("rt_range_require(%s) is not followed by the kind-byte load of the same handle: %q", m[1], lines[i+1])
		}
	}
	// One `for` over a handed-in range, one explicit `.next()`.
	if calls != 2 {
		t.Fatalf("rt_range_require is called %d times, want 2 (the for's iter_init and the .next())\n%s", calls, ir)
	}
}
