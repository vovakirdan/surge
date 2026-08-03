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

// (RV2-DEBT-010) behavior and static gates for the copied
// net-handle contract. A public TcpConn may be a reconstructed 8-byte box
// carrying only the stable runtime handle id; every data-path op resolves that
// id through the handle table before touching fd, owner, closed, or generation
// fields.

// netHandleStaleReuseSource is self-contained: it proves that a copied
// accepted-connection handle taken before close cannot act after close, even
// after a later connection opens. The proof is cross-platform: it does not
// depend on Linux fd reuse order. Every stale op (read, write, close) must fail
// with exactly NET_ERR_NOT_CONNECTED(5) because the handle id was removed from
// the runtime handle table. The load-bearing clause is the stale close: it must
// not close the newer accepted connection, proven by that connection still
// writing a probe byte afterwards. __opaque values are raw handle words, not
// bignum ints, so the program only moves them (no arithmetic or comparison).
// Distinct exit codes identify the first failed expectation.
const netHandleStaleReuseSource = `import stdlib/net as net;

fn probe() -> byte[] {
    let mut out: byte[] = [];
    out.push(88:byte);
    return out;
}

async fn run() -> int {
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
	                    let stale_handle: int = a1_handle;
	                    let stale_live: TcpConn = { __opaque: stale_handle };
	                    let close1_res = net.close_conn(own stale_live);
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

	                            let a2_conn: TcpConn = { __opaque: a2_handle };
	                            let cwrite_res = net.write_all(&a2_conn, probe()).await();
	                            let cwrite_ok: bool = compare cwrite_res {
	                                Success(res) => compare res { Success(_) => true; _ => false; };
	                                Cancelled() => false;
	                            };
	                            if !cwrite_ok { return 25; }
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

@entrypoint
fn main() -> int {
    let res = run().await();
    return compare res {
        Success(code) => code;
        Cancelled() => 99;
    };
}
`

// TestRuntimeV2NetHandleStaleCopyReusedFD: a stale copied/reconstructed conn
// handle must not read, write, or close after its handle id is closed; each
// stale op fails with the guard's stable NET_ERR_NOT_CONNECTED, and a newer
// connection stays functional (in particular, the stale close must not close
// the live fd).
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
// word carries a stable runtime handle id, every conn data-path op validates
// through net_conn_op_open, the close path revalidates under the owner lock,
// and no conn path reads beyond the handle word before canonical lookup.
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
	for _, needle := range []string{
		"handle id",
		"never an OS fd",
		"dropping a public box",
		"rt_net_conn_canonical",
	} {
		if !strings.Contains(handles, needle) {
			t.Fatalf("rt_net_handles.h must document the handle-word contract; missing %q", needle)
		}
	}
	handleSource := read("runtime/native/rt_net_handles.c")
	for _, needle := range []string{
		"net_handle_registry_add",
		"net_handle_registry_lookup",
		"rt_net_conn_free",
		"NET_HANDLE_CONN",
	} {
		if !strings.Contains(handleSource, needle) {
			t.Fatalf("rt_net_handles.c must own the stable handle table; missing %q", needle)
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
	for _, needle := range []string{"rt_net_conn_canonical", "net_conn_owner_local", "rt_net_handle_open_on_owner"} {
		if !strings.Contains(netSource, needle) {
			t.Fatalf("rt_net.c must canonicalize and validate conn handles; missing %q", needle)
		}
	}
	if strings.Contains(netSource, "rt_net_conn_probe_open(") {
		t.Fatal("rt_net.c must not use fd-scanning conn probes after stable handle ids")
	}
	for _, direct := range []string{
		"net_make_success_ptr(listener)",
		"net_make_success_ptr(conn)",
	} {
		if strings.Contains(netSource, direct) {
			t.Fatalf("native canonical objects must not escape as language boxes: found %q", direct)
		}
	}
	if strings.Count(netSource, "net_make_success_handle(conn->handle_id)") != 2 ||
		strings.Count(netSource, "net_make_success_handle(listener->handle_id)") != 1 {
		t.Fatal("listen/connect/accept must export separate language-owned handle boxes")
	}
	for fn, cleanup := range map[string]string{
		"rt_net_close_listener": "rt_net_listener_free(l)",
		"rt_net_close_conn":     "rt_net_conn_free(c)",
	} {
		body, ok := cFunctionBody(netSource, fn)
		if !ok || !strings.Contains(body, cleanup) {
			t.Fatalf("%s must release its registry-owned canonical object via %s", fn, cleanup)
		}
	}
	resultSource := read("runtime/native/rt_net_result.c")
	handleResult, ok := cFunctionBody(resultSource, "net_make_success_handle")
	if !ok {
		t.Fatal("net_make_success_handle not found")
	}
	for _, needle := range []string{"sizeof(uint64_t)", "net_make_success_ptr(handle)", "rt_free("} {
		if !strings.Contains(handleResult, needle) {
			t.Fatalf("handle result must own an independent one-word box; missing %q", needle)
		}
	}
	lifecycle := read("runtime/native/rt_net_lifecycle.c")
	for _, needle := range []string{"rt_fd_registry_handle_open"} {
		if !strings.Contains(lifecycle, needle) {
			t.Fatalf("close path must revalidate the handle under the owner lock; missing %q", needle)
		}
	}
	if strings.Contains(lifecycle, "rt_fd_registry_handle_check_open") {
		t.Fatal("close path must not use the removed 16-bit generation check")
	}
	registry := read("runtime/native/rt_fd_registry.c")
	for _, needle := range []string{"fd_index", "rt_fd_registry_handle_open"} {
		if !strings.Contains(registry, needle) {
			t.Fatalf("fd registry must provide the O(1) guard lookup; missing %q", needle)
		}
	}
	if strings.Contains(registry, "rt_fd_registry_handle_check_open") {
		t.Fatal("fd registry must not keep the removed 16-bit public-handle predicate")
	}
}

// TestRuntimeV2NetHandleCanonicalOutlivesPublicBox proves the ownership half
// of the handle-word ABI without sockets or scheduler timing: discarding an
// 8-byte public box leaves the registry-owned canonical object live, explicit
// canonical teardown rejects a reconstructed stale copy, and listener handles
// follow the same split as connection handles.
func TestRuntimeV2NetHandleCanonicalOutlivesPublicBox(t *testing.T) {
	const source = `
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "rt_net_handles.c"

void* rt_alloc(uint64_t size, uint64_t align) {
    (void)align;
    return malloc((size_t)(size == 0 ? 1 : size));
}

void rt_free(uint8_t* ptr, uint64_t size, uint64_t align) {
    (void)size;
    (void)align;
    free(ptr);
}

void* rt_realloc(uint8_t* ptr, uint64_t old_size, uint64_t new_size, uint64_t align) {
    (void)old_size;
    (void)align;
    return realloc(ptr, (size_t)(new_size == 0 ? 1 : new_size));
}

int main(void) {
    NetConn* conn = rt_net_conn_alloc(17, 2, 41);
    if (conn == NULL) return 1;
    uint64_t conn_id = conn->handle_id;
    uint64_t* conn_box = (uint64_t*)rt_alloc(sizeof(uint64_t), _Alignof(uint64_t));
    if (conn_box == NULL) return 2;
    *conn_box = conn_id;
    if ((void*)conn_box == (void*)conn) return 3;
    if (rt_net_conn_canonical((const NetConn*)conn_box) != conn) return 4;
    rt_free((uint8_t*)conn_box, sizeof(uint64_t), _Alignof(uint64_t));

    NetConn conn_copy;
    memset(&conn_copy, 0, sizeof(conn_copy));
    conn_copy.handle_id = conn_id;
    if (rt_net_conn_canonical(&conn_copy) != conn) return 5;
    rt_net_conn_free(conn);
    if (rt_net_conn_canonical(&conn_copy) != NULL) return 6;

    NetListener* listener = rt_net_listener_alloc(NET_LISTENER_SINGLE, 1, 0);
    if (listener == NULL) return 7;
    uint64_t listener_id = listener->handle_id;
    uint64_t* listener_box = (uint64_t*)rt_alloc(sizeof(uint64_t), _Alignof(uint64_t));
    if (listener_box == NULL) return 8;
    *listener_box = listener_id;
    if ((void*)listener_box == (void*)listener) return 9;
    if (rt_net_listener_canonical((const NetListener*)listener_box) != listener) return 10;
    rt_free((uint8_t*)listener_box, sizeof(uint64_t), _Alignof(uint64_t));

    NetListener listener_copy;
    memset(&listener_copy, 0, sizeof(listener_copy));
    listener_copy.handle_id = listener_id;
    if (rt_net_listener_canonical(&listener_copy) != listener) return 11;
    rt_net_listener_free(listener);
    if (rt_net_listener_canonical(&listener_copy) != NULL) return 12;
    return 0;
}
`
	runFDRegistryBehaviorCheck(t, "net handle box/canonical ownership", source)
}
