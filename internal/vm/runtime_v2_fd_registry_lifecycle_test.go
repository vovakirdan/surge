//go:build runtime_v2_pending

package vm_test

import (
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func requireNoFDRegistryDebugMismatch(t *testing.T, stderr string) {
	t.Helper()
	for _, needle := range []string{"fd-registry-bridge mismatch", "fd-registry-attach-miss"} {
		if strings.Contains(stderr, needle) {
			t.Fatalf("unexpected fd registry debug line %q in stderr:\n%s", needle, stderr)
		}
	}
}

const fdRegistryCancelDuplicateReadBody = `
fn ack() -> byte[] {
    let mut out: byte[] = [];
    out.push(75:byte);
    return out;
}

async fn read_and_ack(handle: int) -> int {
    let read_task = read_nonempty(handle).await();
    let read_ok: bool = compare read_task { Success(flag) => flag; Cancelled() => false; };
    if !read_ok { return 1; }
    let write_task = write_all_ok(handle, ack()).await();
    let write_ok: bool = compare write_task { Success(flag) => flag; Cancelled() => false; };
    if !write_ok { return 2; }
    return 0;
}

async fn serve_one(listener: TcpListener, arg: uint) -> int {
    let _ = arg;
    let accept_res = net.accept(&listener).await();
    compare accept_res {
        Success(conn_res) => {
            compare conn_res {
                Success(conn) => {
                    let handle: int = conn.__opaque;
                    let first = spawn read_and_ack(handle);
                    checkpoint().await();
                    let second = spawn read_and_ack(handle);
                    checkpoint().await();
                    checkpoint().await();
                    first.cancel();
                    let first_res = first.await();
                    let first_cancelled: bool = compare first_res { Success(_) => false; Cancelled() => true; };
                    if !first_cancelled { let _ = net.close_conn(own conn); return 2; }
                    let second_res = second.await();
                    let second_code = compare second_res { Success(code) => code; Cancelled() => 90; };
                    if second_code != 0 { let _ = net.close_conn(own conn); return 3; }

                    let third = spawn read_and_ack(handle);
                    checkpoint().await();
                    checkpoint().await();
                    let third_res = third.await();
                    let third_code = compare third_res { Success(code) => code; Cancelled() => 91; };
                    let _ = net.close_conn(own conn);
                    if third_code != 0 { return 4; }
                    print("ok");
                    return 0;
                }
                _ => return 1;
            };
        }
        Cancelled() => return 1;
    };
    return 1;
}
`

func TestRuntimeV2FDRegistryCancelledDuplicateReadWaiterPreservesLiveAndReregister(t *testing.T) {
	ensureLLVMToolchain(t)
	port := pickFreePort(t)
	source := fdRegistryPrelude + fdRegistryCancelDuplicateReadBody + fdRegistryMain
	env := overrideEnvVar(overrideEnvVar(mtEnv(t), "SURGE_THREADS", "1"), "SURGE_ASYNC_DEBUG", "1")
	srv := startFDRegistryServer(t, source, env, strconv.Itoa(port), "0")

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		srv.fail("dial cancel duplicate read server: %v", err)
	}
	if err = conn.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		_ = conn.Close()
		srv.fail("set deadline: %v", err)
	}
	for i, b := range []byte{'A', 'B'} {
		time.Sleep(300 * time.Millisecond)
		if _, err = conn.Write([]byte{b}); err != nil {
			_ = conn.Close()
			srv.fail("write release byte %d: %v", i, err)
		}
		var ackBuf [1]byte
		if _, err = io.ReadFull(conn, ackBuf[:]); err != nil {
			_ = conn.Close()
			srv.fail("read ack %d: %v", i, err)
		}
		if ackBuf[0] != 'K' {
			_ = conn.Close()
			srv.fail("unexpected ack %d: %q", i, string(ackBuf[:]))
		}
	}
	_ = conn.Close()

	stderr := srv.waitExitZero(10 * time.Second)
	requireFDRegistryExitTrace(t, stderr, 2, 2)
	requireNoFDRegistryDebugMismatch(t, stderr)
}

const fdRegistryCancelReadPreserveWriteBody = `
async fn read_one(handle: int) -> int {
    let read_task = read_nonempty(handle).await();
    let read_ok: bool = compare read_task { Success(flag) => flag; Cancelled() => false; };
    if !read_ok { return 1; }
    return 0;
}

async fn write_bulk(handle: int, total: uint) -> int {
    let payload: byte[] = Array::<byte>.with_len(total);
    let write_task = write_all_ok(handle, payload).await();
    let write_ok: bool = compare write_task { Success(flag) => flag; Cancelled() => false; };
    if !write_ok { return 1; }
    return 0;
}

async fn serve_one(listener: TcpListener, arg: uint) -> int {
    let accept_res = net.accept(&listener).await();
    compare accept_res {
        Success(conn_res) => {
            compare conn_res {
                Success(conn) => {
                    let handle: int = conn.__opaque;
                    let reader = spawn read_one(handle);
                    checkpoint().await();
                    let writer = spawn write_bulk(handle, arg);
                    checkpoint().await();
                    checkpoint().await();
                    reader.cancel();
                    let reader_res = reader.await();
                    let reader_cancelled: bool = compare reader_res { Success(_) => false; Cancelled() => true; };
                    if !reader_cancelled { let _ = net.close_conn(own conn); return 2; }
                    let writer_res = writer.await();
                    let writer_code = compare writer_res { Success(code) => code; Cancelled() => 90; };
                    let _ = net.close_conn(own conn);
                    if writer_code != 0 { return 3; }
                    print("ok");
                    return 0;
                }
                _ => return 1;
            };
        }
        Cancelled() => return 1;
    };
    return 1;
}
`

func TestRuntimeV2FDRegistryCancelledReadInterestPreservesWriteInterest(t *testing.T) {
	ensureLLVMToolchain(t)
	port := pickFreePort(t)
	const payloadLen = 32 << 20
	source := fdRegistryPrelude + fdRegistryCancelReadPreserveWriteBody + fdRegistryMain
	env := overrideEnvVar(overrideEnvVar(mtEnv(t), "SURGE_THREADS", "1"), "SURGE_ASYNC_DEBUG", "1")
	srv := startFDRegistryServer(t, source, env, strconv.Itoa(port), strconv.Itoa(payloadLen))

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		srv.fail("dial cancel read/write server: %v", err)
	}
	if err = conn.SetDeadline(time.Now().Add(60 * time.Second)); err != nil {
		_ = conn.Close()
		srv.fail("set deadline: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err = io.CopyN(io.Discard, conn, payloadLen); err != nil {
		_ = conn.Close()
		srv.fail("drain %d payload bytes: %v", payloadLen, err)
	}
	_ = conn.Close()

	stderr := srv.waitExitZero(30 * time.Second)
	requireFDRegistryExitTrace(t, stderr, 2, 1)
	requireNoFDRegistryDebugMismatch(t, stderr)
}

const fdRegistryCloseAcceptBody = `import stdlib/net as net;
async fn accept_error_after_close(handle: int) -> uint {
    let listener: TcpListener = { __opaque: handle };
    let accept_task = net.accept(&listener).await();
    return compare accept_task {
        Success(res) => compare res { Success(_) => 0:uint; err => err.code; };
        Cancelled() => 900:uint;
    };
}

async fn serve_one(listener: TcpListener, arg: uint) -> int {
    let _ = arg;
    let handle: int = listener.__opaque;
    let waiter = spawn accept_error_after_close(handle);
    checkpoint().await();
    checkpoint().await();
    sleep(20:uint).await();
    let close_res = net.close_listener(own listener);
    let close_ok: bool = compare close_res { Success(_) => true; _ => false; };
    if !close_ok { return 2; }
    let waiter_clone = waiter.clone();
    let wait_res = timeout(waiter_clone, 200);
    let code: uint = compare wait_res { Success(v) => v; Cancelled() => 901:uint; };
    let waiter_done = waiter.await(); let _ = waiter_done;
    if code >= 900:uint { print("accept_close_timeout"); return 3; }
    if code < 2:uint { print("accept_close_bad_code"); return 4; }
    print("ok");
    return 0;
}
`

func TestRuntimeV2FDRegistryCloseWakesParkedAcceptWaiter(t *testing.T) {
	ensureLLVMToolchain(t)
	port := pickFreePort(t)
	source := fdRegistryCloseAcceptBody + fdRegistryMain
	env := overrideEnvVar(mtEnv(t), "SURGE_ASYNC_DEBUG", "1")
	srv := startFDRegistryServer(t, source, env, strconv.Itoa(port), "0")

	stderr := srv.waitExitZero(10 * time.Second)
	requireNoFDRegistryDebugMismatch(t, stderr)
}

const fdRegistryCloseReadBody = `import stdlib/net as net;
async fn read_error_after_close(handle: int) -> uint {
    let conn: TcpConn = { __opaque: handle };
    let read_task = net.read_some(&conn, 1:uint).await();
    return compare read_task {
        Success(res) => compare res { Success(_) => 0:uint; err => err.code; };
        Cancelled() => 900:uint;
    };
}

async fn serve_one(listener: TcpListener, arg: uint) -> int {
    let _ = arg;
    let accept_res = net.accept(&listener).await();
    compare accept_res {
        Success(conn_res) => {
            compare conn_res {
                Success(conn) => {
                    let handle: int = conn.__opaque;
                    let waiter = spawn read_error_after_close(handle);
                    checkpoint().await();
                    checkpoint().await();
                    sleep(20:uint).await();
                    let close_res = net.close_conn(own conn);
                    let close_ok: bool = compare close_res { Success(_) => true; _ => false; };
                    if !close_ok { return 2; }
                    let waiter_clone = waiter.clone();
                    let wait_res = timeout(waiter_clone, 200);
                    let code: uint = compare wait_res { Success(v) => v; Cancelled() => 901:uint; };
                    let waiter_done = waiter.await(); let _ = waiter_done;
                    if code >= 900:uint { print("read_close_timeout"); return 3; }
                    if code < 2:uint { print("read_close_bad_code"); return 4; }
                    print("ok");
                    return 0;
                }
                _ => return 1;
            };
        }
        Cancelled() => return 1;
    };
    return 1;
}
`

func TestRuntimeV2FDRegistryCloseWakesParkedReadWaiter(t *testing.T) {
	ensureLLVMToolchain(t)
	port := pickFreePort(t)
	source := fdRegistryCloseReadBody + fdRegistryMain
	env := overrideEnvVar(mtEnv(t), "SURGE_ASYNC_DEBUG", "1")
	srv := startFDRegistryServer(t, source, env, strconv.Itoa(port), "0")

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := dialWithRetry(addr, time.Now().Add(10*time.Second))
	if err != nil {
		srv.fail("dial close read server: %v", err)
	}
	defer conn.Close()

	stderr := srv.waitExitZero(10 * time.Second)
	requireNoFDRegistryDebugMismatch(t, stderr)
}

// TestRuntimeV2FDRegistryHandleWordPublishedInline pins the shape that makes
// close reachable at all. A Surge composite is a run of bytes that lives where
// it was declared, so the success payload slot of NetResult<TcpConn> IS the one
// word TcpConn exposes as __opaque, and rt_net_handles.h says that word is a
// handle id and "never a pointer". If net_make_success_handle publishes the
// address of a side box instead, every by-value entrypoint -- close_conn and
// close_listener are the only two -- reads that address where the handle table
// expects a small id, misses, and answers NET_ERR_NOT_CONNECTED without
// touching the fd. Close then becomes a silent no-op that leaks a descriptor
// per served request, which is why this is an fd-registry liveness gate and not
// only a handle-ABI one.
func TestRuntimeV2FDRegistryHandleWordPublishedInline(t *testing.T) {
	const source = `
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "rt.h"

static uint64_t alloc_calls;
static uint64_t free_calls;

void* rt_alloc(uint64_t size, uint64_t align) {
    (void)align;
    alloc_calls++;
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

#include "rt_net_result.c"

int main(void) {
    const uint64_t id = UINT64_C(0x123456789abcdef0);
    alloc_calls = 0;
    free_calls = 0;
    void* result = net_make_success_handle(id);
    if (result == NULL) return 1;
    size_t offset = rt_tag_payload_offset(_Alignof(void*));
    uint64_t word = 0;
    memcpy(&word, (uint8_t*)result + offset, sizeof(word));
    // The payload slot is the struct's own storage: its first word must be the
    // handle id itself, never the address of a box holding it.
    if (word != id) return 2;
    // Exactly one allocation, the tag box. A second one would be a box no
    // language-side drop owns, so it would leak once per published handle.
    if (alloc_calls != 1) return 3;
    if (free_calls != 0) return 4;
    rt_free((uint8_t*)result, 1, _Alignof(void*));
    if (free_calls != 1) return 5;
    return 0;
}
`
	runFDRegistryBehaviorCheck(t, "net handle word published inline", source)
}
