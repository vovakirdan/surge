package llvm

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestArrayIteratorFloatYieldRetainsOnce(t *testing.T) {
	for _, tc := range []struct {
		name, size string
	}{
		{"dynamic_float", ""},
		{"fixed_float", "2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SURGE_STDLIB", repoRootFromLLVMTest(t))
			source := fmt.Sprintf(`@entrypoint
fn main() -> int {
    let values: float[%s] = [1.5, 2.5];
    let mut n: int = 0;
    for x: float in values { n = n + 1; }
    return n;
}
`, tc.size)
			module, result := lowerMIRFromSource(t, source)
			ir, err := EmitModule(module, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
			if err != nil {
				t.Fatal(err)
			}
			body := functionBody(t, ir, findMIRFunc(t, module, "main").ID)
			// A cursor addresses its element by a runtime stride. Constant
			// offsets in literal initialization and bound descriptors cannot
			// satisfy this shape, so this selects the actual yielded load.
			load := regexp.MustCompile(`(?m)^\s*(%t\d+) = getelementptr inbounds i8, ptr %t\d+, i64 %t\d+\n\s*(%t\d+) = load (ptr), ptr (%t\d+)(?:, align \d+)?\n`)
			matches := load.FindAllStringSubmatch(body, -1)
			if len(matches) != 1 || matches[0][1] != matches[0][4] {
				t.Fatalf("expected one cursor element load, got %v:\n%s", matches, body)
			}
			value := matches[0][2]
			bumps := strings.Count(body, " = icmp eq ptr "+value+", null\n")
			if bumps != 1 {
				t.Fatalf("yielded scalar retain count = %d, want exactly one:\n%s", bumps, body)
			}
			loadAt := strings.Index(body, matches[0][0])
			retainAt := strings.Index(body, " = icmp eq ptr "+value+", null\n")
			payloadAt := strings.Index(body, "store ptr "+value+", ptr ")
			if retainAt <= loadAt || payloadAt <= retainAt {
				t.Fatal("yield did not acquire its reference between the element load and Some payload store")
			}
			between := body[retainAt:payloadAt]
			bump := regexp.MustCompile(`(?m)^\s*(%t\d+) = select i1 %t\d+, ptr @\S+, ptr ` + value + `\n\s*(%t\d+) = load i32, ptr (%t\d+)\n\s*(%t\d+) = add i32 (%t\d+), 1\n\s*store i32 (%t\d+), ptr (%t\d+)\n`)
			chains := bump.FindAllStringSubmatch(between, -1)
			if len(chains) != 1 {
				t.Fatalf("yield retain does not perform exactly one RC bump on the loaded element:\n%s", between)
			}
			m := chains[0]
			if m[1] != m[3] || m[1] != m[7] || m[2] != m[5] || m[4] != m[6] {
				t.Fatalf("yield RC load/add/store does not use the selected element's counter: %v", m)
			}
		})
	}
}
