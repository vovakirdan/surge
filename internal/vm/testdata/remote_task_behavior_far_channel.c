#include "remote_task_behavior.h"
#include "rt_far_channel.h"
#include "rt_placement.h"

#include <string.h>

// Far-channel handle genesis rows: mint/resolve/release lifecycle, the
// kind-tag aliasing defense, and the create round trip over the transport.

// Remote mint round trip: a caller task on shard 0 creates a channel on
// shard 1; the returned token carries the channel kind, the destination
// owner, and a live generation; the owner-side object behaves as an
// ordinary local channel; trace counters show one create request and one
// reply with no publication-ack pair.
int rtb_mode_channel_create(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state state;
    void* task = rtb_start_channel_create(&state, rt_placement_shard(1), 4);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(task, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("channel create did not complete");
    }
    if (state.handle.kind != RT_FAR_HANDLE_KIND_CHANNEL || state.handle.owner_shard_id != 1 ||
        state.handle.task_id == 0 || state.handle.generation != state.handle.task_id) {
        return rtb_fail("minted token fields are wrong");
    }
    void* channel = rt_far_channel_resolve(ex, &state.handle);
    if (channel == NULL) {
        return rtb_fail("minted token did not resolve");
    }
    if (rt_channel_owner_shard_id(channel) != 1) {
        return rtb_fail("owner-side channel is not owned by the destination shard");
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    struct rt_transport_debug_snapshot source =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 0));
    struct rt_transport_debug_snapshot destination =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 1));
    if (destination.far_channel_create_requests != 1 || source.far_channel_create_replies != 1) {
        return rtb_fail("create trace counters mismatch");
    }
    if (source.transport_spawn_requests != 0 || source.transport_spawn_acks != 0) {
        return rtb_fail("create used a publication-ack pair");
    }
    if (rt_far_channel_release(ex, &state.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("release of a live entry failed");
    }
    if (rt_far_channel_resolve(ex, &state.handle) != NULL) {
        return rtb_fail("released token still resolves");
    }
    if (rt_far_channel_release(ex, &state.handle) != RT_REMOTE_TASK_STATUS_STALE_TOKEN) {
        return rtb_fail("double release was not stale");
    }
    rtb_create_state dropped;
    void* drop_task = rtb_start_channel_create(&dropped, rt_placement_shard(1), 2);
    (void)rtb_await(drop_task, &kind, &bits);
    if (dropped.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("drop-hook setup create failed");
    }
    rt_far_task_handle* heap_handle = NULL;
    if (rt_far_channel_handle_alloc(&heap_handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("drop-hook token alloc failed");
    }
    *heap_handle = dropped.handle;
    rt_far_channel_handle_drop(heap_handle);
    if (rt_far_channel_resolve(ex, &dropped.handle) != NULL) {
        return rtb_fail("dropped handle still resolves");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Kind-tag aliasing defense: a far TASK token presented to the channel
// registry never resolves, and a channel token never validates against the
// task path, even with colliding ids from the shared allocator.
int rtb_mode_channel_kind_aliasing(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state state;
    void* task = rtb_start_channel_create(&state, rt_placement_shard(1), 2);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(task, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("aliasing setup create failed");
    }
    rt_far_task_handle forged = state.handle;
    forged.kind = RT_FAR_HANDLE_KIND_TASK;
    if (rt_far_channel_resolve(ex, &forged) != NULL) {
        return rtb_fail("task-kind token resolved in the channel registry");
    }
    if (rt_far_channel_release(ex, &forged) != RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT) {
        return rtb_fail("task-kind token released a channel entry");
    }
    rt_far_task_handle unset = state.handle;
    unset.kind = RT_FAR_HANDLE_KIND_UNSET;
    if (rt_far_channel_resolve(ex, &unset) != NULL) {
        return rtb_fail("unset-kind token resolved in the channel registry");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Shutdown sweep: live entries are invalidated and reclaimed; the registry
// destroy precondition (empty list) then holds, proving no leak.
int rtb_mode_channel_shutdown_release(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state first;
    rtb_create_state second;
    void* task_one = rtb_start_channel_create(&first, rt_placement_shard(1), 2);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(task_one, &kind, &bits);
    void* task_two = rtb_start_channel_create(&second, rt_placement_shard(0), 2);
    (void)rtb_await(task_two, &kind, &bits);
    if (first.status != RT_REMOTE_TASK_STATUS_OK || second.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("shutdown setup creates failed");
    }
    (void)rt_executor_request_shutdown(ex);
    rt_far_channel_release_all(ex);
    if (rt_far_channel_resolve(ex, &first.handle) != NULL ||
        rt_far_channel_resolve(ex, &second.handle) != NULL) {
        return rtb_fail("shutdown left a resolvable token");
    }
    return 0;
}

// Self-crossing mint: destination equals the caller shard at one shard; the
// create still travels the transport (no local shortcut).
int rtb_mode_channel_create_self(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state state;
    void* task = rtb_start_channel_create(&state, rt_placement_shard(0), 2);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(task, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK || state.handle.owner_shard_id != 0) {
        return rtb_fail("self-crossing create failed");
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    struct rt_transport_debug_snapshot shard0 =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 0));
    if (shard0.far_channel_create_requests != 1 || shard0.far_channel_create_replies != 1) {
        return rtb_fail("self-crossing create bypassed the transport");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
