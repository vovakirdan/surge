//go:build runtime_v2_pending

package vm_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runFDRegistryStaticCheck compiles one C snippet against runtime/native with
// the strict flags shared by the Runtime V2 static gates. clang is required
// (not skipped) because this file is opt-in via the runtime_v2_pending tag and
// exists only to be a hard gate. Failures surface the full clang output so the
// expected-red shape gate records actionable errors in the evidence ledger.
func runFDRegistryStaticCheck(t *testing.T, label, source string) {
	t.Helper()
	root := repoRoot(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("clang is required for Runtime V2 pending static check: %v", err)
	}

	cmd := exec.Command(
		clang,
		"-std=c11",
		"-Wall",
		"-Wextra",
		"-Werror",
		"-fsyntax-only",
		"-I"+filepath.Join(root, "runtime", "native"),
		"-x",
		"c",
		"-",
	)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(source)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed:\n%s", label, output)
	}
}

// TestRuntimeV2FDRegistryStaticShape guards the Epic 4 Task 5 fd registry
// skeleton contract. It is EXPECTED RED until Task 5 lands exactly this shape,
// reachable from rt_async_internal.h:
//
//	typedef enum {
//	    RT_FD_CLOSE_STATE_OPEN = 0,
//	    RT_FD_CLOSE_STATE_CLOSED = 1,
//	} rt_fd_close_state;
//	typedef struct {
//	    int fd;
//	    uint64_t generation;
//	    uint8_t close_state; // holds rt_fd_close_state values (rt_task.status pattern)
//	    uint8_t want_accept;
//	    uint8_t want_read;
//	    uint8_t want_write;
//	} rt_fd_entry;
//	typedef struct {
//	    rt_fd_entry* entries;
//	    size_t len;
//	    size_t cap;
//	} rt_fd_registry;
//	// rt_shard gains a by-value field: rt_fd_registry fd_registry;
//	rt_fd_registry* rt_shard_fd_registry(rt_shard* shard);
//	const rt_fd_registry* rt_shard_fd_registry_const(const rt_shard* shard);
//	rt_fd_registry* rt_executor_fd_registry(rt_executor* ex);
//	const rt_fd_registry* rt_executor_fd_registry_const(const rt_executor* ex);
//	rt_runtime_status rt_fd_registry_init(rt_fd_registry* registry);
//	void rt_fd_registry_free(rt_fd_registry* registry);
//	rt_runtime_status rt_fd_registry_ensure_cap(rt_fd_registry* registry);
//	size_t rt_fd_registry_len(const rt_fd_registry* registry);
//	const rt_fd_entry* rt_fd_registry_find_const(const rt_fd_registry* registry, int fd);
//
// Task 6 extended the guard with the registration-side interest mutators:
//
//	rt_runtime_status rt_fd_registry_attach_net_interest(rt_fd_registry* registry, waker_key key);
//	void rt_fd_registry_detach_net_interest(rt_fd_registry* registry, waker_key key);
//
// Task 7 extended the guard with the poll-input reads (the registry is the
// only poll-set source; the snapshot row is the ex->lock-held copy that
// poll() and completion run against):
//
//	int rt_fd_registry_net_interest_present(const rt_fd_registry* registry, waker_key key);
//	size_t rt_fd_registry_snapshot_poll_interest(const rt_fd_registry* registry,
//	                                             rt_fd_poll_interest* out, size_t out_cap);
//
// The remaining mutation surface (close/generation) is deliberately not
// pinned here; Task 9 extends this guard when it implements it.
func TestRuntimeV2FDRegistryStaticShape(t *testing.T) {
	source := `
#include "rt_async_internal.h"

// Owner accessors: shard-first ownership with shard0-routing executor
// compatibility adapters, mirroring rt_shard_waiter_store / rt_executor_waiter_store.
rt_fd_registry* (*runtime_v2_check_shard_fd_registry)(rt_shard*) = rt_shard_fd_registry;
const rt_fd_registry* (*runtime_v2_check_shard_fd_registry_const)(const rt_shard*) = rt_shard_fd_registry_const;
rt_fd_registry* (*runtime_v2_check_executor_fd_registry)(rt_executor*) = rt_executor_fd_registry;
const rt_fd_registry* (*runtime_v2_check_executor_fd_registry_const)(const rt_executor*) = rt_executor_fd_registry_const;

// Owner-first lifecycle and queries with explicit status codes for
// recoverable failures (Global Rule 8: no plain bool, no panic_msg).
rt_runtime_status (*runtime_v2_check_fd_registry_init)(rt_fd_registry*) = rt_fd_registry_init;
void (*runtime_v2_check_fd_registry_free)(rt_fd_registry*) = rt_fd_registry_free;
rt_runtime_status (*runtime_v2_check_fd_registry_ensure_cap)(rt_fd_registry*) = rt_fd_registry_ensure_cap;
size_t (*runtime_v2_check_fd_registry_len)(const rt_fd_registry*) = rt_fd_registry_len;
const rt_fd_entry* (*runtime_v2_check_fd_registry_find_const)(const rt_fd_registry*, int) = rt_fd_registry_find_const;

// Task 6 registration-side interest mutators, driven by the waiter-store
// bridge in rt_async_waiter.c under ex->lock. Attach returns explicit status
// (allocation can fail on row creation); detach is the caller-proved
// last-waiter path and cannot fail in a way callers act on.
rt_runtime_status (*runtime_v2_check_fd_registry_attach_net_interest)(rt_fd_registry*, waker_key) = rt_fd_registry_attach_net_interest;
void (*runtime_v2_check_fd_registry_detach_net_interest)(rt_fd_registry*, waker_key) = rt_fd_registry_detach_net_interest;

// Task 7 poll-input reads: interest-present resolves the attach-miss bridge
// after prepare_park; the snapshot copies rows into the shard poll scratch
// under ex->lock as the only poll-set source (no waiter-store scan).
int (*runtime_v2_check_fd_registry_net_interest_present)(const rt_fd_registry*, waker_key) = rt_fd_registry_net_interest_present;
size_t (*runtime_v2_check_fd_registry_snapshot_poll_interest)(
    const rt_fd_registry*, rt_fd_poll_interest*, size_t) = rt_fd_registry_snapshot_poll_interest;

// Poll snapshot row: fd plus readable-class (read|accept folded) and write
// interest. Completion fan-out to separate read/accept keys stays in rt_net.c.
_Static_assert(sizeof(((rt_fd_poll_interest*)0)->fd) == sizeof(int), "rt_fd_poll_interest.fd must stay int");
_Static_assert(sizeof(((rt_fd_poll_interest*)0)->want_read) == sizeof(uint8_t), "rt_fd_poll_interest.want_read must stay byte-sized");
_Static_assert(sizeof(((rt_fd_poll_interest*)0)->want_write) == sizeof(uint8_t), "rt_fd_poll_interest.want_write must stay byte-sized");

// One durable entry per live fd: fd number, generation stale-wake guard,
// close state, and accept/read/write interest bytes. Accept stays distinct
// from read because WAKER_NET_ACCEPT and WAKER_NET_READ waiters must be woken
// separately on close and cancel.
_Static_assert(sizeof(((rt_fd_entry*)0)->fd) == sizeof(int), "rt_fd_entry.fd must stay int");
_Static_assert(sizeof(((rt_fd_entry*)0)->generation) == sizeof(uint64_t), "rt_fd_entry.generation must stay uint64_t");
_Static_assert(sizeof(((rt_fd_entry*)0)->close_state) == sizeof(uint8_t), "rt_fd_entry.close_state must stay byte-sized");
_Static_assert(sizeof(((rt_fd_entry*)0)->want_accept) == sizeof(uint8_t), "rt_fd_entry.want_accept must stay byte-sized");
_Static_assert(sizeof(((rt_fd_entry*)0)->want_read) == sizeof(uint8_t), "rt_fd_entry.want_read must stay byte-sized");
_Static_assert(sizeof(((rt_fd_entry*)0)->want_write) == sizeof(uint8_t), "rt_fd_entry.want_write must stay byte-sized");

// Shard-local growable container mirroring rt_waiter_store, owned by value
// on rt_shard beside net_poll_scratch.
_Static_assert(sizeof(rt_fd_registry) > 0, "rt_fd_registry must be complete");
_Static_assert(sizeof(((rt_fd_registry*)0)->entries) == sizeof(rt_fd_entry*), "rt_fd_registry.entries must store rt_fd_entry rows");
_Static_assert(sizeof(((rt_fd_registry*)0)->len) == sizeof(size_t), "rt_fd_registry.len must stay size_t");
_Static_assert(sizeof(((rt_fd_registry*)0)->cap) == sizeof(size_t), "rt_fd_registry.cap must stay size_t");
_Static_assert(sizeof(((rt_shard*)0)->fd_registry) == sizeof(rt_fd_registry), "rt_shard.fd_registry must own registry storage by value");

// Close-state codes stay explicit; the implementation may add states (e.g.
// CLOSING) without breaking this guard.
_Static_assert(RT_FD_CLOSE_STATE_OPEN == 0, "rt_fd_close_state OPEN must stay 0");
_Static_assert(RT_FD_CLOSE_STATE_CLOSED != RT_FD_CLOSE_STATE_OPEN, "rt_fd_close_state CLOSED must stay distinct from OPEN");
_Static_assert(RT_RUNTIME_STATUS_OK == 0, "rt_runtime_status OK must stay 0");
`

	runFDRegistryStaticCheck(t, "Runtime V2 fd registry static shape check", source)
}

// TestRuntimeV2FDRegistryStaticBoundary proves the current approved
// placeholder still holds: shard-owned poll scratch, stable io-loop and
// wake-fd entry points, net waker keys as the wake currency, explicit
// rt_runtime_status codes, and the N=1 runtime shape. This must stay GREEN
// through Tasks 5-11; it deliberately does not duplicate the waiter-store
// surface already pinned by TestRuntimeV2WaiterHelperStaticBoundary.
func TestRuntimeV2FDRegistryStaticBoundary(t *testing.T) {
	source := `
#include "rt_async_internal.h"

#ifndef RT_RUNTIME_SHARD_COUNT
#error "RT_RUNTIME_SHARD_COUNT must define the N=1 runtime shape"
#endif

#if RT_RUNTIME_SHARD_COUNT != 1
#error "Epic 4 fd registry work assumes exactly one shard"
#endif

// Poll scratch stays reachable through the shard-first owner path; the FD
// Registry Contract keeps it alive as registry-derived scratch.
rt_net_poll_scratch* (*runtime_v2_check_shard_net_poll_scratch)(rt_shard*) = rt_shard_net_poll_scratch;
rt_net_poll_scratch* (*runtime_v2_check_executor_net_poll_scratch)(rt_executor*) = rt_executor_net_poll_scratch;

// io-loop entry and wake-fd surface whose signatures must stay stable while
// Tasks 6-11 reroute their internals through the registry.
int (*runtime_v2_check_poll_net_waiters)(rt_executor*, int) = poll_net_waiters;
void (*runtime_v2_check_net_wake_poll)(void) = rt_net_wake_poll;

// Net waker keys remain the wake currency between the registry and the
// waiter store.
waker_key (*runtime_v2_check_net_accept_key)(int) = net_accept_key;
waker_key (*runtime_v2_check_net_read_key)(int) = net_read_key;
waker_key (*runtime_v2_check_net_write_key)(int) = net_write_key;
int (*runtime_v2_check_waker_is_net)(waker_key) = waker_is_net;

_Static_assert(sizeof(((rt_shard*)0)->net_poll_scratch) == sizeof(rt_net_poll_scratch), "rt_shard.net_poll_scratch must own poll scratch by value");
_Static_assert(sizeof(((rt_net_poll_scratch*)0)->fds_cap) == sizeof(size_t), "rt_net_poll_scratch.fds_cap must stay size_t");
_Static_assert(sizeof(((rt_net_poll_scratch*)0)->pfds_cap) == sizeof(size_t), "rt_net_poll_scratch.pfds_cap must stay size_t");

// Recoverable-failure statuses stay explicit and distinct (Global Rule 8).
_Static_assert(RT_RUNTIME_STATUS_OK == 0, "rt_runtime_status OK must stay 0");
_Static_assert(RT_RUNTIME_STATUS_INVALID_ARGUMENT != RT_RUNTIME_STATUS_OK, "invalid-argument status must stay distinct from OK");
_Static_assert(RT_RUNTIME_STATUS_ALLOCATION_FAILED != RT_RUNTIME_STATUS_OK, "allocation-failed status must stay distinct from OK");
`

	runFDRegistryStaticCheck(t, "Runtime V2 fd registry static boundary check", source)
}
