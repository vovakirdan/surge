package llvm

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"surge/internal/mir"
)

func TestArrayLiteralStorageForEmptyAndPopulatedValues(t *testing.T) {
	withRepoStdlib(t)
	t.Setenv(allocRefusalEnvVar, "")
	for _, tc := range []struct {
		name     string
		typeName string
		literal  string
		length   int
		dynamic  bool
	}{
		{"dynamic_empty_float", "float[]", "[]", 0, true},
		{"dynamic_empty_int32", "int32[]", "[]", 0, true},
		{"dynamic_nonempty_int32", "int32[]", "[17:int32, 23:int32]", 2, true},
		{"fixed_nonempty_int32", "int32[2]", "[17:int32, 23:int32]", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := fmt.Sprintf(`fn make_values() -> %s { return %s; }

@entrypoint
fn main() -> int {
    let values = make_values();
    return values.__len() to int;
}
`, tc.typeName, tc.literal)
			mod, result := lowerMIRFromSource(t, src)
			fn := findFuncByName(t, mod, "make_values")
			literals := 0
			for _, block := range fn.Blocks {
				for _, ins := range block.Instrs {
					if ins.Kind != mir.InstrAssign || ins.Assign.Src.Kind != mir.RValueArrayLit {
						continue
					}
					literals++
					if got := len(ins.Assign.Src.ArrayLit.Elems); got != tc.length {
						t.Fatalf("MIR array literal length = %d, want %d", got, tc.length)
					}
				}
			}
			if literals != 1 {
				t.Fatalf("make_values has %d MIR array literals, want exactly one", literals)
			}
			ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
			if err != nil {
				t.Fatalf("emit LLVM IR: %v", err)
			}
			body := functionBody(t, ir, fn.ID)
			if !tc.dynamic {
				requireFixedInt32LiteralStorage(t, body)
				return
			}

			wantAllocs := 1
			data := "null"
			if tc.length > 0 {
				wantAllocs++
				data = requireArrayLiteralAllocation(t, body, 8, 4)
			}
			if got := strings.Count(body, "call ptr @rt_alloc("); got != wantAllocs {
				t.Fatalf("make_values has %d allocations, want %d:\n%s", got, wantAllocs, body)
			}
			header := requireArrayLiteralAllocation(t, body, 24, 8)
			requireArrayLiteralFieldStore(t, body, header, 0, "i64", fmt.Sprint(tc.length), 8)
			requireArrayLiteralFieldStore(t, body, header, 8, "i64", fmt.Sprint(tc.length), 8)
			requireArrayLiteralFieldStore(t, body, header, 16, "ptr", data, 8)
			if tc.length > 0 {
				requireArrayLiteralFieldStore(t, body, data, 0, "i32", "17", 4)
				requireArrayLiteralFieldStore(t, body, data, 4, "i32", "23", 4)
			}
		})
	}
}

func requireArrayLiteralAllocation(t *testing.T, body string, size, align int) string {
	t.Helper()
	pattern := fmt.Sprintf(`(?m)^  (%%t\d+) = call ptr @rt_alloc\(i64 %d, i64 %d\)$`, size, align)
	matches := regexp.MustCompile(pattern).FindAllStringSubmatch(body, -1)
	if len(matches) != 1 {
		t.Fatalf("want one %d-byte allocation aligned to %d, got %d:\n%s", size, align, len(matches), body)
	}
	ptr := matches[0][1]
	check := regexp.MustCompile(`(?m)^  (%t\d+) = icmp eq ptr ` + regexp.QuoteMeta(ptr) + `, null$`).FindStringSubmatch(body)
	if len(check) != 2 || !strings.Contains(body, "br i1 "+check[1]+", label %") {
		t.Fatalf("allocation %s has no null-check branch:\n%s", ptr, body)
	}
	return ptr
}

func requireArrayLiteralFieldStore(t *testing.T, body, base string, offset int, ty, value string, align int) {
	t.Helper()
	pattern := fmt.Sprintf(`(?m)^  (%%t\d+) = getelementptr inbounds i8, ptr %s, i64 %d$`, regexp.QuoteMeta(base), offset)
	matches := regexp.MustCompile(pattern).FindAllStringSubmatch(body, -1)
	stores := 0
	for _, match := range matches {
		store := fmt.Sprintf("  store %s %s, ptr %s, align %d\n", ty, value, match[1], align)
		stores += strings.Count(body, store)
	}
	if stores != 1 {
		t.Fatalf("want one %s %s store at %s + %d, got %d:\n%s", ty, value, base, offset, stores, body)
	}
}

func requireFixedInt32LiteralStorage(t *testing.T, body string) {
	t.Helper()
	if strings.Contains(body, "call ptr @rt_alloc(") {
		t.Fatalf("fixed array literal allocated a header or element buffer:\n%s", body)
	}
	// The literal has its own inline slot; return-value storage may add others.
	allocas := regexp.MustCompile(`(?m)^  (%t\d+) = alloca \[8 x i8\], align 4$`).FindAllStringSubmatch(body, -1)
	var literalSlots []string
	for _, alloca := range allocas {
		pattern := `(?m)^  (%t\d+) = getelementptr inbounds i8, ptr ` + regexp.QuoteMeta(alloca[1]) + `, i64 0$`
		for _, field := range regexp.MustCompile(pattern).FindAllStringSubmatch(body, -1) {
			if strings.Contains(body, "  store i32 17, ptr "+field[1]+", align 4\n") {
				literalSlots = append(literalSlots, alloca[1])
			}
		}
	}
	if len(literalSlots) != 1 {
		t.Fatalf("want one inline literal slot storing the first element, got %d:\n%s", len(literalSlots), body)
	}
	requireArrayLiteralFieldStore(t, body, literalSlots[0], 0, "i32", "17", 4)
	requireArrayLiteralFieldStore(t, body, literalSlots[0], 4, "i32", "23", 4)
}
