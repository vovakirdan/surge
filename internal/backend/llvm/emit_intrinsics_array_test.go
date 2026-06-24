package llvm

import (
	"path/filepath"
	"regexp"
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
