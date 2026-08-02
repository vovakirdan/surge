//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestRuntimeV2RemotePublicationAPIShape(t *testing.T) {
	source := `
#include <stdint.h>
#include "rt_async_internal.h"
#include "rt_far_channel.h"
#include "rt_remote_spawn.h"
#include "rt_remote_task.h"

rt_remote_spawn_status (*check_publish)(uint32_t, uint64_t, uint64_t, int64_t, void*,
    rt_remote_spawn_pending**, rt_far_task_handle*) = rt_remote_spawn_publish;
rt_remote_spawn_status (*check_publish_placement)(rt_placement, uint64_t, uint64_t, int64_t, void*,
    rt_remote_spawn_pending**, rt_far_task_handle*) = rt_remote_spawn_publish_placement;
rt_remote_spawn_status (*check_validate)(rt_executor*, const rt_far_task_handle*) =
    rt_remote_spawn_handle_validate;
size_t (*check_drain)(rt_executor*, rt_shard*, size_t) =
    rt_remote_spawn_drain_inbound_locked;
rt_remote_task_status (*check_far_await)(const rt_far_task_handle*, uint64_t,
    rt_remote_task_pending**, uint8_t*, uint64_t*) = rt_far_task_await;
rt_remote_task_status (*check_far_cancel)(const rt_far_task_handle*, uint64_t,
    rt_remote_task_pending**, uint8_t*, uint64_t*) = rt_far_task_cancel;
rt_remote_task_status (*check_far_release)(const rt_far_task_handle*) =
    rt_far_task_release;
rt_remote_spawn_status (*check_handle_alloc)(rt_far_task_handle**) =
    rt_far_task_handle_alloc;
rt_runtime_status (*check_remote_task_state_destroy)(rt_executor*) =
    rt_remote_task_state_destroy;
rt_remote_task_status (*check_far_channel_select)(const rt_far_task_handle* const*,
    const uint8_t*, uint64_t*, const uint64_t*, uint64_t, uint64_t, int64_t, void*,
    rt_remote_task_pending**, uint8_t*, uint64_t*) = rt_far_channel_select;

_Static_assert(RT_REMOTE_SPAWN_STATUS_OK == 0, "OK status must stay zero");
_Static_assert(RT_REMOTE_SPAWN_STATUS_PENDING != RT_REMOTE_SPAWN_STATUS_OK,
               "pending must not look successful");
_Static_assert(RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN != RT_REMOTE_SPAWN_STATUS_OK,
               "shutdown must not look successful");
_Static_assert(RT_REMOTE_SPAWN_STATUS_QUEUE_FULL != RT_REMOTE_SPAWN_STATUS_OK,
               "queue-full must not look successful");
_Static_assert(RT_REMOTE_SPAWN_STATUS_STALE_TOKEN != RT_REMOTE_SPAWN_STATUS_OK,
               "stale token must not look successful");
_Static_assert(RT_REMOTE_SPAWN_STATUS_UNSUPPORTED_PLACEMENT != RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT,
               "unsupported placement must not look like missing async context");
_Static_assert(RT_REMOTE_SPAWN_STATUS_INVALID_PLACEMENT != RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT,
               "invalid placement must not look like missing async context");
_Static_assert(RT_REMOTE_TASK_STATUS_OK == 0, "remote task OK status must stay zero");
_Static_assert(RT_REMOTE_TASK_STATUS_PENDING != RT_REMOTE_TASK_STATUS_OK,
               "remote task pending must not look successful");
_Static_assert(RT_REMOTE_TASK_STATUS_CONSUMED != RT_REMOTE_TASK_STATUS_STALE_TOKEN,
               "consumed handle must not look stale");
_Static_assert(sizeof(rt_far_task_handle) == 24,
               "LLVM far Task handle allocation assumes exact native handle size");
_Static_assert(_Alignof(rt_far_task_handle) == 8,
               "LLVM far Task handle allocation assumes exact native handle alignment");
_Static_assert(sizeof(rt_far_task_handle) >= sizeof(uint64_t) * 2,
               "far task handle must carry id and generation");
_Static_assert(sizeof(((rt_far_task_handle*)0)->task_id) == sizeof(uint64_t),
               "task id must stay uint64_t");
_Static_assert(sizeof(((rt_far_task_handle*)0)->generation) == sizeof(uint64_t),
               "generation token must stay uint64_t");
_Static_assert(sizeof(((rt_far_task_handle*)0)->owner_shard_id) == sizeof(uint32_t),
               "owner shard id must stay uint32_t");
_Static_assert(sizeof(((rt_task*)0)->generation) == sizeof(uint64_t),
               "task birth generation must be stored on the task");
_Static_assert(sizeof(((rt_task*)0)->remote_handle_state) == sizeof(_Atomic uint8_t),
               "remote task handle consume state must be atomic");
_Static_assert(WAKER_REMOTE_SPAWN_REPLY != WAKER_NONE,
               "remote spawn reply wait must have its own waiter key");
_Static_assert(WAKER_REMOTE_TASK_REPLY != WAKER_NONE,
               "remote task reply wait must have its own waiter key");
_Static_assert(RT_TRANSPORT_MSG_REMOTE_TASK_AWAIT_REQUEST != RT_TRANSPORT_MSG_NONE,
               "remote task await request must be a real control category");
_Static_assert(RT_TRANSPORT_MSG_REMOTE_TASK_RELEASE_REQUEST != RT_TRANSPORT_MSG_NONE,
               "remote task release request must be a real control category");
`

	runFDRegistryStaticCheck(t, "Runtime V2 remote publication API shape", source)
}

func TestRuntimeV2RemotePublicationBehavior(t *testing.T) {
	bin := buildRemotePublicationHarness(t)
	rows := []struct {
		name string
		mode string
		env  []string
	}{
		{
			name: "publish-other-shard-2",
			mode: "publish-other",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "publish-other-shard-8",
			mode: "publish-other",
			env:  remotePublicationEnv("SURGE_SHARDS=8", "SURGE_THREADS=8", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "self-crossing",
			mode: "self-crossing",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "queue-full",
			mode: "queue-full",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "shutdown",
			mode: "shutdown",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "shutdown-queued-kinds",
			mode: "shutdown-queued-kinds",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "stale-token",
			mode: "stale-token",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1"),
		},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runRemotePublicationHarness(t, bin, row.mode, row.env)
			if code != 0 {
				t.Fatalf("remote publication mode %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					row.mode, code, stdout, stderr)
			}
		})
	}
}

// Spawn-on abandon and refusal edges over a DROPPABLE shipped state: every
// row asserts the exactly-once census (the harness drop stub) on top of the
// edge it forces. Refusals drop through the pending exactly once; abandons
// land while the dispatch lane is held at an armed window, and because the
// request is already in flight the body still runs and owns the state — the
// abandoned pending must not drop it, and the ack resolves as an
// owner-routed release.
func TestRuntimeV2RemoteSpawnAbandonEdges(t *testing.T) {
	bin := buildRemotePublicationHarness(t)
	rows := []struct {
		name string
		mode string
		env  []string
	}{
		{
			name: "refusal-queue-full-drops-once",
			mode: "refusal-drop-queue-full",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "refusal-shutdown-drops-once",
			mode: "refusal-drop-shutdown",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "abandon-before-dispatch",
			mode: "abandon-before-dispatch",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_REMOTE_SPAWN_BEFORE_DISPATCH:block"),
		},
		{
			name: "abandon-before-body-publish",
			mode: "abandon-before-body-publish",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_REMOTE_SPAWN_BEFORE_BODY_PUBLISH:block"),
		},
		{
			name: "abandon-before-ack",
			mode: "abandon-before-ack",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_REMOTE_SPAWN_BEFORE_ACK:block"),
		},
		{
			// Saturated source control lane at ack time: the dispatch
			// rescue-drains (control-first) and the ack still lands — a
			// full lane can never orphan the handle or the shipped state.
			name: "ack-rescue-drain-after-handoff",
			mode: "ack-rescue-drain",
			env: remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_REMOTE_SPAWN_BEFORE_ACK:block"),
		},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runRemotePublicationHarness(t, bin, row.mode, row.env)
			if code != 0 {
				t.Fatalf("abandon edge %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					row.mode, code, stdout, stderr)
			}
		})
	}
}

// Immediate-on edges on the same shipped-state contract: the family has
// no publicly observable far Task handle, so refusals resolve entirely
// through the pending — the sole owner drops the droppable state exactly
// once and no body runs.
func TestRuntimeV2ImmediateOnAbandonEdges(t *testing.T) {
	bin := buildRemotePublicationHarness(t)
	rows := []struct {
		name string
		mode string
		env  []string
	}{
		{
			name: "refusal-queue-full-drops-once",
			mode: "immediate-refusal-queue-full",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "refusal-shutdown-drops-once",
			mode: "immediate-refusal-shutdown",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			// Caller cancelled while the request is UNBOUND: the teardown
			// sweep resolves the pending, the late dispatch refuses to
			// create a body, and the sole-owner pending drops the state
			// exactly once.
			name: "cancel-unbound-refuses-body",
			mode: "immediate-cancel-unbound",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_IMMEDIATE_ON_BEFORE_DISPATCH:block"),
		},
		{
			// Caller cancelled while the request is BOUND (created but
			// unpublished): exactly one routed cancel, the reply edge
			// resolves with no caller to wake, and the state handed off
			// with the publication never drops through the pending.
			name: "cancel-bound-reply-resolves-once",
			mode: "immediate-cancel-bound",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_IMMEDIATE_ON_BEFORE_PUBLISH:block"),
		},
		{
			// A duplicate of the ORIGINAL request still carries the
			// request-scoped token while the pending was rebound to the
			// body task at the bind: the token match fails, exactly one
			// stale drop is counted, and only the message reference is
			// released — no drop, no second body.
			name: "duplicate-request-fails-token-match",
			mode: "immediate-duplicate-request",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_IMMEDIATE_ON_BEFORE_PUBLISH:block"),
		},
		{
			// A redelivered reply matches the already-resolved pending:
			// the finish is a no-op and only the message reference is
			// released.
			name: "stale-reply-releases-reference-only",
			mode: "immediate-stale-reply",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_IMMEDIATE_ON_BEFORE_PUBLISH:block"),
		},
		{
			// A stale-generation anchor refuses the execute at dispatch
			// entry, before any body exists: the pending is the sole owner
			// of the shipped state and drops it exactly once, no body runs,
			// and the failed pin attempt leaks nothing (checked through the
			// registry's own reclaim rule).
			name: "anchored-stale-anchor-refuses",
			mode: "anchored-stale-anchor",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			// Happy path: the anchored execute completes, the reply carries
			// the result, and the dispatch-time pin is already released at
			// the reply edge (proven by releasing the driver's own lease
			// afterward and observing immediate reclaim).
			name: "anchored-happy-path-pin-balance",
			mode: "anchored-happy-path",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			// The anchored twin of the unbound teardown row: the sweep must
			// treat an anchored execute like a placement one — refused at
			// the snapshot check before the pin, no body, one drop.
			name: "anchored-cancel-unbound-refuses-body",
			mode: "anchored-cancel-unbound",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_IMMEDIATE_ON_BEFORE_DISPATCH:block"),
		},
		{
			// Caller cancelled while the anchored execute is BOUND (anchor
			// pinned, body created, held at SP_IMMEDIATE_ON_BEFORE_PUBLISH):
			// the reply edge resolves with no caller to wake, the
			// handed-off state never drops, and the pin releases exactly
			// once at that same reply edge.
			name: "anchored-cancel-bound-reply-resolves-once",
			mode: "anchored-cancel-bound",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_IMMEDIATE_ON_BEFORE_PUBLISH:block"),
		},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runRemotePublicationHarness(t, bin, row.mode, row.env)
			if code != 0 {
				t.Fatalf("immediate-on edge %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					row.mode, code, stdout, stderr)
			}
		})
	}
}

// Remote select abandon/race edges (Epic 20 Task 7 rows 2-5), deterministic
// over Copy payloads: cancel-vs-commit (the winner ships as success even
// though a cancel races in right after the commit), cancel-before-dispatch
// (the select twin of the immediate-on unbound teardown row), double-cancel
// idempotency (a third, independent cancel route is a pure no-op once
// cancel_routed is set), and the refusal-after-shipped regression guard (a
// stale mid-pin arm unpins the already-pinned prefix and never reaches
// rt_select_poll).
func TestRuntimeV2RemoteSelectAbandonEdges(t *testing.T) {
	bin := buildRemotePublicationHarness(t)
	rows := []struct {
		name string
		mode string
		env  []string
	}{
		{
			name: "cancel-vs-commit-ships-winner",
			mode: "far-select-cancel-vs-commit",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY:block"),
		},
		{
			name: "cancel-before-dispatch-refuses-body",
			mode: "far-select-cancel-before-dispatch",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_FAR_SELECT_BEFORE_DISPATCH:block"),
		},
		{
			name: "double-cancel-is-idempotent",
			mode: "far-select-double-cancel",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY:block"),
		},
		{
			name: "refusal-after-shipped-unpins-prefix",
			mode: "far-select-refusal-after-shipped",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "initial-owner-mismatch-drops-payloads",
			mode: "far-select-initial-owner-mismatch",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "initial-null-anchor-array-drops-payload",
			mode: "far-select-initial-null-anchors",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "initial-enqueue-refusal-drops-payload-once",
			mode: "far-select-enqueue-refusal",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1", "SURGE_BLOCKING_THREADS=1"),
		},
		{
			name: "recv-winner-returns-losing-payload",
			mode: "far-select-recv-winner-handback",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1"),
		},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runRemotePublicationHarness(t, bin, row.mode, row.env)
			if code != 0 {
				t.Fatalf("remote select edge %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					row.mode, code, stdout, stderr)
			}
		})
	}
}

// Stale-generation rows for the shipped-state contract: a pending that
// resolves before its request dispatches is the sole owner and drops the
// state exactly once (no body); a redelivered request or ack arriving
// after resolution releases only its own message reference — the
// body-owned state never drops through it and no second body is created.
func TestRuntimeV2RemoteSpawnStaleGenerationRows(t *testing.T) {
	bin := buildRemotePublicationHarness(t)
	rows := []struct {
		name string
		mode string
		env  []string
	}{
		{
			name: "stale-request-before-body",
			mode: "stale-request-before-body",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_REMOTE_SPAWN_BEFORE_DISPATCH:block"),
		},
		{
			name: "duplicate-request-after-handoff",
			mode: "duplicate-request-after-handoff",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_REMOTE_SPAWN_BEFORE_ACK:block"),
		},
		{
			name: "stale-ack-after-resolution",
			mode: "stale-ack-after-resolution",
			env: remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_REMOTE_SPAWN_BEFORE_ACK:block"),
		},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runRemotePublicationHarness(t, bin, row.mode, row.env)
			if code != 0 {
				t.Fatalf("stale row %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					row.mode, code, stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2RemotePublicationFailurePathStaticGuards(t *testing.T) {
	root := repoRoot(t)
	source := readTransportContractFile(t, root, "runtime/native/rt_remote_spawn.c")
	if strings.Contains(source,
		"remote_spawn_pending_finish(ex, req, RT_REMOTE_SPAWN_STATUS_OK, &handle);") {
		t.Fatal("ack enqueue failure must not complete the pending publication as OK")
	}
	publishStatus := strings.Index(source, "status = rt_remote_spawn_publish_body_task")
	ackStatus := strings.Index(source, "status = remote_spawn_enqueue_ack")
	ackFailure := strings.Index(source, "(void)rt_far_task_release(&handle);")
	if publishStatus < 0 || ackStatus < publishStatus {
		t.Fatal("destination task must be published before its OK ack becomes observable")
	}
	if ackFailure < ackStatus {
		t.Fatal("failed ack enqueue must owner-route release of the already-published task")
	}
	if !strings.Contains(source, "remote_spawn_release_msg_payload(&msg);") {
		t.Fatal("shutdown must drop queued remote-spawn transport payload refs")
	}
	if !strings.Contains(source, `panic_msg("remote spawn: unsupported transport message kind")`) {
		t.Fatal("drain must fail closed for transport kinds it does not handle")
	}
	// The shutdown drain releases every kind production can enqueue and
	// fails closed only for truly unknown kinds (RV2-DEBT-047): a message
	// parked between the last steady-state drain and shutdown is valid
	// traffic, and its payload releases through the kind-complete switch.
	if !strings.Contains(
		source, `panic_msg("remote spawn: unknown transport message kind during shutdown")`) {
		t.Fatal("shutdown drain must fail closed for unknown transport kinds")
	}
	for _, kind := range []string{
		"RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST",
		"RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY",
		"RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REQUEST",
		"RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REQUEST",
		"RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REQUEST",
		"RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REPLY",
		"RT_TRANSPORT_MSG_CREDIT_CONTROL",
	} {
		if !strings.Contains(source, "case "+kind+":") {
			t.Fatalf("shutdown drain release switch must cover %s (RV2-DEBT-047)", kind)
		}
	}
}

// The shipped-state ownership contract (rt_remote_spawn_internal.h):
// pending owns -> body owns, linearized at the publication-accepted
// handoff. Every family records the obligation when it publishes, each
// dispatch clears it exactly once immediately after the accepted
// publication, and the final pending release stays the single
// pre-handoff drop site.
func TestRuntimeV2RemoteStateHandoffStaticContract(t *testing.T) {
	root := repoRoot(t)
	for path, record := range map[string]string{
		"runtime/native/rt_remote_spawn.c":          "req->state_owned = state_drop_fn_id != 0;",
		"runtime/native/rt_immediate_on.c":          "request->state_owned = state_drop_fn_id != 0;",
		"runtime/native/rt_immediate_on_anchored.c": "request->state_owned = state_drop_fn_id != 0;",
		"runtime/native/rt_far_channel_select.c":    "request->state_owned = state_drop_fn_id != 0;",
	} {
		if !strings.Contains(readTransportContractFile(t, root, path), record) {
			t.Fatalf("%s must record the shipped-state drop obligation at publish", path)
		}
	}
	for path, handoff := range map[string]string{
		"runtime/native/rt_remote_spawn.c":       "req->state_owned = 0;",
		"runtime/native/rt_immediate_on.c":       "pending->state_owned = 0;",
		"runtime/native/rt_far_channel_select.c": "pending->state_owned = 0;",
	} {
		dispatch := readTransportContractFile(t, root, path)
		if strings.Count(dispatch, handoff) != 1 {
			t.Fatalf("%s must clear the obligation at exactly one publication-accepted handoff", path)
		}
		publish := strings.Index(dispatch, "rt_remote_spawn_publish_body_task")
		if publish < 0 || strings.Index(dispatch, handoff) < publish {
			t.Fatalf("%s handoff must follow the accepted publication", path)
		}
	}
	// Anchored bodies dispatch through the shared immediate-on path; a
	// second handoff site would split the linearization point.
	if strings.Contains(
		readTransportContractFile(t, root, "runtime/native/rt_immediate_on_anchored.c"),
		"state_owned = 0") {
		t.Fatal("anchored dispatch must reuse the immediate-on handoff, not add a second one")
	}
	// Pre-handoff drops stay gated on the owned flag at the final release.
	for path, guard := range map[string]string{
		"runtime/native/rt_remote_spawn_pending.c": "pending->state_owned != 0",
		"runtime/native/rt_remote_task_pending.c":  "pending->state_owned != 0",
	} {
		if !strings.Contains(readTransportContractFile(t, root, path), guard) {
			t.Fatalf("%s final release must drop only while the pending still owns the state", path)
		}
	}
}

func TestRuntimeV2FarSelectInitialFailurePayloadOwnershipStaticContract(t *testing.T) {
	root := repoRoot(t)
	source := readTransportContractFile(t, root, "runtime/native/rt_far_channel_select.c")

	requestFailStart := strings.Index(source, "if (request == NULL) {")
	requestFailEnd := strings.Index(source, "request->handle.generation = request->request_id;")
	if requestFailStart < 0 || requestFailEnd <= requestFailStart {
		t.Fatal("could not isolate far-select pending-allocation failure path")
	}
	requestFail := source[requestFailStart:requestFailEnd]
	if strings.Count(requestFail, "__surge_drop_result_call(") != 1 {
		t.Fatal("pending-allocation failure must have exactly one payload-drop loop")
	}
	if strings.Contains(requestFail, "select_drop_input_payloads(") {
		t.Fatal("pending-allocation failure must not also run the pre-arm payload cleanup")
	}

	enqueueFailStart := strings.Index(source, "rt_remote_task_transport_status(rt_transport_enqueue(destination, &msg));")
	enqueueFailEnd := strings.Index(source, "// Unpins every arm the dispatch pinned")
	if enqueueFailStart < 0 || enqueueFailEnd <= enqueueFailStart {
		t.Fatal("could not isolate far-select initial-enqueue failure path")
	}
	enqueueFail := source[enqueueFailStart:enqueueFailEnd]
	for _, required := range []string{
		"rt_remote_task_pending_consume(request);",
		"rt_remote_task_pending_release(request);",
	} {
		if !strings.Contains(enqueueFail, required) {
			t.Fatalf("initial-enqueue failure is missing pending-owned cleanup %q", required)
		}
	}
	if strings.Contains(enqueueFail, "select_drop_input_payloads(") ||
		strings.Contains(enqueueFail, "__surge_drop_result_call(") {
		t.Fatal("initial-enqueue failure must release only through the pending, not a second direct payload drop")
	}
}

func buildRemotePublicationHarness(t *testing.T) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("clang is required for Runtime V2 remote publication check: %v", err)
	}
	root := repoRoot(t)
	tmp := t.TempDir()
	harness := filepath.Join(tmp, "remote_publication_harness.c")
	bin := filepath.Join(tmp, "remote_publication_harness")
	if err := os.WriteFile(harness, []byte(remotePublicationHarness), 0o600); err != nil {
		t.Fatalf("write harness: %v", err)
	}
	sources, err := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if err != nil {
		t.Fatalf("glob runtime sources: %v", err)
	}
	sort.Strings(sources)
	args := []string{
		"-std=c11",
		"-Wall",
		"-Wextra",
		"-Werror",
		"-DRT_TEST_SYNC_POINTS",
		"-pthread",
		"-I" + filepath.Join(root, "runtime", "native"),
		"-o",
		bin,
		harness,
	}
	for _, src := range sources {
		if filepath.Base(src) != "rt_entry.c" {
			args = append(args, src)
		}
	}
	cmd := exec.Command(clang, args...)
	cmd.Dir = root
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("build remote publication harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	return bin
}

func runRemotePublicationHarness(t *testing.T, bin, mode string, env []string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, mode)
	cmd.Env = env
	return runCommand(t, cmd, "")
}

func remotePublicationEnv(values ...string) []string {
	env := os.Environ()
	for _, value := range values {
		parts := strings.SplitN(value, "=", 2)
		if len(parts) == 2 {
			env = overrideEnvVar(env, parts[0], parts[1])
		}
	}
	return env
}

const remotePublicationHarness = `
#define _POSIX_C_SOURCE 199309L
#include "rt_async_internal.h"
#include "rt_far_channel.h"
#include "rt_placement.h"
#include "rt_remote_spawn.h"
#include "rt_remote_spawn_internal.h"
#include "rt_remote_task.h"
#include "rt_remote_task_internal.h"
#include "rt_sync_point.h"
#include "rt_transport.h"

#include <pthread.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

enum {
    POLL_REMOTE_PUBLISHER = 7001,
    POLL_REMOTE_CHILD = 7002,
    DROP_REMOTE_STATE = 7003,
    POLL_IMMEDIATE_CALLER = 7004,
    POLL_SELECT_CALLER = 7005,
    POLL_SELECT_BODY = 7006,
    DROP_SELECT_PAYLOAD = 7007
};

typedef struct remote_child_state {
    _Atomic uint32_t ran;
    _Atomic uint32_t owner;
    _Atomic uint32_t worker;
} remote_child_state;

typedef struct remote_publish_state {
    rt_remote_spawn_pending* pending;
    // Release/acquire twin of the pending pointer for driver threads: the publisher
    // stores it after publish returns PENDING, giving a cross-thread
    // happens-before edge the sync-point counters (relaxed) do not.
    _Atomic(rt_remote_spawn_pending*) pending_shared;
    remote_child_state* child;
    rt_far_task_handle handle;
    uint32_t dst;
    uint32_t fill_queue;
    uint32_t shutdown_first;
    uint32_t droppable;
    uint32_t abandon_mode;
    uint32_t filled;
    uint32_t shutdown_done;
    uint32_t saw_pending;
    uint64_t request_id;
    rt_remote_spawn_status status;
    rt_remote_spawn_status validate_status;
    size_t children_after;
} remote_publish_state;

typedef struct immediate_exec_state {
    rt_remote_task_pending* pending;
    // Release/acquire twin of the pending pointer for driver threads
    // (the sync-point counters alone give no happens-before edge).
    _Atomic(rt_remote_task_pending*) pending_shared;
    remote_child_state* child;
    uint64_t placement;
    uint32_t fill_queue;
    uint32_t shutdown_first;
    uint32_t droppable;
    uint32_t filled;
    uint32_t shutdown_done;
    uint32_t saw_pending;
    // Anchored rows: route through rt_immediate_on_execute_anchored against
    // this token instead of rt_immediate_on_execute against a placement.
    uint32_t anchored;
    rt_far_task_handle anchor;
    uint8_t out_kind;
    uint64_t out_bits;
    rt_remote_task_status status;
} immediate_exec_state;

// Remote select rows (Epic 20 Task 7 rows 2-5). The caller side wraps
// rt_far_channel_select the same way immediate_exec_state wraps
// rt_immediate_on_execute_anchored; the body side is minimal, since the
// real work (rt_anchored_channel_select) is production runtime code, not
// harness code -- the body poll function only has to hand the winner
// index to rt_async_return, mirroring the compiled select lowering.
typedef struct select_exec_state {
    rt_remote_task_pending* pending;
    _Atomic(rt_remote_task_pending*) pending_shared;
    void* body_state;
    uint32_t droppable;
    uint32_t saw_pending;
    uint32_t fill_queue;
    uint32_t filled;
	uint32_t null_anchor_array;
    rt_far_task_handle anchors[2];
    const rt_far_task_handle* anchor_ptrs[2];
    uint8_t kinds[2];
    uint64_t send_bits[2];
    uint64_t send_drop_ids[2];
    uint64_t count;
    uint8_t out_kind;
    uint64_t out_bits;
    rt_remote_task_status status;
} select_exec_state;

uint64_t __surge_blocking_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
    return 0;
}

static void sleep_us(unsigned long micros) {
    struct timespec ts;
    ts.tv_sec = (time_t)(micros / 1000000UL);
    ts.tv_nsec = (long)((micros % 1000000UL) * 1000UL);
    while (nanosleep(&ts, &ts) != 0) {
    }
}

static int fail(const char* msg) {
    if (msg != NULL) {
        fputs(msg, stderr);
        fputc('\n', stderr);
    }
    return 1;
}

static int wait_child(remote_child_state* child, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (atomic_load_explicit(&child->ran, memory_order_acquire) != 0) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static uint32_t pin_shard(rt_executor* ex, uint32_t wanted) {
    size_t count = rt_runtime_shard_count(rt_executor_runtime(ex));
    return count > 0 ? (uint32_t)(wanted % (uint32_t)count) : 0;
}

static int fill_data_lane(rt_executor* ex, uint32_t dst) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), dst);
    if (shard == NULL) {
        return 0;
    }
    rt_transport_msg data = {
        .kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST,
        .source_shard_id = 0,
        .target_shard_id = dst,
        .route_id = 9000,
        .generation = 1,
        .payload = NULL,
        .payload_len = 0,
    };
    for (size_t i = 0; i < RT_TRANSPORT_DATA_QUEUE_CAP; i++) {
        if (rt_transport_enqueue(shard, &data) != RT_TRANSPORT_STATUS_OK) {
            return 0;
        }
    }
    data.kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK;
    return rt_transport_enqueue(shard, &data) == RT_TRANSPORT_STATUS_OK;
}

// Drop-dispatch stub: the abandon/refusal rows publish a droppable state
// (DROP_REMOTE_STATE) and count releases here — the exactly-once census
// for the shipped-state ownership contract. Any other id is a test bug.
static _Atomic uint32_t drop_calls;
static _Atomic uint32_t payload_drop_calls;
static void* drop_expected_state;
void __surge_drop_call(uint64_t id, void* state) {
    if (id == DROP_REMOTE_STATE && state == drop_expected_state) {
        atomic_fetch_add_explicit(&drop_calls, 1, memory_order_acq_rel);
        return;
    }
    fputs("unexpected __surge_drop_call\n", stderr);
    exit(97);
}

void __surge_drop_result_call(uint64_t id, void* value) {
    (void)value;
    if (id == DROP_SELECT_PAYLOAD) {
        atomic_fetch_add_explicit(&payload_drop_calls, 1, memory_order_acq_rel);
        return;
    }
    fputs("unexpected __surge_drop_result_call\n", stderr);
    exit(97);
}

// No row here threads a nonzero abandoned-state drop id through
// rt_async_yield/rt_async_return_cancelled, so reaching this is a test bug.
void __surge_drop_abandoned_state_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
    fputs("unexpected __surge_drop_abandoned_state_call\n", stderr);
    exit(97);
}

static int wait_reached(rt_sync_point_id id, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (rt_sync_point_reached_count(id) > 0) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static int wait_drops(uint32_t want, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (atomic_load_explicit(&drop_calls, memory_order_acquire) == want) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static int wait_payload_drops(uint32_t want, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (atomic_load_explicit(&payload_drop_calls, memory_order_acquire) == want) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

// Saturate the shard's control lane so the NEXT control-kind enqueue
// (an ack) fails deterministically. NULL-payload acks are harmless to
// drain later (the ack dispatcher ignores them).
static int fill_control_lane(rt_shard* shard, uint32_t shard_id) {
    if (shard == NULL) {
        return 0;
    }
    rt_transport_msg ack = {0};
    ack.kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK;
    ack.target_shard_id = shard_id;
    for (size_t i = 0; i < RT_TRANSPORT_CONTROL_QUEUE_CAP * 2; i++) {
        if (rt_transport_enqueue(shard, &ack) == RT_TRANSPORT_STATUS_QUEUE_FULL) {
            return 1;
        }
    }
    return 0;
}

// Dispatch-completion signal for the redelivery rows: the redelivered
// message's pending_release is the LAST step of its dispatch path, so an
// acquire-load seeing the reference count fall to the wanted value happens-after
// everything that dispatch did (or erroneously did) with the pending.
static int wait_refs(rt_remote_spawn_pending* req, uint32_t want, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (atomic_load_explicit(&req->refs, memory_order_acquire) == want) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static rt_remote_task_pending* wait_task_pending_shared(immediate_exec_state* st,
                                                        uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        rt_remote_task_pending* req =
            atomic_load_explicit(&st->pending_shared, memory_order_acquire);
        if (req != NULL) {
            return req;
        }
        sleep_us(1000);
    }
    return NULL;
}

static int wait_task_refs(rt_remote_task_pending* req, uint32_t want, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (atomic_load_explicit(&req->refs, memory_order_acquire) == want) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static rt_remote_task_pending* wait_select_pending_shared(select_exec_state* st,
                                                          uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        rt_remote_task_pending* req =
            atomic_load_explicit(&st->pending_shared, memory_order_acquire);
        if (req != NULL) {
            return req;
        }
        sleep_us(1000);
    }
    return NULL;
}

// Non-blocking, driver-thread-safe channel probes for the select rows: the
// driver runs outside any task, so the task-context channel API
// (rt_channel_recv/rt_channel_try_send) is unavailable; the control-locked
// status wrappers used by the select slow lane work from any thread.
static int channel_recv_once(rt_executor* ex, void* channel, uint64_t want_bits) {
    rt_control_lock(ex);
    uint64_t bits = 0;
    uint8_t status = rt_channel_try_recv_status_locked(ex, channel, &bits);
    rt_control_unlock(ex);
    if (status != 1 || bits != want_bits) {
        return 0;
    }
    rt_control_lock(ex);
    uint8_t empty_status = rt_channel_try_recv_status_locked(ex, channel, NULL);
    rt_control_unlock(ex);
    return empty_status == 0;
}

static int channel_is_empty(rt_executor* ex, void* channel) {
    rt_control_lock(ex);
    uint8_t status = rt_channel_try_recv_status_locked(ex, channel, NULL);
    rt_control_unlock(ex);
    return status == 0;
}

static rt_remote_spawn_pending* wait_pending_shared(remote_publish_state* st, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        rt_remote_spawn_pending* req =
            atomic_load_explicit(&st->pending_shared, memory_order_acquire);
        if (req != NULL) {
            return req;
        }
        sleep_us(1000);
    }
    return NULL;
}

void __surge_poll_call(uint64_t id) {
    if (id == POLL_REMOTE_CHILD) {
        remote_child_state* child = (remote_child_state*)__task_state();
        const rt_task* task = rt_current_task();
        atomic_store_explicit(&child->owner,
                              task != NULL ? task->owner_shard_id : UINT32_MAX,
                              memory_order_release);
        atomic_store_explicit(&child->worker, rt_debug_current_worker_shard_id(), memory_order_release);
        // A counter, not a flag: the duplicate/stale rows assert that a
        // redelivered request never creates a SECOND body.
        atomic_fetch_add_explicit(&child->ran, 1, memory_order_acq_rel);
        rt_async_return(child, 77);
        return;
    }
    if (id == POLL_IMMEDIATE_CALLER) {
        immediate_exec_state* st = (immediate_exec_state*)__task_state();
        rt_executor* ex = ensure_exec();
        if (st->shutdown_first && !st->shutdown_done) {
            st->shutdown_done = 1;
            (void)rt_executor_request_shutdown(ex);
        }
        if (st->fill_queue && !st->filled) {
            st->filled = 1;
            if (!fill_data_lane(ex, 0)) {
                st->status = RT_REMOTE_TASK_STATUS_REFUSED;
                rt_async_return(st, (uint64_t)st->status);
                return;
            }
        }
        rt_remote_task_status status = st->anchored
            ? rt_immediate_on_execute_anchored(
                  &st->anchor, st->droppable ? DROP_REMOTE_STATE : 0, POLL_REMOTE_CHILD,
                  st->child, &st->pending, &st->out_kind, &st->out_bits)
            : rt_immediate_on_execute(
                  st->placement, st->droppable ? DROP_REMOTE_STATE : 0, POLL_REMOTE_CHILD,
                  st->child, &st->pending, &st->out_kind, &st->out_bits);
        if (status == RT_REMOTE_TASK_STATUS_PENDING) {
            st->saw_pending = 1;
            atomic_store_explicit(&st->pending_shared, st->pending, memory_order_release);
            rt_async_yield(st, 0);
            return;
        }
        st->status = status;
        rt_async_return(st, (uint64_t)status);
        return;
    }
    if (id == POLL_SELECT_CALLER) {
        select_exec_state* st = (select_exec_state*)__task_state();
        if (st->fill_queue && !st->filled) {
            st->filled = 1;
            if (!fill_data_lane(ensure_exec(), st->anchors[0].owner_shard_id)) {
                st->status = RT_REMOTE_TASK_STATUS_REFUSED;
                rt_async_return(st, (uint64_t)st->status);
                return;
            }
        }
		const rt_far_task_handle* const* anchors =
			st->null_anchor_array ? NULL : st->anchor_ptrs;
		rt_remote_task_status status = rt_far_channel_select(
			anchors, st->kinds, st->send_bits, st->send_drop_ids, st->count,
            st->droppable ? DROP_REMOTE_STATE : 0, POLL_SELECT_BODY, st->body_state,
            &st->pending, &st->out_kind, &st->out_bits);
        if (status == RT_REMOTE_TASK_STATUS_PENDING) {
            st->saw_pending = 1;
            atomic_store_explicit(&st->pending_shared, st->pending, memory_order_release);
            rt_async_yield(st, 0);
            return;
        }
        st->status = status;
        rt_async_return(st, (uint64_t)status);
        return;
    }
    if (id == POLL_SELECT_BODY) {
        // The compiled lowering calls rt_anchored_channel_select and stores
        // the winner into the block's select_index destination; the
        // harness body mirrors that exactly, since the select machinery
        // itself is production runtime code under test, not a stand-in.
        uint64_t winner = rt_anchored_channel_select();
        rt_async_return(__task_state(), winner);
        return;
    }
    if (id == POLL_REMOTE_PUBLISHER) {
        remote_publish_state* st = (remote_publish_state*)__task_state();
        rt_executor* ex = ensure_exec();
        if (st->abandon_mode && st->saw_pending) {
            // The abandon rows model a departed caller: after the driver
            // abandons the handle the pending may be freed at any moment,
            // so this task must never touch it again.
            rt_async_yield(st, 0);
            return;
        }
        if (st->shutdown_first && !st->shutdown_done) {
            st->shutdown_done = 1;
            (void)rt_executor_request_shutdown(ex);
        }
        if (st->fill_queue && !st->filled) {
            st->filled = 1;
            if (!fill_data_lane(ex, st->dst)) {
                rt_async_return(st, RT_REMOTE_SPAWN_STATUS_REFUSED);
                return;
            }
        }
        rt_remote_spawn_status status = rt_remote_spawn_publish(
            st->dst, st->droppable ? DROP_REMOTE_STATE : 0, 0, POLL_REMOTE_CHILD,
            st->child, &st->pending, &st->handle);
        if (status == RT_REMOTE_SPAWN_STATUS_PENDING) {
            st->saw_pending = 1;
            st->request_id = rt_remote_spawn_pending_request_id(st->pending);
            atomic_store_explicit(&st->pending_shared, st->pending, memory_order_release);
            rt_async_yield(st, 0);
            return;
        }
        st->status = status;
        st->children_after = rt_current_task() != NULL ? rt_current_task()->children_len : 999;
        st->validate_status = status == RT_REMOTE_SPAWN_STATUS_OK
                                  ? rt_remote_spawn_handle_validate(ex, &st->handle)
                                  : status;
        rt_async_return(st, (uint64_t)status);
        return;
    }
    rt_async_return(NULL, 0);
}

static int await_parent(remote_publish_state* st) {
    void* task = __task_create(POLL_REMOTE_PUBLISHER, st);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(task, &kind, &bits);
    if (kind != 1) {
        return 0;
    }
    return bits == (uint64_t)st->status;
}

static int run_publish(uint32_t wanted_dst, int stale) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.dst = pin_shard(ex, wanted_dst);
    if (!await_parent(&st)) return fail("publisher await failed");
    if (st.status != RT_REMOTE_SPAWN_STATUS_OK) return fail("publish did not return OK");
    if (st.validate_status != RT_REMOTE_SPAWN_STATUS_OK) return fail("handle did not validate");
    if (st.handle.owner_shard_id != st.dst) return fail("ack returned wrong owner shard");
    if (st.handle.generation == 0) return fail("ack returned empty generation");
    if (st.children_after != 0) return fail("remote child was enrolled under caller children");
    if (st.saw_pending == 0 || st.request_id == 0) return fail("publisher did not suspend for ack");
    if (!wait_child(&child, 5000)) return fail("remote child did not run");
    if (atomic_load_explicit(&child.owner, memory_order_acquire) != st.dst) {
        return fail("remote child owner mismatch");
    }
    uint32_t worker = atomic_load_explicit(&child.worker, memory_order_acquire);
    if (worker != st.dst && !(st.dst == 0 && worker == UINT32_MAX)) {
        return fail("remote child ran on non-owner shard");
    }
    rt_shard* source = rt_runtime_shard(rt_executor_runtime(ex), 0);
    rt_shard* dest = rt_runtime_shard(rt_executor_runtime(ex), st.dst);
    struct rt_transport_debug_snapshot src = rt_transport_debug_snapshot(source);
    struct rt_transport_debug_snapshot dst = rt_transport_debug_snapshot(dest);
    if (dst.transport_spawn_requests == 0) return fail("destination request counter missing");
    if (src.transport_spawn_acks == 0) return fail("source ack counter missing");
    if (rt_sync_point_reached_count(RT_SYNC_POINT_SP_TRANSPORT_REPLY_WAIT_BEFORE_TASK_SUSPEND) == 0) {
        return fail("reply wait did not use task-suspend seam");
    }
    if (stale) {
        rt_far_task_handle bad = st.handle;
        bad.generation++;
        if (rt_remote_spawn_handle_validate(ex, &bad) != RT_REMOTE_SPAWN_STATUS_STALE_TOKEN) {
            return fail("fabricated stale token validated");
        }
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int run_queue_full(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.dst = 0;
    st.fill_queue = 1;
    if (!await_parent(&st)) return fail("queue-full publisher await failed");
    if (st.status != RT_REMOTE_SPAWN_STATUS_QUEUE_FULL) return fail("queue full status mismatch");
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), 0);
    struct rt_transport_debug_snapshot snap = rt_transport_debug_snapshot(shard);
    if (snap.control_len == 0 || snap.data_len != RT_TRANSPORT_DATA_QUEUE_CAP) {
        return fail("control lane was blocked by full data lane");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// RV2-DEBT-047: messages parked between the last steady-state drain and
// shutdown are valid traffic for every production kind — the shutdown
// drain must release them, never panic.
static int run_shutdown_queued_kinds(void) {
    rt_executor* ex = ensure_exec();
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), 0);
    const rt_transport_msg_kind kinds[] = {
        RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST,
        RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY,
        RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REQUEST,
        RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REQUEST,
        RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REQUEST,
        RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REPLY,
        RT_TRANSPORT_MSG_CREDIT_CONTROL,
    };
    for (size_t i = 0; i < sizeof(kinds) / sizeof(kinds[0]); i++) {
        rt_transport_msg msg = {0};
        msg.kind = kinds[i];
        msg.target_shard_id = 0;
        if (rt_transport_enqueue(shard, &msg) != RT_TRANSPORT_STATUS_OK) {
            return fail("queued-kind enqueue failed");
        }
    }
    rt_remote_spawn_fail_all_pending(ex, RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN);
    struct rt_transport_debug_snapshot snap = rt_transport_debug_snapshot(shard);
    if (snap.data_len != 0 || snap.control_len != 0) {
        return fail("shutdown drain left queued messages behind");
    }
    return 0;
}

static int run_shutdown(void) {
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.dst = 0;
    st.shutdown_first = 1;
    if (!await_parent(&st)) return fail("shutdown publisher await failed");
    if (st.status != RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN) {
        return fail("shutdown status mismatch");
    }
    return 0;
}

// Refusal edges with a droppable shipped state: the publish is refused
// (queue full or destination shutdown), so the pending — or the pre-link
// path — is the sole owner and must drop the state exactly once, before
// the caller observes the refusal.
static int run_refusal_drop(int shutdown_first) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.dst = 0;
    st.fill_queue = shutdown_first ? 0 : 1;
    st.shutdown_first = shutdown_first ? 1 : 0;
    st.droppable = 1;
    drop_expected_state = &child;
    if (!await_parent(&st)) return fail("refusal publisher await failed");
    rt_remote_spawn_status want = shutdown_first ? RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN
                                                 : RT_REMOTE_SPAWN_STATUS_QUEUE_FULL;
    if (st.status != want) return fail("refusal status mismatch");
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 1) {
        return fail("refused publish must drop the shipped state exactly once");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 0) {
        return fail("refused publish must not run a body");
    }
    if (!shutdown_first) {
        (void)rt_executor_request_shutdown(ex);
    }
    return 0;
}

// Abandon edges: the caller-owned handle is abandoned while the dispatch
// lane is held at an armed window (dispatch entry / created-but-unpublished
// / published-but-unacked). In every window the request is already in
// flight, so the body still runs and owns the shipped state — the pending
// must NOT drop it (drop count stays zero), and the resolved-while-abandoned
// ack turns into an owner-routed release instead of waking a caller.
static int run_abandon_window(rt_sync_point_id window) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.dst = pin_shard(ex, 1);
    st.droppable = 1;
    st.abandon_mode = 1;
    drop_expected_state = &child;
    void* publisher = __task_create(POLL_REMOTE_PUBLISHER, &st);
    if (publisher == NULL) return fail("publisher task create failed");
    if (!wait_reached(window, 5000)) return fail("armed window was never reached");
    if (!rt_remote_spawn_abandon_handle(&st.handle)) {
        return fail("abandon did not find the listed pending");
    }
    rt_sync_point_open();
    if (!wait_child(&child, 5000)) return fail("abandoned spawn body did not run");
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("handed-off state must not drop through the abandoned pending");
    }
    if (rt_sync_point_reached_count(window) == 0) {
        return fail("window count vanished");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row-4 remainder, pinned as what the runtime actually guarantees: an
// ack facing a SATURATED control lane does not fail the publication —
// enqueue_with_drain rescue-drains (control-first) and the ack lands, so
// a full lane can never orphan the handle or the handed-off state. The
// failure branch below the rescue is reachable only through transport
// shutdown; its release ordering stays pinned by the static guards.
// Single-shard execution is driven by the main thread inside
// rt_task_await, so the lane fill runs on a helper thread while main is
// held at the armed window.
typedef struct ack_failure_driver {
    rt_executor* ex;
    _Atomic uint32_t failed;
} ack_failure_driver;

static void* ack_failure_driver_main(void* arg) {
    ack_failure_driver* drv = (ack_failure_driver*)arg;
    if (!wait_reached(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_ACK, 5000)) {
        atomic_store_explicit(&drv->failed, 1, memory_order_release);
        rt_sync_point_open();
        return NULL;
    }
    rt_shard* source = rt_runtime_shard(rt_executor_runtime(drv->ex), 0);
    if (!fill_control_lane(source, 0)) {
        atomic_store_explicit(&drv->failed, 2, memory_order_release);
    }
    rt_sync_point_open();
    return NULL;
}

static int run_ack_rescue_drain(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.dst = 0;
    st.droppable = 1;
    drop_expected_state = &child;
    ack_failure_driver drv;
    memset(&drv, 0, sizeof(drv));
    drv.ex = ex;
    pthread_t driver;
    if (pthread_create(&driver, NULL, ack_failure_driver_main, &drv) != 0) {
        return fail("driver thread create failed");
    }
    remote_publish_state* stp = &st;
    if (!await_parent(stp)) {
        (void)pthread_join(driver, NULL);
        return fail("ack-failure publisher await failed");
    }
    (void)pthread_join(driver, NULL);
    if (atomic_load_explicit(&drv.failed, memory_order_acquire) != 0) {
        return fail(atomic_load_explicit(&drv.failed, memory_order_acquire) == 1
                        ? "ack window was never reached"
                        : "control lane did not saturate");
    }
    if (st.status != RT_REMOTE_SPAWN_STATUS_OK) {
        return fail("saturated control lane must not fail the publication (rescue drain)");
    }
    if (!wait_child(&child, 5000)) return fail("published body did not run");
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("handed-off state must not drop across the ack rescue");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Stale request before body creation: the pending resolves while its
// request message is still waiting at dispatch entry. The dispatch must
// step aside (no body), leaving the pending the sole owner — the state
// drops exactly once through the final pending release.
static int run_stale_request_before_body(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.dst = pin_shard(ex, 1);
    st.droppable = 1;
    drop_expected_state = &child;
    void* publisher = __task_create(POLL_REMOTE_PUBLISHER, &st);
    if (publisher == NULL) return fail("publisher task create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_DISPATCH, 5000)) {
        return fail("dispatch window was never reached");
    }
    rt_remote_spawn_fail_all_pending(ex, RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN);
    rt_sync_point_open();
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(publisher, &kind, &bits);
    if (kind != 1 || st.status != RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN) {
        return fail("resolved-before-dispatch must surface the failure status");
    }
    if (!wait_drops(1, 5000)) {
        return fail("sole-owner pending must drop the state exactly once");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 0) {
        return fail("stale request must not create a body");
    }
    return 0;
}

// Duplicate/stale delivery after resolution: a second copy of an already
// resolved request (or its ack) must release only its own message
// reference — never drop the body-owned state, never create a second
// body. The extra pending reference taken in the ack window models the
// redelivered copy's payload reference.
static int run_stale_redelivery(int redeliver_ack) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.dst = pin_shard(ex, 1);
    st.droppable = 1;
    drop_expected_state = &child;
    void* publisher = __task_create(POLL_REMOTE_PUBLISHER, &st);
    if (publisher == NULL) return fail("publisher task create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_ACK, 5000)) {
        return fail("ack window was never reached");
    }
    rt_remote_spawn_pending* req = wait_pending_shared(&st, 5000);
    if (req == NULL) return fail("pending missing in ack window");
    // Two references while the window pins the pending alive: one models
    // the redelivered copy's payload reference (consumed by its dispatch),
    // one keeps this driver's view of the refcount valid until the final
    // assertions are done.
    remote_spawn_pending_add_ref(req);
    remote_spawn_pending_add_ref(req);
    uint32_t source_shard = req->source_shard_id;
    rt_sync_point_open();
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(publisher, &kind, &bits);
    if (kind != 1 || st.status != RT_REMOTE_SPAWN_STATUS_OK) {
        return fail("publication must resolve OK before the redelivery");
    }
    if (!wait_child(&child, 5000)) return fail("body did not run before the redelivery");
    uint32_t target = redeliver_ack ? source_shard : st.dst;
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), target);
    rt_transport_msg dup = {0};
    dup.kind = redeliver_ack ? RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK
                             : RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST;
    dup.source_shard_id = source_shard;
    dup.target_shard_id = target;
    dup.route_id = rt_remote_spawn_pending_request_id(req);
    dup.payload = req;
    if (rt_transport_enqueue(shard, &dup) != RT_TRANSPORT_STATUS_OK) {
        return fail("redelivery enqueue failed");
    }
    // The redelivery's own pending_release is the last step of its
    // dispatch: refs falling back to the driver's single reference
    // happens-after everything that dispatch did with the pending.
    if (!wait_refs(req, 1, 5000)) {
        return fail("redelivered message was never dispatched");
    }
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("redelivery must not drop the body-owned state");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 1) {
        return fail("redelivery must not create a second body");
    }
    remote_spawn_pending_release(req);
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int await_immediate(immediate_exec_state* st) {
    void* task = __task_create(POLL_IMMEDIATE_CALLER, st);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(task, &kind, &bits);
    if (kind != 1) {
        return 0;
    }
    return bits == (uint64_t)st->status;
}

static int await_select(select_exec_state* st) {
    void* task = __task_create(POLL_SELECT_CALLER, st);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(task, &kind, &bits);
    if (kind != 1) {
        return 0;
    }
    return bits == (uint64_t)st->status;
}

// Immediate-on refusal edges: no far handle exists for the execute/reply
// category, so the pending (or the pre-link path) is the sole owner of the
// shipped state — it drops exactly once, no body runs, and the caller
// resumes with the refusal status.
static int run_immediate_refusal_drop(int shutdown_first) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    immediate_exec_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.placement = rt_placement_shard(0);
    st.fill_queue = shutdown_first ? 0 : 1;
    st.shutdown_first = shutdown_first ? 1 : 0;
    st.droppable = 1;
    drop_expected_state = &child;
    if (!await_immediate(&st)) return fail("immediate refusal await failed");
    rt_remote_task_status want = shutdown_first ? RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN
                                                : RT_REMOTE_TASK_STATUS_QUEUE_FULL;
    if (st.status != want) return fail("immediate refusal status mismatch");
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 1) {
        return fail("refused immediate execute must drop the shipped state exactly once");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 0) {
        return fail("refused immediate execute must not run a body");
    }
    if (!shutdown_first) {
        (void)rt_executor_request_shutdown(ex);
    }
    return 0;
}

// Immediate-on caller-teardown split. Cancelling the caller while its
// execute request is UNBOUND (held at dispatch entry) must resolve the
// pending through the teardown sweep so the late dispatch refuses to
// create a body — the pending stays the state's sole owner and drops it
// exactly once. Cancelling while the request is BOUND (held between the
// body bind and its publication) routes exactly one cancel; the state
// handed off with the publication, so it must never drop through the
// pending, and the reply edge resolves once with no caller to wake.
static int run_immediate_cancel(rt_sync_point_id window, int expect_body) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    immediate_exec_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.placement = rt_placement_shard(pin_shard(ex, 1));
    st.droppable = 1;
    drop_expected_state = &child;
    void* caller = __task_create(POLL_IMMEDIATE_CALLER, &st);
    if (caller == NULL) return fail("immediate caller create failed");
    if (!wait_reached(window, 5000)) return fail("immediate window was never reached");
    rt_task_cancel(caller);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (kind == 1) return fail("cancelled immediate caller must not resolve successfully");
    rt_sync_point_open();
    if (expect_body) {
        if (!wait_child(&child, 5000)) return fail("bound execute body did not run");
        // The reply edge resolves behind the body completion; give the
        // release chain a settle window before pinning the no-drop census.
        sleep_us(50000);
        if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
            return fail("handed-off state must not drop after bind");
        }
    } else {
        if (!wait_drops(1, 5000)) {
            return fail("unbound cancel must drop the state exactly once through the pending");
        }
        if (atomic_load_explicit(&child.ran, memory_order_acquire) != 0) {
            return fail("unbound cancel must refuse body creation");
        }
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Immediate-on redelivery after resolution. A duplicate of the ORIGINAL
// execute request still carries the request-scoped token, while the
// pending's handle was rebound to the body task's generation at the
// bind — so the duplicate must fail the token match, count one stale
// drop, answer stale-token into the (already resolved, hence no-op)
// reply edge, and release only its own message reference. A redelivered
// REPLY matches the resolved pending and must equally release only its
// reference. Neither may drop the body-owned state or create a second
// body.
static int run_immediate_redelivery(int redeliver_reply) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    immediate_exec_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    uint32_t dst = pin_shard(ex, 1);
    st.placement = rt_placement_shard(dst);
    st.droppable = 1;
    drop_expected_state = &child;
    void* caller = __task_create(POLL_IMMEDIATE_CALLER, &st);
    if (caller == NULL) return fail("immediate caller create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_PUBLISH, 5000)) {
        return fail("immediate publish window was never reached");
    }
    rt_remote_task_pending* req = wait_task_pending_shared(&st, 5000);
    if (req == NULL) return fail("pending missing in publish window");
    rt_remote_task_pending_add_ref(req);
    rt_remote_task_pending_add_ref(req);
    uint64_t request_id = req->request_id;
    uint32_t source_shard = req->source_shard_id;
    rt_shard* dst_shard = rt_runtime_shard(rt_executor_runtime(ex), dst);
    uint64_t stale_before = rt_transport_debug_snapshot(dst_shard).remote_task_stale_drops;
    rt_sync_point_open();
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (kind != 1 || st.status != RT_REMOTE_TASK_STATUS_OK || st.out_kind != 1 ||
        st.out_bits != 77) {
        return fail("immediate execute must resolve OK before the redelivery");
    }
    if (!wait_child(&child, 5000)) return fail("body did not run before the redelivery");
    rt_transport_msg dup = {0};
    if (redeliver_reply) {
        dup.kind = RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY;
        dup.source_shard_id = req->handle.owner_shard_id;
        dup.target_shard_id = source_shard;
        dup.route_id = request_id;
        dup.generation = req->handle.generation;
    } else {
        dup.kind = RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST;
        dup.source_shard_id = source_shard;
        dup.target_shard_id = dst;
        dup.route_id = request_id;
        // The original request's token: request-scoped generation, minted
        // before the bind rebound the handle to the body task.
        dup.generation = request_id;
    }
    rt_shard* target_shard =
        rt_runtime_shard(rt_executor_runtime(ex), dup.target_shard_id);
    dup.payload = req;
    if (rt_transport_enqueue(target_shard, &dup) != RT_TRANSPORT_STATUS_OK) {
        return fail("redelivery enqueue failed");
    }
    if (!wait_task_refs(req, 1, 5000)) {
        return fail("redelivered message was never fully released");
    }
    if (!redeliver_reply) {
        uint64_t stale_after = rt_transport_debug_snapshot(dst_shard).remote_task_stale_drops;
        if (stale_after != stale_before + 1) {
            return fail("duplicate request must count exactly one stale drop");
        }
    }
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("redelivery must not drop the body-owned state");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 1) {
        return fail("redelivery must not create a second body");
    }
    rt_remote_task_pending_release(req);
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Anchored immediate-on rows. The dispatch entry (rt_immediate_on_dispatch_execute)
// pins the anchor before a body exists (rt_immediate_on.c); a stale generation
// answers STALE_TOKEN without ever incrementing the entry's in-flight pin
// count, and every later exit (create-fail, teardown re-check, publish-fail,
// the owner-done reply edge) unpins exactly once. There is no counter the
// harness can read directly, so pin balance is proven through the registry's
// own reclaim rule (rt_far_channel.c: an entry with no active leases and no
// in-flight pins is freed): the driver mints the anchor (one lease, live
// count 1), lets the scenario run, then releases its own lease last. If a
// dispatch-side pin were still outstanding, the entry's in-flight count
// would be nonzero and the release would NOT bring the live count to zero;
// if the anchored path leaked no pin, the release is the final one and the
// entry reclaims immediately.
static int mint_anchor(rt_executor* ex, uint32_t owner_shard_id, rt_far_task_handle* out) {
    void* channel = rt_channel_new(0, 0);
    if (channel == NULL) {
        return 0;
    }
    rt_channel_bind_owner_shard(channel, owner_shard_id);
    return rt_far_channel_mint(ex, channel, owner_shard_id, out) == RT_REMOTE_TASK_STATUS_OK;
}

// Select-row variant: returns the local channel pointer too (the select
// rows drive/observe the channel directly from the driver thread to prove
// exactly-once delivery) and takes a capacity so a SEND arm can commit
// deterministically against a buffered channel with room.
static void* mint_channel_anchor(rt_executor* ex,
                                 uint32_t owner_shard_id,
                                 uint64_t capacity,
                                 rt_far_task_handle* out) {
    void* channel = rt_channel_new(capacity, 0);
    if (channel == NULL) {
        return NULL;
    }
    rt_channel_bind_owner_shard(channel, owner_shard_id);
    if (rt_far_channel_mint(ex, channel, owner_shard_id, out) != RT_REMOTE_TASK_STATUS_OK) {
        return NULL;
    }
    return channel;
}

// Row A: a stale-generation anchor refuses the execute before any body
// exists. The pending is the sole owner of the shipped state (dispatch bails
// at the pin check, well before the publication-accepted handoff), so it
// drops exactly once; the failed pin attempt never touches the entry's
// in-flight count, so releasing the original (still-active) lease reclaims
// the entry immediately.
static int run_anchored_stale_anchor(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    immediate_exec_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.droppable = 1;
    st.anchored = 1;
    drop_expected_state = &child;

    rt_far_task_handle anchor = {0};
    if (!mint_anchor(ex, 0, &anchor)) return fail("anchor mint failed");
    st.anchor = anchor;
    st.anchor.generation++;  // corrupt a COPY; the original lease stays valid below

    if (!await_immediate(&st)) return fail("stale anchor await failed");
    if (st.status != RT_REMOTE_TASK_STATUS_STALE_TOKEN) {
        return fail("stale anchor execute must answer STALE_TOKEN");
    }
    if (!wait_drops(1, 5000)) {
        return fail("stale anchor path must drop the shipped state exactly once");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 0) {
        return fail("stale anchor must not run a body");
    }
    if (rt_far_channel_debug_live_count(ex) != 1) return fail("mint left no live entry to check");
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("original anchor lease release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return fail("stale-anchor dispatch attempt leaked a pin on the entry");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row B: the happy path. Execute against a live anchor, let the body run
// (it does not touch the channel), and prove the dispatch-time pin was
// already released at the reply edge (rt_remote_task_reply_owner_done unpins
// before answering OK) by releasing the driver's own lease afterward and
// checking the entry reclaims immediately.
static int run_anchored_happy_path(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    immediate_exec_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.droppable = 1;
    st.anchored = 1;
    drop_expected_state = &child;

    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle anchor = {0};
    if (!mint_anchor(ex, dst, &anchor)) return fail("anchor mint failed");
    st.anchor = anchor;

    if (!await_immediate(&st)) return fail("anchored happy-path await failed");
    if (st.status != RT_REMOTE_TASK_STATUS_OK || st.out_kind != 1 || st.out_bits != 77) {
        return fail("anchored happy path did not resolve OK");
    }
    if (!wait_child(&child, 5000)) return fail("anchored body did not run");
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("handed-off state must not drop on the anchored happy path");
    }
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("anchor lease release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return fail("anchored happy path leaked the dispatch-time pin");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row C: caller cancelled while the anchored execute is BOUND (anchor
// pinned, body created, held at SP_IMMEDIATE_ON_BEFORE_PUBLISH before its
// publication). Mirrors the placement family's cancel-bound row: the body
// still runs (no suspension points; both completions are legal per the
// cancel-route contract), the reply edge resolves with no caller to wake,
// the handed-off state never drops through the pending, and the
// dispatch-time pin is released exactly once at that same reply edge.
static int run_anchored_cancel_bound(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    immediate_exec_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.droppable = 1;
    st.anchored = 1;
    drop_expected_state = &child;

    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle anchor = {0};
    if (!mint_anchor(ex, dst, &anchor)) return fail("anchor mint failed");
    st.anchor = anchor;

    void* caller = __task_create(POLL_IMMEDIATE_CALLER, &st);
    if (caller == NULL) return fail("anchored caller create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_PUBLISH, 5000)) {
        return fail("anchored publish window was never reached");
    }
    rt_task_cancel(caller);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (kind == 1) return fail("cancelled anchored caller must not resolve successfully");
    rt_sync_point_open();
    if (!wait_child(&child, 5000)) return fail("bound anchored body did not run");
    // The reply edge (and its unpin) resolves behind the body completion;
    // give the release chain a settle window before pinning the census.
    sleep_us(50000);
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("handed-off state must not drop after the anchored bind");
    }
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("anchor lease release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return fail("anchored cancel-bound path leaked the dispatch-time pin");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// The anchored twin of the unbound caller-teardown row: the sweep must
// resolve an UNBOUND anchored execute exactly like a placement one, so
// the late dispatch refuses at its snapshot check BEFORE the anchor pin
// — no body, no pin, the sole-owner pending drops the state once.
static int run_anchored_cancel_unbound(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    immediate_exec_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.droppable = 1;
    st.anchored = 1;
    drop_expected_state = &child;

    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle anchor = {0};
    if (!mint_anchor(ex, dst, &anchor)) return fail("anchor mint failed");
    st.anchor = anchor;

    void* caller = __task_create(POLL_IMMEDIATE_CALLER, &st);
    if (caller == NULL) return fail("anchored caller create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_DISPATCH, 5000)) {
        return fail("anchored dispatch window was never reached");
    }
    rt_task_cancel(caller);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (kind == 1) return fail("cancelled anchored caller must not resolve successfully");
    rt_sync_point_open();
    if (!wait_drops(1, 5000)) {
        return fail("unbound anchored cancel must drop the state exactly once");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 0) {
        return fail("unbound anchored cancel must refuse body creation");
    }
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("anchor lease release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return fail("refused anchored dispatch must not touch the pin");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Remote select rows (Epic 20 Task 7 rows 2-5): deterministic runtime races
// over Copy payloads on the same execute/reply pending discipline the
// anchored rows already prove. Every row uses a SEND arm into a freshly
// minted, buffered (capacity-1) far channel: rt_select_poll's SEND case
// pushes the value into the channel AS PART of committing the winner, so
// the commit is deterministic on the body's first poll (no parking), and
// the driver can verify exactly-once delivery afterward straight off the
// local channel object (mint_channel_anchor hands back both the far handle
// and the local pointer).

// Row 2: cancel-vs-commit race. The body is held at the new
// SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY window with the winner already
// committed (the send already landed in the channel). Cancelling the
// caller from there races a caller-side cancel against the still-unsent
// reply. The caller resolves as Cancelled immediately (rt_async_yield's
// own cancelled check short-circuits its retry poll -- the pending stays
// alive, still owner-registered, so the eventual reply is unaffected) --
// so the row tracks the PENDING directly (an extra driver-held reference)
// rather than trusting the caller's own outcome.
static int run_far_select_cancel_vs_commit(void) {
    rt_executor* ex = ensure_exec();
    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle anchor = {0};
    void* channel = mint_channel_anchor(ex, dst, 1, &anchor);
    if (channel == NULL) return fail("select channel mint failed");

    select_exec_state st;
    memset(&st, 0, sizeof(st));
    st.anchors[0] = anchor;
    st.anchor_ptrs[0] = &st.anchors[0];
    st.kinds[0] = SELECT_CHAN_SEND;
    st.send_bits[0] = 42;
    st.send_drop_ids[0] = DROP_SELECT_PAYLOAD;
    st.count = 1;
    st.droppable = 1;
    int state_marker = 0;
    st.body_state = &state_marker;
    drop_expected_state = &state_marker;

    void* caller = __task_create(POLL_SELECT_CALLER, &st);
    if (caller == NULL) return fail("select caller create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY, 5000)) {
        return fail("commit window was never reached");
    }
    rt_remote_task_pending* req = wait_select_pending_shared(&st, 5000);
    if (req == NULL) return fail("select pending missing at commit window");
    rt_remote_task_pending_add_ref(req);

    rt_task_cancel(caller);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (kind == 1) return fail("cancelled select caller must not resolve successfully");
    rt_sync_point_open();

    if (!wait_task_refs(req, 1, 5000)) return fail("select reply never resolved");
    uint8_t result_kind = 0;
    uint64_t result_bits = 0;
    rt_remote_task_status status = rt_remote_task_pending_snapshot(req, &result_kind, &result_bits);
    if (status != RT_REMOTE_TASK_STATUS_OK || result_kind != 1 || result_bits != 0) {
        return fail("commit-vs-cancel race must still resolve kind=1 with the committed winner");
    }
    if (!channel_recv_once(ex, channel, 42)) {
        return fail("committed send must land in the channel exactly once");
    }
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("handed-off select state must not drop across the commit race");
    }
    if (atomic_load_explicit(&payload_drop_calls, memory_order_acquire) != 0) {
        return fail("committed select payload must stay with its channel");
    }
    rt_remote_task_pending_release(req);
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("select anchor lease release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return fail("commit-vs-cancel race leaked a pin");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row 3: cancel-before-dispatch. The select request is cancelled while
// still UNBOUND, held at the new SP_FAR_SELECT_BEFORE_DISPATCH entry
// window (the select twin of SP_IMMEDIATE_ON_BEFORE_DISPATCH). The
// cancelled caller's own completion resolves the pending through the
// teardown sweep (rt_immediate_on_release_owned, which lists
// RT_REMOTE_TASK_OP_CHANNEL_SELECT) before the late dispatch ever reaches
// the pin loop -- no arm is touched.
static int run_far_select_cancel_before_dispatch(void) {
    rt_executor* ex = ensure_exec();
    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle anchor = {0};
    void* channel = mint_channel_anchor(ex, dst, 1, &anchor);
    if (channel == NULL) return fail("select channel mint failed");

    select_exec_state st;
    memset(&st, 0, sizeof(st));
    st.anchors[0] = anchor;
    st.anchor_ptrs[0] = &st.anchors[0];
    st.kinds[0] = SELECT_CHAN_SEND;
    st.send_bits[0] = 55;
    st.send_drop_ids[0] = DROP_SELECT_PAYLOAD;
    st.count = 1;
    st.droppable = 1;
    int state_marker = 0;
    st.body_state = &state_marker;
    drop_expected_state = &state_marker;

    void* caller = __task_create(POLL_SELECT_CALLER, &st);
    if (caller == NULL) return fail("select caller create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_FAR_SELECT_BEFORE_DISPATCH, 5000)) {
        return fail("dispatch window was never reached");
    }
    rt_task_cancel(caller);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (kind == 1) return fail("cancelled select caller must not resolve successfully");
    rt_sync_point_open();

    if (!wait_drops(1, 5000)) {
        return fail("unbound select cancel must drop the state exactly once");
    }
    if (!wait_payload_drops(1, 5000)) {
        return fail("unbound select cancel must drop the pending payload exactly once");
    }
    if (!channel_is_empty(ex, channel)) {
        return fail("unbound select cancel must never touch the arm");
    }
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("select anchor lease release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return fail("refused select dispatch must not touch the pin");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row 4: double-cancel idempotency. The first cancel route runs through
// rt_task_cancel: the caller's own retry poll routes it, and (since the
// caller resolves as Cancelled through the same yield short-circuit as
// row 2) its own teardown immediately re-attempts the same route --
// cancel_routed already absorbs that inner duplicate. This row adds a
// THIRD, fully independent route: the driver, holding its own reference
// on the same pending, calls rt_immediate_on_cancel_inflight directly.
// The owner shard's single worker is still parked at the armed window
// (the sync point is not opened until after this check), so the first
// route's in-flight cancel-request message cannot possibly be drained
// out from under the before/after snapshot below -- nothing else can be
// touching refs at that instant, a clean proof that the third route is a
// pure no-op once cancel_routed is set.
static int run_far_select_double_cancel(void) {
    rt_executor* ex = ensure_exec();
    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle anchor = {0};
    void* channel = mint_channel_anchor(ex, dst, 1, &anchor);
    if (channel == NULL) return fail("select channel mint failed");

    select_exec_state st;
    memset(&st, 0, sizeof(st));
    st.anchors[0] = anchor;
    st.anchor_ptrs[0] = &st.anchors[0];
    st.kinds[0] = SELECT_CHAN_SEND;
    st.send_bits[0] = 91;
    st.send_drop_ids[0] = DROP_SELECT_PAYLOAD;
    st.count = 1;
    st.droppable = 1;
    int state_marker = 0;
    st.body_state = &state_marker;
    drop_expected_state = &state_marker;

    void* caller = __task_create(POLL_SELECT_CALLER, &st);
    if (caller == NULL) return fail("select caller create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY, 5000)) {
        return fail("commit window was never reached");
    }
    rt_remote_task_pending* req = wait_select_pending_shared(&st, 5000);
    if (req == NULL) return fail("select pending missing at commit window");
    rt_remote_task_pending_add_ref(req);

    rt_task_cancel(caller);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (kind == 1) return fail("cancelled select caller must not resolve successfully");

    uint32_t refs_before = atomic_load_explicit(&req->refs, memory_order_acquire);
    rt_immediate_on_cancel_inflight(ex, req);
    uint32_t refs_after = atomic_load_explicit(&req->refs, memory_order_acquire);
    if (refs_after != refs_before) {
        return fail("a second cancel route must not land once cancel_routed is set");
    }

    rt_sync_point_open();

    if (!wait_task_refs(req, 1, 5000)) return fail("select reply never resolved");
    uint8_t result_kind = 0;
    uint64_t result_bits = 0;
    rt_remote_task_status status = rt_remote_task_pending_snapshot(req, &result_kind, &result_bits);
    if (status != RT_REMOTE_TASK_STATUS_OK || result_kind != 1 || result_bits != 0) {
        return fail("double-cancel must still resolve exactly once with the committed winner");
    }
    if (!channel_recv_once(ex, channel, 91)) {
        return fail("committed send must land exactly once under double-cancel");
    }
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("handed-off select state must not drop under double-cancel");
    }
    if (atomic_load_explicit(&payload_drop_calls, memory_order_acquire) != 0) {
        return fail("committed select payload must not drop under double-cancel");
    }
    rt_remote_task_pending_release(req);
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("select anchor lease release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return fail("double-cancel leaked a pin");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row 5: refusal-after-shipped regression guard. Two arms share the same
// owner shard; arm 0 pins cleanly, arm 1 carries a corrupted (stale)
// anchor generation COPY -- its original lease stays valid, so the
// mint-side registry entry is untouched. The dispatch-time pin loop pins
// arm 0, fails on arm 1, unpins the already-pinned prefix (arm 0), and
// answers STALE_TOKEN before any body exists: the request never reaches
// rt_select_poll, so neither channel is ever touched despite both arms
// carrying live SEND payloads, and the sole-owner pending drops the
// shipped poll state exactly once.
static int run_far_select_refusal_after_shipped(void) {
    rt_executor* ex = ensure_exec();
    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle anchor0 = {0};
    void* channel0 = mint_channel_anchor(ex, dst, 1, &anchor0);
    if (channel0 == NULL) return fail("select channel 0 mint failed");
    rt_far_task_handle anchor1 = {0};
    void* channel1 = mint_channel_anchor(ex, dst, 1, &anchor1);
    if (channel1 == NULL) return fail("select channel 1 mint failed");

    select_exec_state st;
    memset(&st, 0, sizeof(st));
    st.anchors[0] = anchor0;
    st.anchors[1] = anchor1;
    st.anchors[1].generation++;  // corrupt a COPY; the original lease stays valid
    st.anchor_ptrs[0] = &st.anchors[0];
    st.anchor_ptrs[1] = &st.anchors[1];
    st.kinds[0] = SELECT_CHAN_SEND;
    st.kinds[1] = SELECT_CHAN_SEND;
    st.send_bits[0] = 12;
    st.send_bits[1] = 34;
    st.send_drop_ids[0] = DROP_SELECT_PAYLOAD;
    st.send_drop_ids[1] = DROP_SELECT_PAYLOAD;
    st.count = 2;
    st.droppable = 1;
    int state_marker = 0;
    st.body_state = &state_marker;
    drop_expected_state = &state_marker;

    if (!await_select(&st)) return fail("refusal-after-shipped await failed");
    if (st.status != RT_REMOTE_TASK_STATUS_STALE_TOKEN) {
        return fail("stale mid-pin arm must answer STALE_TOKEN");
    }
    if (!wait_drops(1, 5000)) {
        return fail("refused select must drop the shipped poll state exactly once");
    }
    if (!wait_payload_drops(2, 5000)) {
        return fail("refused select must drop each shipped payload exactly once");
    }
    if (!channel_is_empty(ex, channel0) || !channel_is_empty(ex, channel1)) {
        return fail("refused select must never touch send_bits on either arm");
    }
    if (rt_far_channel_release(ex, &anchor0) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &anchor1) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("select anchor lease release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return fail("refusal-after-shipped leaked a pin on one of the arms");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// A reachable synchronous failure before a pending exists: compiler-built
// arms are well formed, but their far leases name different owners. The
// conditional-transfer ABI has already consumed both SEND operands when the
// call starts, so this status must drop each exactly once instead of leaving
// ownership in caller slots that no longer exist in the async state.
static int run_far_select_initial_owner_mismatch(void) {
    rt_executor* ex = ensure_exec();
    if (rt_runtime_shard_count(rt_executor_runtime(ex)) < 2) {
        return fail("owner-mismatch row requires two shards");
    }
    rt_far_task_handle anchor0 = {0};
    rt_far_task_handle anchor1 = {0};
    void* channel0 = mint_channel_anchor(ex, 0, 1, &anchor0);
    void* channel1 = mint_channel_anchor(ex, 1, 1, &anchor1);
    if (channel0 == NULL || channel1 == NULL) return fail("owner-mismatch mint failed");

    select_exec_state st;
    memset(&st, 0, sizeof(st));
    st.anchors[0] = anchor0;
    st.anchors[1] = anchor1;
    st.anchor_ptrs[0] = &st.anchors[0];
    st.anchor_ptrs[1] = &st.anchors[1];
    st.kinds[0] = SELECT_CHAN_SEND;
    st.kinds[1] = SELECT_CHAN_SEND;
    st.send_bits[0] = 101;
    st.send_bits[1] = 202;
    st.send_drop_ids[0] = DROP_SELECT_PAYLOAD;
    st.send_drop_ids[1] = DROP_SELECT_PAYLOAD;
    st.count = 2;

    if (!await_select(&st)) return fail("owner-mismatch await failed");
    if (st.status != RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT) {
        return fail("owner-mismatch select must fail synchronously");
    }
    if (!wait_payload_drops(2, 5000)) {
        return fail("owner-mismatch failure must consume both payloads exactly once");
    }
    if (!channel_is_empty(ex, channel0) || !channel_is_empty(ex, channel1)) {
        return fail("owner-mismatch failure must not touch either channel");
    }
    if (rt_far_channel_release(ex, &anchor0) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &anchor1) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("owner-mismatch lease release failed");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// The API has consumed the owned SEND operand before it validates the anchor
// table. Even a wholly missing table must therefore reclaim every payload it
// can describe instead of leaking the caller's now-unreachable owner.
static int run_far_select_initial_null_anchors(void) {
	select_exec_state st;
	memset(&st, 0, sizeof(st));
	st.null_anchor_array = 1;
	st.kinds[0] = SELECT_CHAN_SEND;
	st.send_bits[0] = 303;
	st.send_drop_ids[0] = DROP_SELECT_PAYLOAD;
	st.count = 1;

	if (!await_select(&st)) return fail("null-anchor select await failed");
	if (st.status != RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT) {
		return fail("null-anchor select must fail synchronously");
	}
	if (st.pending != NULL) return fail("null-anchor select must not create a pending");
	if (!wait_payload_drops(1, 5000)) {
		return fail("null-anchor failure must consume its payload exactly once");
	}
	(void)rt_executor_request_shutdown(ensure_exec());
	return 0;
}

// The arm table and pending both exist before the initial transport enqueue.
// A saturated destination must therefore release through the pending exactly
// once; calling the earlier input-payload cleanup as well would double-drop.
static int run_far_select_enqueue_refusal(void) {
    rt_executor* ex = ensure_exec();
    uint32_t dst = pin_shard(ex, 0);
    rt_far_task_handle anchor = {0};
    void* channel = mint_channel_anchor(ex, dst, 1, &anchor);
    if (channel == NULL) return fail("enqueue-refusal mint failed");

    select_exec_state st;
    memset(&st, 0, sizeof(st));
    st.anchors[0] = anchor;
    st.anchor_ptrs[0] = &st.anchors[0];
    st.kinds[0] = SELECT_CHAN_SEND;
    st.send_bits[0] = 505;
    st.send_drop_ids[0] = DROP_SELECT_PAYLOAD;
    st.count = 1;
    st.fill_queue = 1;

    if (!await_select(&st)) return fail("enqueue-refusal await failed");
    if (st.status != RT_REMOTE_TASK_STATUS_QUEUE_FULL) {
        return fail("enqueue-refusal select must report QUEUE_FULL");
    }
    if (atomic_load_explicit(&payload_drop_calls, memory_order_acquire) != 1) {
        return fail("enqueue-refusal pending must consume the payload exactly once");
    }
    if (!channel_is_empty(ex, channel)) {
        return fail("enqueue-refusal must not touch the channel");
    }
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("enqueue-refusal lease release failed");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// A normal RECV winner returns the losing SEND payload to compiled code
// before the pending disowns/frees its arm table. The harness observes the
// raw handback buffer, then performs the one compiled losing-arm drop itself.
static int run_far_select_recv_winner_handback(void) {
    rt_executor* ex = ensure_exec();
    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle send_anchor = {0};
    rt_far_task_handle recv_anchor = {0};
    void* send_channel = mint_channel_anchor(ex, dst, 0, &send_anchor);
    void* recv_channel = mint_channel_anchor(ex, dst, 1, &recv_anchor);
    if (send_channel == NULL || recv_channel == NULL) return fail("handback mint failed");
    rt_channel_send_blocking(recv_channel, 303);

    select_exec_state st;
    memset(&st, 0, sizeof(st));
    st.anchors[0] = send_anchor;
    st.anchors[1] = recv_anchor;
    st.anchor_ptrs[0] = &st.anchors[0];
    st.anchor_ptrs[1] = &st.anchors[1];
    st.kinds[0] = SELECT_CHAN_SEND;
    st.kinds[1] = SELECT_CHAN_RECV;
    st.send_bits[0] = 404;
    st.send_drop_ids[0] = DROP_SELECT_PAYLOAD;
    st.count = 2;

    if (!await_select(&st)) return fail("handback await failed");
    if (st.status != RT_REMOTE_TASK_STATUS_OK || st.out_kind != 1 || st.out_bits != 1) {
        return fail("handback row must choose the ready RECV arm");
    }
    if (atomic_load_explicit(&payload_drop_calls, memory_order_acquire) != 0) {
        return fail("pending must disown, not drop, a returned losing payload");
    }
    if (st.send_bits[0] != 404) {
        return fail("losing SEND payload was not returned to caller buffer");
    }
    __surge_drop_result_call(DROP_SELECT_PAYLOAD, (void*)st.send_bits[0]);
    if (atomic_load_explicit(&payload_drop_calls, memory_order_acquire) != 1) {
        return fail("compiled losing-arm drop must consume returned payload once");
    }
    if (!channel_is_empty(ex, send_channel) || !channel_is_empty(ex, recv_channel)) {
        return fail("handback row left unexpected channel data");
    }
    if (rt_far_channel_release(ex, &send_anchor) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &recv_anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("handback lease release failed");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

int main(int argc, char** argv) {
    if (argc != 2) return fail("usage: remote_publication_harness <mode>");
    if (strcmp(argv[1], "publish-other") == 0) return run_publish(1, 0);
    if (strcmp(argv[1], "self-crossing") == 0) return run_publish(0, 0);
    if (strcmp(argv[1], "stale-token") == 0) return run_publish(1, 1);
    if (strcmp(argv[1], "queue-full") == 0) return run_queue_full();
    if (strcmp(argv[1], "shutdown") == 0) return run_shutdown();
    if (strcmp(argv[1], "shutdown-queued-kinds") == 0) return run_shutdown_queued_kinds();
    if (strcmp(argv[1], "refusal-drop-queue-full") == 0) return run_refusal_drop(0);
    if (strcmp(argv[1], "refusal-drop-shutdown") == 0) return run_refusal_drop(1);
    if (strcmp(argv[1], "abandon-before-dispatch") == 0) {
        return run_abandon_window(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_DISPATCH);
    }
    if (strcmp(argv[1], "abandon-before-body-publish") == 0) {
        return run_abandon_window(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_BODY_PUBLISH);
    }
    if (strcmp(argv[1], "abandon-before-ack") == 0) {
        return run_abandon_window(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_ACK);
    }
    if (strcmp(argv[1], "ack-rescue-drain") == 0) return run_ack_rescue_drain();
    if (strcmp(argv[1], "stale-request-before-body") == 0) return run_stale_request_before_body();
    if (strcmp(argv[1], "duplicate-request-after-handoff") == 0) return run_stale_redelivery(0);
    if (strcmp(argv[1], "stale-ack-after-resolution") == 0) return run_stale_redelivery(1);
    if (strcmp(argv[1], "immediate-refusal-queue-full") == 0) return run_immediate_refusal_drop(0);
    if (strcmp(argv[1], "immediate-refusal-shutdown") == 0) return run_immediate_refusal_drop(1);
    if (strcmp(argv[1], "immediate-cancel-unbound") == 0) {
        return run_immediate_cancel(RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_DISPATCH, 0);
    }
    if (strcmp(argv[1], "immediate-cancel-bound") == 0) {
        return run_immediate_cancel(RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_PUBLISH, 1);
    }
    if (strcmp(argv[1], "immediate-duplicate-request") == 0) return run_immediate_redelivery(0);
    if (strcmp(argv[1], "immediate-stale-reply") == 0) return run_immediate_redelivery(1);
    if (strcmp(argv[1], "anchored-stale-anchor") == 0) return run_anchored_stale_anchor();
    if (strcmp(argv[1], "anchored-happy-path") == 0) return run_anchored_happy_path();
    if (strcmp(argv[1], "anchored-cancel-bound") == 0) return run_anchored_cancel_bound();
    if (strcmp(argv[1], "anchored-cancel-unbound") == 0) return run_anchored_cancel_unbound();
    if (strcmp(argv[1], "far-select-cancel-vs-commit") == 0) {
        return run_far_select_cancel_vs_commit();
    }
    if (strcmp(argv[1], "far-select-cancel-before-dispatch") == 0) {
        return run_far_select_cancel_before_dispatch();
    }
    if (strcmp(argv[1], "far-select-double-cancel") == 0) return run_far_select_double_cancel();
    if (strcmp(argv[1], "far-select-refusal-after-shipped") == 0) {
        return run_far_select_refusal_after_shipped();
    }
    if (strcmp(argv[1], "far-select-initial-owner-mismatch") == 0) {
        return run_far_select_initial_owner_mismatch();
    }
	if (strcmp(argv[1], "far-select-initial-null-anchors") == 0) {
		return run_far_select_initial_null_anchors();
	}
    if (strcmp(argv[1], "far-select-enqueue-refusal") == 0) {
        return run_far_select_enqueue_refusal();
    }
    if (strcmp(argv[1], "far-select-recv-winner-handback") == 0) {
        return run_far_select_recv_winner_handback();
    }
    return fail("unknown mode");
}
`
