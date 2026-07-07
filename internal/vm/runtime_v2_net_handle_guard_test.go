//go:build runtime_v2_pending

package vm_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Epic 10 Task 3 (RV2-DEBT-010) behavior and static gates for the copied
// net-handle generation contract. A public TcpConn may be a reconstructed
// 8-byte box carrying only the handle word {fd, closed, owner_shard_valid,
// generation_check}; every data-path op validates that word against the
// owning shard's fd-registry row before touching the fd.

// netHandleStaleReuseSource is self-contained: it proves that a handle copy
// taken before close cannot act on the SAME fd number after the OS reuses it
// for a new connection. Flow: conn1/a1 open, snapshot conn1's handle word,
// close ONLY conn1 so exactly one fd number is free, connect conn2 — on
// Linux the lowest free descriptor, i.e. conn1's number, is reused — then
// every stale op (read, write, close) must fail with exactly
// NET_ERR_NOT_CONNECTED(5) from the generation guard: the reused fd's
// registry row carries a newer generation whose low 16 bits mismatch the
// stale handle word. The load-bearing clause is the stale close: it must NOT
// close(2) conn2's live fd, proven by conn2 round-tripping a probe byte
// afterwards. __opaque values are raw handle words, not bignum ints, so the
// program only moves them (no arithmetic or comparison). Distinct exit codes
// identify the first failed expectation.
const netHandleStaleReuseSource = `import stdlib/net as net;

fn probe() -> byte[] {
    let mut out: byte[] = [];
    out.push(88:byte);
    return out;
}

@entrypoint
fn main() -> int {
    let listen_res = net.listen("127.0.0.1", %[1]d:uint);
    compare listen_res {
        Success(listener) => {
            let conn1_res = net.connect("127.0.0.1", %[1]d:uint);
            compare conn1_res {
                Success(conn1) => {
                    let a1_res = net.accept(&listener).await();
                    let mut a1_handle: int = 0;
                    let mut a1_ok: bool = false;
                    compare a1_res {
                        Success(res) => {
                            compare res {
                                Success(a1) => {
                                    a1_handle = a1.__opaque;
                                    a1_ok = true;
                                }
                                _ => {}
                            };
                        }
                        Cancelled() => {}
                    };
                    if !a1_ok { return 31; }
                    let stale_handle: int = conn1.__opaque;
                    let close1_res = net.close_conn(own conn1);
                    let close1_ok: bool = compare close1_res { Success(_) => true; _ => false; };
                    if !close1_ok { return 32; }

                    let conn2_res = net.connect("127.0.0.1", %[1]d:uint);
                    compare conn2_res {
                        Success(conn2) => {
                            let a2_res = net.accept(&listener).await();
                            let mut a2_handle: int = 0;
                            let mut a2_ok: bool = false;
                            compare a2_res {
                                Success(res) => {
                                    compare res {
                                        Success(a2) => {
                                            a2_handle = a2.__opaque;
                                            a2_ok = true;
                                        }
                                        _ => {}
                                    };
                                }
                                Cancelled() => {}
                            };
                            if !a2_ok { return 34; }

                            let stale: TcpConn = { __opaque: stale_handle };
                            let sread_res = net.read_some(&stale, 4:uint).await();
                            let sread_code: uint = compare sread_res {
                                Success(res) => compare res { Success(_) => 0:uint; err => err.code; };
                                Cancelled() => 900:uint;
                            };
                            if sread_code != 5:uint { return 22; }
                            let swrite_res = net.write_all(&stale, probe()).await();
                            let swrite_code: uint = compare swrite_res {
                                Success(res) => compare res { Success(_) => 0:uint; err => err.code; };
                                Cancelled() => 900:uint;
                            };
                            if swrite_code != 5:uint { return 23; }
                            let stale_close: TcpConn = { __opaque: stale_handle };
                            let sclose_res = net.close_conn(own stale_close);
                            let sclose_code: uint = compare sclose_res { Success(_) => 0:uint; err => err.code; };
                            if sclose_code != 5:uint { return 24; }

                            let cwrite_res = net.write_all(&conn2, probe()).await();
                            let cwrite_ok: bool = compare cwrite_res {
                                Success(res) => compare res { Success(_) => true; _ => false; };
                                Cancelled() => false;
                            };
                            if !cwrite_ok { return 25; }
                            let a2_conn: TcpConn = { __opaque: a2_handle };
                            let aread_res = net.read_some(&a2_conn, 4:uint).await();
                            let aread_ok: bool = compare aread_res {
                                Success(res) => compare res {
                                    Success(bytes) => bytes.__len() == 1:uint;
                                    _ => false;
                                };
                                Cancelled() => false;
                            };
                            if !aread_ok { return 26; }
                            let _ = net.close_conn(own conn2);
                            let a2_close: TcpConn = { __opaque: a2_handle };
                            let _ = net.close_conn(own a2_close);
                            let _ = net.close_listener(own listener);
                            print("ok");
                            return 0;
                        }
                        _ => return 35;
                    };
                }
                _ => return 36;
            };
        }
        _ => return 37;
    };
    return 38;
}
`

// TestRuntimeV2NetHandleStaleCopyReusedFD: a stale copied/reconstructed conn
// handle must not read, write, or close an OS fd number that has been reused
// by a newer connection; each stale op fails with the guard's stable
// NET_ERR_NOT_CONNECTED, and the reusing connection stays fully functional
// (in particular, the stale close must not close(2) the live fd).
func TestRuntimeV2NetHandleStaleCopyReusedFD(t *testing.T) {
	ensureLLVMToolchain(t)
	port := pickFreePort(t)
	source := fmt.Sprintf(netHandleStaleReuseSource, port)
	outputPath := buildLLVMProgramFromSource(t, source)
	for _, shards := range []string{"1", "2", "8"} {
		t.Run("shards-"+shards, func(t *testing.T) {
			env := append(mtEnv(t), "SURGE_SHARDS="+shards, "SURGE_THREADS="+shards)
			dur, res := runBinaryWithTimeout(t, outputPath, env, 15*time.Second)
			if res.exitCode != 0 {
				t.Fatalf("stale-reuse fixture failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
					res.exitCode, dur, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stdout, "ok") {
				t.Fatalf("unexpected stdout: %q", res.stdout)
			}
		})
	}
}

// TestRuntimeV2NetHandleGuardStaticShape pins the guard wiring: the handle
// word carries generation_check, every conn data-path op validates through
// net_conn_op_open, the close path revalidates under the owner lock, and the
// probe resolves the owner shard from the registry instead of reading beyond
// the 8-byte handle word.
func TestRuntimeV2NetHandleGuardStaticShape(t *testing.T) {
	root := repoRoot(t)
	read := func(rel string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(data)
	}
	handles := read("runtime/native/rt_net_handles.h")
	for _, needle := range []string{"generation_check", "handle word"} {
		if !strings.Contains(handles, needle) {
			t.Fatalf("rt_net_handles.h must document the handle-word contract; missing %q", needle)
		}
	}
	netSource := read("runtime/native/rt_net.c")
	for _, fn := range []string{"rt_net_read(", "rt_net_write(", "rt_net_read_bytes(", "rt_net_write_bytes("} {
		idx := strings.Index(netSource, "void* "+fn)
		if idx < 0 {
			t.Fatalf("missing data-path op %s in rt_net.c", fn)
		}
		tail := netSource[idx:]
		end := strings.Index(tail, "\n}")
		if end < 0 {
			end = len(tail)
		}
		if !strings.Contains(tail[:end], "net_conn_op_open(") {
			t.Fatalf("data-path op %s must call the stale-handle guard net_conn_op_open", fn)
		}
	}
	if !strings.Contains(netSource, "rt_net_conn_probe_open(") {
		t.Fatal("rt_net.c must resolve conn owner shards through rt_net_conn_probe_open")
	}
	lifecycle := read("runtime/native/rt_net_lifecycle.c")
	for _, needle := range []string{"rt_fd_registry_handle_check_open", "rt_fd_registry_handle_open"} {
		if !strings.Contains(lifecycle, needle) {
			t.Fatalf("close path must revalidate the handle under the owner lock; missing %q", needle)
		}
	}
	registry := read("runtime/native/rt_fd_registry.c")
	for _, needle := range []string{"fd_index", "rt_fd_registry_handle_check_open"} {
		if !strings.Contains(registry, needle) {
			t.Fatalf("fd registry must provide the O(1) guard lookup; missing %q", needle)
		}
	}
}
