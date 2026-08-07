package llvm

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestEmitByteArrayAppendRangePassesSourceArrayHandle(t *testing.T) {
	repoRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	t.Setenv("SURGE_STDLIB", repoRoot)

	sourceCode := `fn append_range(dst: &mut byte[], src: &byte[]) -> nothing {
    rt_byte_array_append_range(dst, src, 1:uint64, 2:uint64);
    return nothing;
}

fn parse_token(src: &byte[]) -> bool {
    let mut value: uint64 = 0:uint64;
    let mut next: uint64 = 0:uint64;
    return rt_byte_parse_uint64_token(src, 0:uint64, src.__len() to uint64, &mut value, &mut next);
}

@entrypoint
fn main() -> int {
    let source: byte[] = "abcd" to byte[];
    let mut out: byte[] = [];
    append_range(&mut out, &source);
    rt_byte_array_drop_prefix(&mut out, 1:uint64);
    let _ = parse_token(&source);
    return out.__len() to int;
}
`

	ir := emitLLVMFromSource(t, sourceCode)

	if !regexp.MustCompile(`call void @rt_byte_array_append_range\(`).MatchString(ir) {
		t.Fatalf("expected byte range append intrinsic in IR:\n%s", ir)
	}
	callRe := regexp.MustCompile(`call void @rt_byte_array_append_range\(ptr [^,]+, ptr (%t\d+),`)
	matches := callRe.FindStringSubmatch(ir)
	if len(matches) != 2 {
		t.Fatalf("expected byte range append source to be a loaded temp:\n%s", ir)
	}
	if !strings.Contains(ir, matches[1]+" = load ptr, ptr ") {
		t.Fatalf("rt_byte_array_append_range source was not loaded as an array handle:\n%s", ir)
	}
	if regexp.MustCompile(`call void @rt_byte_array_append_range\([^,]+, ptr %l\d+,`).MatchString(ir) {
		t.Fatalf("rt_byte_array_append_range received a local slot instead of an array handle:\n%s", ir)
	}
	if !regexp.MustCompile(`call void @rt_byte_array_drop_prefix\(`).MatchString(ir) {
		t.Fatalf("expected byte drop-prefix intrinsic in IR:\n%s", ir)
	}
	if !regexp.MustCompile(`call i1 @rt_byte_parse_uint64_token\(`).MatchString(ir) {
		t.Fatalf("expected byte uint64 token parse intrinsic in IR:\n%s", ir)
	}
	parseRe := regexp.MustCompile(`call i1 @rt_byte_parse_uint64_token\(ptr (%t\d+),`)
	parseMatches := parseRe.FindStringSubmatch(ir)
	if len(parseMatches) != 2 {
		t.Fatalf("expected byte uint64 token parser source to be a loaded temp:\n%s", ir)
	}
	if !strings.Contains(ir, parseMatches[1]+" = load ptr, ptr ") {
		t.Fatalf("rt_byte_parse_uint64_token source was not loaded as an array handle:\n%s", ir)
	}
}

// A dynamic array of a struct holds the STRUCTS, not pointers to them. Its
// data buffer is length times the language stride, and element `i` is written
// where that stride puts it.
//
// This used to be the other way round: the buffer held one word per element and
// each word pointed at a separately allocated box, so the buffer was allocated
// at a pointer stride of 8 while `internal/layout` said 16. Two strides for one
// array is what made every element access have to know which of them applied.
func TestDynamicArrayOfStructValuesStoresElementsAtTheLanguageStride(t *testing.T) {
	withRepoStdlib(t)
	sourceCode := `type Point = { x: int, y: int };

@entrypoint
fn main() -> int {
    let points: Point[] = [
        Point { x = 1, y = 2 },
        Point { x = 3, y = 4 },
        Point { x = 5, y = 6 },
    ];
    let mut sum: int = 0;
    for point in points {
        sum = sum + point.x + point.y;
    }
    return sum;
}
`

	ir := emitLLVMFromSource(t, sourceCode)
	body := llvmFunctionContaining(t, ir, "@rt_alloc(i64 48, i64 8)")

	// Three elements at a 16-byte stride, and each one copied whole into its
	// own slot. A buffer of pointers would be 24 bytes and each store a word.
	data := regexp.MustCompile(`(%t\d+) = call ptr @rt_alloc\(i64 48, i64 8\)`).FindStringSubmatch(body)
	if len(data) != 2 {
		t.Fatalf("dynamic Point[] did not allocate 3 elements at the 16-byte language stride:\n%s", body)
	}
	for index, offset := range []int{0, 16, 32} {
		slot := regexp.MustCompile(
			`(%t\d+) = getelementptr inbounds i8, ptr ` + regexp.QuoteMeta(data[1]) +
				`, i64 ` + strconv.Itoa(offset) + `\n`).FindStringSubmatch(body)
		if len(slot) != 2 {
			t.Fatalf("dynamic Point[] element %d is not at byte offset %d:\n%s", index, offset, body)
		}
		copied := `@llvm.memcpy.p0.p0.i64\(ptr align 8 ` + regexp.QuoteMeta(slot[1]) + `, ptr align 8 %\w+, i64 16,`
		if !regexp.MustCompile(copied).MatchString(body) {
			t.Fatalf("dynamic Point[] element %d was not copied whole into its slot:\n%s", index, body)
		}
	}
}
