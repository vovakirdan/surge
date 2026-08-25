#ifndef SURGE_RUNTIME_NATIVE_RT_REMOTE_SPAWN_INTERNAL_H
#define SURGE_RUNTIME_NATIVE_RT_REMOTE_SPAWN_INTERNAL_H

#include "rt_async_internal.h"
#include "rt_remote_spawn.h"

struct rt_remote_spawn_pending {
    uint64_t request_id;
    uint64_t poll_fn_id;
    void* state;
    // Drop obligation for a droppable shipped state. The ownership
    // contract for every crossing family (spawn-on, immediate-on,
    // anchored, remote select — the remote-task pending carries the twin
    // fields): pending owns -> body owns, linearized at the
    // PUBLICATION-ACCEPTED HANDOFF, the dispatch-lane store that clears
    // state_owned immediately after rt_remote_spawn_publish_body_task
    // succeeds. Every exit BEFORE the handoff leaves state_owned set, so
    // the final pending release is the single pre-handoff drop site
    // (__surge_drop_call frees captures and envelope together). Every
    // exit AFTER the handoff reclaims compiled-side: the body unpacks
    // the captures at entry and releases the envelope at its return. No
    // path sees both releases because the dispatch lane still holds a
    // pending reference when it clears the flag: the plain store is
    // ordered before that reference's acq_rel refcount drop, so whichever
    // release ends up final observes the cleared flag.
    uint64_t state_type_id;
    uint8_t state_owned;
    // Owner-side drop obligation for the body's heap-carried RESULT,
    // carried from the crossing to the created body task (0 = Copy/inert).
    // Stamped onto task->result_type_id at the publication-accepted
    // handoff; never a caller-side drop site (RV2-DEBT-053a).
    uint64_t result_type_id;
    uint64_t caller_task_id;
    uint32_t source_shard_id;
    uint32_t target_shard_id;
    rt_remote_spawn_status status;
    rt_far_task_handle handle;
    rt_far_task_handle* out_handle;
    _Atomic uint32_t refs;
    uint8_t listed;
    uint8_t abandoned;
    struct rt_remote_spawn_pending* next;
};

void remote_spawn_pending_add_ref(rt_remote_spawn_pending* pending);
void remote_spawn_pending_release(rt_remote_spawn_pending* pending);
void remote_spawn_pending_link(rt_remote_spawn_pending* pending);
void remote_spawn_pending_finish(rt_executor* ex,
                                 rt_remote_spawn_pending* pending,
                                 rt_remote_spawn_status status,
                                 const rt_far_task_handle* handle);
rt_remote_spawn_status remote_spawn_pending_snapshot(const rt_remote_spawn_pending* pending,
                                                     rt_far_task_handle* out);
void remote_spawn_pending_consume(rt_remote_spawn_pending* pending);
void remote_spawn_pending_fail_all(rt_executor* ex, rt_remote_spawn_status status);
// `result_type_id` names the type of the result the body will publish, in the
// only form that survives a crossing: a number the local side turns back into a
// descriptor. Zero is a body with no result value.
rt_remote_spawn_status rt_remote_spawn_create_body_task(rt_executor* ex,
                                                        uint64_t poll_fn_id,
                                                        void* state,
                                                        uint32_t target_shard_id,
                                                        uint64_t result_type_id,
                                                        rt_task** out_task);
rt_remote_spawn_status rt_remote_spawn_publish_body_task(rt_executor* ex, rt_task* task);
void rt_remote_spawn_free_unpublished_task(rt_executor* ex, rt_task* task);

#endif
