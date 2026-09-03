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
		"rt_net_listener_rollback_unpublished",
		"rt_net_conn_registry_remove",
		"rt_net_conn_free_unpublished",
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
		"rt_net_close_listener": "rt_net_listener_registry_remove(l)",
		"rt_net_close_conn":     "rt_net_conn_registry_remove(c)",
	} {
		body, ok := cFunctionBody(netSource, fn)
		if !ok || !strings.Contains(body, cleanup) {
			t.Fatalf("%s must invalidate its canonical registry row via %s", fn, cleanup)
		}
		if strings.Contains(body, "free_unpublished(") || strings.Contains(body, "rollback_unpublished(") {
			t.Fatalf("%s must not reclaim a published canonical", fn)
		}
	}
	rollback, ok := cFunctionBody(netSource, "net_conn_rollback_unpublished")
	if !ok {
		t.Fatal("net_conn_rollback_unpublished not found")
	}
	rollbackCalls := []string{
		"rt_net_forget_registered_fd_on_owner(",
		"close(conn->fd)",
		"rt_net_conn_free_unpublished(conn)",
	}
	last := -1
	for _, call := range rollbackCalls {
		at := strings.Index(rollback, call)
		if at <= last {
			t.Fatalf("unpublished conn rollback must forget, close, then free; bad %q", call)
		}
		last = at
	}
	for _, fn := range []string{"rt_net_listen", "rt_net_connect", "rt_net_accept"} {
		body, found := cFunctionBody(netSource, fn)
		publishAt := strings.Index(body, "net_make_success_handle(")
		rollbackAt := strings.Index(body[publishAt+1:], "rollback_unpublished(")
		if !found || publishAt < 0 || rollbackAt < 0 {
			t.Fatalf("%s must roll back its unpublished canonical on handle-box failure", fn)
		}
	}
	for fn, successMarkers := range map[string][]string{
		"rt_net_connect": {"rt_net_place_current_task_on_owner("},
		"rt_net_accept": {
			"rt_net_place_current_task_on_owner(",
			"net-accept-success",
			"rt_net_trace_accept_owner(",
		},
	} {
		body, _ := cFunctionBody(netSource, fn)
		publishAt := strings.Index(body, "net_make_success_handle(")
		for _, marker := range successMarkers {
			if at := strings.Index(body, marker); at <= publishAt {
				t.Fatalf("%s success effect %q must follow handle publication", fn, marker)
			}
		}
	}
	resultSource := read("runtime/native/rt_net_result.c")
	handleResult, ok := cFunctionBody(resultSource, "net_make_success_handle")
	if !ok {
		t.Fatal("net_make_success_handle not found")
	}
	// The handle word lives IN the success payload, not in a box the payload
	// points at. rt_net_handles.h says the public handle is "never an OS fd and
	// never a pointer"; publishing a box address made it exactly a pointer, so
	// every by-value entrypoint canonicalised an address where it expects a
	// small id and answered NotConnected — which is why close became a silent
	// no-op that leaked an fd per served request.
	for _, needle := range []string{"memcpy(mem + payload_offset, &handle_id, sizeof(handle_id))"} {
		if !strings.Contains(handleResult, needle) {
			t.Fatalf("the handle id must be written into the payload itself; missing %q", needle)
		}
	}
	for _, forbidden := range []string{"net_make_success_ptr(handle)", "uint64_t* handle ="} {
		if strings.Contains(handleResult, forbidden) {
			t.Fatalf("the handle result still allocates a separate box (%q); the payload must hold the id", forbidden)
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
#include <errno.h>
#include <fcntl.h>
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include "rt_net_handles.c"

static uint64_t free_calls;

void* rt_alloc(uint64_t size, uint64_t align) {
    (void)align;
    return malloc((size_t)(size == 0 ? 1 : size));
}

void rt_free(uint8_t* ptr, uint64_t size, uint64_t align) {
    (void)size;
    (void)align;
    if (ptr != NULL) free_calls++;
    free(ptr);
}

void* rt_realloc(uint8_t* ptr, uint64_t old_size, uint64_t new_size, uint64_t align) {
    (void)old_size;
    (void)align;
    return realloc(ptr, (size_t)(new_size == 0 ? 1 : new_size));
}

int main(void) {
    NetConn* conn = rt_net_conn_alloc(-1, 2, 41);
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
    rt_net_conn_free_unpublished(conn);
    if (rt_net_conn_canonical(&conn_copy) != NULL || free_calls != 2) return 6;

    NetListener* listener = rt_net_listener_alloc(NET_LISTENER_SINGLE, 1, 0);
    if (listener == NULL) return 7;
    int listener_pipe[2];
    if (pipe(listener_pipe) != 0) return 8;
    if (!rt_net_listener_set_member(listener, 0, listener_pipe[0], 0)) return 9;
    uint64_t listener_id = listener->handle_id;
    uint64_t* listener_box = (uint64_t*)rt_alloc(sizeof(uint64_t), _Alignof(uint64_t));
    if (listener_box == NULL) return 10;
    *listener_box = listener_id;
    if ((void*)listener_box == (void*)listener) return 11;
    if (rt_net_listener_canonical((const NetListener*)listener_box) != listener) return 12;
    rt_free((uint8_t*)listener_box, sizeof(uint64_t), _Alignof(uint64_t));

    NetListener listener_copy;
    memset(&listener_copy, 0, sizeof(listener_copy));
    listener_copy.handle_id = listener_id;
    if (rt_net_listener_canonical(&listener_copy) != listener) return 13;
    rt_net_listener_rollback_unpublished(listener);
    if (rt_net_listener_canonical(&listener_copy) != NULL) return 14;
    errno = 0;
    if (fcntl(listener_pipe[0], F_GETFD) != -1 || errno != EBADF) return 15;
    close(listener_pipe[1]);
    return 0;
}
`
	runFDRegistryBehaviorCheck(t, "net handle box/canonical ownership", source)
}

// TestRuntimeV2NetHandleResultAllocationRollback injects failure into the
// allocation net_make_success_handle performs. Since RV2-DEBT-309 a refused
// block is not answered as NULL: the builder hands it to rt_tag_alloc_or_report,
// which ends the process with the refusal reported, and nothing is left to
// roll back. The stand's reporter records the call and unwinds to the driver
// instead of exiting, so the row can read what the contract now promises --
// exactly one attempt, one report, no free -- and the success arm still
// pins one allocation and the id published inline.
func TestRuntimeV2NetHandleResultAllocationRollback(t *testing.T) {
	const source = `
#include <setjmp.h>
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "rt.h"

static uint64_t alloc_calls;
static uint64_t free_calls;
static uint64_t fail_on_alloc;
static uint64_t report_calls;
static jmp_buf report_jump;

void* rt_alloc(uint64_t size, uint64_t align) {
    (void)align;
    alloc_calls++;
    if (alloc_calls == fail_on_alloc) return NULL;
    return malloc((size_t)(size == 0 ? 1 : size));
}

void rt_free(uint8_t* ptr, uint64_t size, uint64_t align) {
    (void)size;
    (void)align;
    if (ptr != NULL) free_calls++;
    free(ptr);
}

size_t rt_tag_payload_offset(size_t payload_align) {
    size_t header = sizeof(uint32_t);
    return (header + payload_align - 1U) & ~(payload_align - 1U);
}

void* rt_tag_alloc(uint32_t tag, size_t payload_align, size_t payload_size) {
    size_t offset = rt_tag_payload_offset(payload_align);
    uint8_t* out = (uint8_t*)rt_alloc(offset + payload_size, payload_align);
    if (out != NULL) memcpy(out, &tag, sizeof(tag));
    return out;
}

void* rt_string_from_bytes(const uint8_t* ptr, uint64_t len) {
    (void)ptr;
    (void)len;
    return NULL;
}

void* rt_biguint_from_u64(uint64_t value) {
    (void)value;
    return NULL;
}

const uint8_t* rt_string_ptr(void* value) {
    (void)value;
    return NULL;
}

uint64_t rt_string_len_bytes(void* value) {
    (void)value;
    return 0;
}

// The reporters the builders reach for a refused block. In the runtime they
// end the process; here they count the report and unwind to run_failure,
// which is the only way a stand can read "reported, not answered" and go on.
void* rt_alloc_or_report(uint64_t size, uint64_t align, const uint8_t* message, uint64_t length) {
    (void)message;
    (void)length;
    void* out = rt_alloc(size, align);
    if (out == NULL) {
        report_calls++;
        longjmp(report_jump, 1);
    }
    return out;
}

void* rt_tag_alloc_or_report(uint32_t tag, size_t payload_align, size_t payload_size,
                             const uint8_t* message, uint64_t length) {
    (void)message;
    (void)length;
    void* out = rt_tag_alloc(tag, payload_align, payload_size);
    if (out == NULL) {
        report_calls++;
        longjmp(report_jump, 1);
    }
    return out;
}

#include "rt_net_result.c"

static int run_failure(uint64_t fail_at, uint64_t want_allocs, uint64_t want_frees) {
    alloc_calls = 0;
    free_calls = 0;
    report_calls = 0;
    fail_on_alloc = fail_at;
    if (setjmp(report_jump) == 0) {
        // The builder must not come back with an answer: the refusal is
        // reported, and the report is what unwinds us here.
        (void)net_make_success_handle(73);
        return 1;
    }
    if (report_calls != 1) return 6;
    if (alloc_calls != want_allocs) return 2;
    if (free_calls != want_frees) return 3;
    return 0;
}

int main(void) {
    // ONE allocation to fail, ONE report, and nothing to roll back when it
    // does. The second arm used to assert the rollback of a separate handle
    // box: fail the second allocation, expect two attempts and one free. There
    // is no second allocation now, because the id lives in the payload the
    // first one returned — so the rollback it tested no longer has anything to
    // undo, and asserting it would pin the representation this change removes.
    if (run_failure(1, 1, 0) != 0) return 1;

    alloc_calls = 0;
    free_calls = 0;
    fail_on_alloc = 0;
    void* result = net_make_success_handle(UINT64_C(0x123456789abcdef0));
    // ONE allocation: the tagged result itself. A second would be the box this
    // representation exists to remove.
    if (result == NULL || alloc_calls != 1 || free_calls != 0) return 3;
    size_t offset = rt_tag_payload_offset(_Alignof(void*));
    // The payload IS the id, read directly rather than dereferenced.
    uint64_t published = 0;
    memcpy(&published, (uint8_t*)result + offset, sizeof(published));
    if (published != UINT64_C(0x123456789abcdef0)) return 4;
    rt_free((uint8_t*)result, 1, _Alignof(void*));
    // ONE free, matching the one allocation. It was two while the box existed:
    // the payload and the box it pointed at.
    if (free_calls != 1) return 5;
    return 0;
}
`
	runFDRegistryBehaviorCheck(t, "net handle result allocation rollback", source)
}
