package llvm

import (
	"regexp"
	"testing"
)

func TestEmitNetBufferedWritePassesArrayHandle(t *testing.T) {
	sourceCode := `@entrypoint
fn main() -> int {
    let conn: TcpConn = { __opaque: 0 };
    let data: byte[] = [];
    let read_res: NetResult<byte[]> = rt_net_read_bytes(&conn, 64:uint);
    let _ = read_res;
    let write_res: NetResult<uint> = rt_net_write_bytes(&conn, &data, 0:uint, data.__len());
    let _ = write_res;
    return 0;
}
`

	ir := emitLLVMFromSource(t, sourceCode)

	if !regexp.MustCompile(`call ptr @rt_net_read_bytes\(`).MatchString(ir) {
		t.Fatalf("expected buffered net read intrinsic in IR:\n%s", ir)
	}
	if !regexp.MustCompile(`call ptr @rt_net_write_bytes\(`).MatchString(ir) {
		t.Fatalf("expected buffered net write intrinsic in IR:\n%s", ir)
	}
	if regexp.MustCompile(`call ptr @rt_net_write_bytes\([^,]+, ptr %l\d+,`).MatchString(ir) {
		t.Fatalf("rt_net_write_bytes received a local slot instead of an array handle:\n%s", ir)
	}
}
