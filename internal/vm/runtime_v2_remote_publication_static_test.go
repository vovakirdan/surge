//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// Contracts read out of the SOURCE rather than out of a run.
//
// These assert shapes a behavioural test cannot see: that a failure path
// releases what it took, that the handoff keeps its ordering, that an initial
// far-select failure owns its payload. A green run proves none of them,
// because the paths they pin are the ones a happy run never takes.

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
		"runtime/native/rt_remote_spawn.c":          "req->state_owned = state_type_id != 0;",
		"runtime/native/rt_immediate_on.c":          "request->state_owned = state_type_id != 0;",
		"runtime/native/rt_immediate_on_anchored.c": "request->state_owned = state_type_id != 0;",
		"runtime/native/rt_far_channel_select.c":    "request->state_owned = state_type_id != 0;",
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
	// The staged payloads are the arms' now, each in its own cell, so the one
	// cleanup this path may run is the cells' own dispose. A second drop of the
	// caller's storage would be destroying a value that has already moved.
	if strings.Count(requestFail, "rt_value_cell_dispose(") != 1 {
		t.Fatal("pending-allocation failure must have exactly one payload-dispose loop")
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
		strings.Contains(enqueueFail, "rt_value_cell_dispose(") {
		t.Fatal("initial-enqueue failure must release only through the pending, not a second direct payload drop")
	}
}
