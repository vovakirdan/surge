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
