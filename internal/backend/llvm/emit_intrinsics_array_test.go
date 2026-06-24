package llvm

import (
	"path/filepath"
	"regexp"
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

@entrypoint
fn main() -> int {
    let source: byte[] = "abcd" to byte[];
    let mut out: byte[] = [];
    append_range(&mut out, &source);
    rt_byte_array_drop_prefix(&mut out, 1:uint64);
    return out.__len() to int;
}
`

	ir := emitLLVMFromSource(t, sourceCode)

	if !regexp.MustCompile(`call void @rt_byte_array_append_range\(`).MatchString(ir) {
		t.Fatalf("expected byte range append intrinsic in IR:\n%s", ir)
	}
	if regexp.MustCompile(`call void @rt_byte_array_append_range\([^,]+, ptr %l\d+,`).MatchString(ir) {
		t.Fatalf("rt_byte_array_append_range received a local slot instead of an array handle:\n%s", ir)
	}
	if !regexp.MustCompile(`call void @rt_byte_array_drop_prefix\(`).MatchString(ir) {
		t.Fatalf("expected byte drop-prefix intrinsic in IR:\n%s", ir)
	}
}
