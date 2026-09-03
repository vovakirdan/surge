#ifndef SURGE_RUNTIME_NATIVE_RT_REMOTE_TASK_INTERNAL_H
#define SURGE_RUNTIME_NATIVE_RT_REMOTE_TASK_INTERNAL_H

#include "rt_async_internal.h"
#include "rt_remote_task.h"
#include "rt_task_result.h"

typedef struct rt_far_task_lease rt_far_task_lease;
typedef struct rt_remote_task_state rt_remote_task_state;

typedef enum rt_remote_task_op {
    RT_REMOTE_TASK_OP_AWAIT = 1,
    RT_REMOTE_TASK_OP_CANCEL = 2,
    RT_REMOTE_TASK_OP_RELEASE = 3,
    RT_REMOTE_TASK_OP_EXECUTE = 4,
    RT_REMOTE_TASK_OP_CHANNEL_CREATE = 5,
    RT_REMOTE_TASK_OP_EXECUTE_ANCHORED = 6,
    RT_REMOTE_TASK_OP_CHANNEL_SHARE = 7,
    RT_REMOTE_TASK_OP_CHANNEL_SELECT = 8,
} rt_remote_task_op;

// One remote-select arm: the caller's lease token, the send payload for
// SELECT_CHAN_SEND arms, and the local channel resolved atomically with the
// dispatch-time pin (valid until the reply-edge unpin).
typedef struct rt_far_channel_select_arm {
    rt_far_task_handle anchor;
    void* channel;
    // A SEND arm's payload, at its own type, staged out of the caller's
    // storage when the select was armed. Empty for a RECV arm.
    //
    // The cell is per-arm and not per-pending because a select's arms may span
    // channels of different element types, and it holds the value rather than
    // a word plus a numeric drop id because those two could disagree: one
    // descriptor says how this value moves AND how it is destroyed.
    //
    // It stays the arm's until exactly one thing happens to it: the
    // destination's select MOVES it into the channel (the arm
    // rt_remote_task_pending.select_committed_index names), the caller takes
    // it back on the terminal retry, or the pending's own cleanup destroys it.
    rt_value_cell payload;
    uint8_t kind;
} rt_far_channel_select_arm;

// Arm-count cap for one remote select: source-level selects are small, and
// the cap keeps the dispatch pin loop and the body's stack arrays bounded.
#define RT_FAR_CHANNEL_SELECT_MAX_ARMS 16U

// select_committed_index sentinel: no arm has committed (yet, or the
// pending never reaches a select commit at all, e.g. a channel_on/execute
// op reusing this same struct). Any real committed index is < the arm cap.
#define RT_FAR_CHANNEL_SELECT_NO_COMMIT UINT64_MAX

enum {
    RT_REMOTE_TASK_HANDLE_OPEN = 0,
    RT_REMOTE_TASK_HANDLE_AWAIT = 1,
    RT_REMOTE_TASK_HANDLE_CANCEL = 2,
    RT_REMOTE_TASK_HANDLE_RELEASE = 3,
};

typedef enum rt_far_task_lease_state {
    RT_FAR_TASK_LEASE_OPEN = 0,
    RT_FAR_TASK_LEASE_TRANSFERRING = 1,
    RT_FAR_TASK_LEASE_RETURNING = 2,
    RT_FAR_TASK_LEASE_CONSUMED = 3,
    RT_FAR_TASK_LEASE_RELEASING = 4,
} rt_far_task_lease_state;

struct rt_far_task_lease {
    rt_far_task_handle handle;
    rt_executor* executor;
    rt_task* holder;
    rt_task* result_owner;
    _Atomic uint8_t state;
    _Atomic uint32_t refs;
    struct rt_far_task_lease* next;
};

struct rt_remote_task_pending {
    rt_executor* executor;
    uint64_t request_id;
    rt_far_task_handle handle;
    uint32_t source_shard_id;
    uint8_t op;
    uint8_t status;
    uint8_t reply_status;
    uint8_t result_kind;
    uint8_t owner_registered;
    // The request may remain in flight after its caller has gone away, so
    // request status cannot honestly represent whether the reply-key lifecycle
    // is still open.  This independent owner-lock bit claims its one terminal
    // wake across reply, caller teardown, fail-all, and queued-message drain.
    uint8_t reply_wait_retired;
    uint8_t cancel_routed;
    uint64_t caller_task_id;
    uint64_t body_poll_fn_id;
    void* body_state;
    // Drop obligation for a droppable shipped body state: the
    // publication-accepted handoff contract is documented on the spawn
    // pending twin (rt_remote_spawn_internal.h). Final release is the
    // single drop site while owned.
    uint64_t state_type_id;
    uint8_t state_owned;
    rt_far_task_handle anchor;
    // Anchored blocks only: the local channel resolved atomically with the
    // dispatch-time pin; valid until the reply-edge unpin.
    void* anchored_channel;
    // Anchored blocks only: whether this pending currently holds the pin
    // rt_far_channel_pin took on the anchor. Every path that gives the pin
    // back goes through rt_immediate_on_anchor_unpin, which clears it, so a
    // second call on the same pending is a no-op rather than a second
    // decrement, and a pending that never pinned never unpins.
    uint8_t anchor_pinned;
    // Remote select only: the arm table copied at request time (owned by the
    // pending, freed with it); channels filled by the dispatch-time pins.
    rt_far_channel_select_arm* select_arms;
    uint64_t select_count;
    // Remote select only: the arm index rt_select_poll's commit chose,
    // written once inside that same critical section
    // (rt_far_channel_select.c) and RT_FAR_CHANNEL_SELECT_NO_COMMIT until
    // then. Immutable after that single write — in particular, the
    // shutdown/cancel-inflight sweep that stomps result_kind on
    // any still-pending entry (rt_remote_task_wait.c) must never touch this
    // field, or the free path could wrongly skip-or-drop the wrong arm.
    uint64_t select_committed_index;
    // Channel-create only: the channel's element TYPE, as the id the
    // channel_on::<T> crossing lowering site names it by -- the one form that
    // crosses the boundary -- turned back into its descriptor on the owner
    // shard (rt_channel_element_ops_for) before rt_channel_new sizes a cell.
    // Never 0 from compiled code: every payload type, a scalar included, has
    // the exact descriptor its storage was laid out with.
    uint64_t payload_type_id;
    // Where a task RESULT is, for the reply kinds that carry one. It names the
    // producer's slot rather than copying a value out of it: the transport is
    // in-process, the lease already decides who may adopt, and a value that
    // never becomes a machine word never has to be boxed to fit one. The
    // producer is PINNED while this is set, so the slot cannot be reused or
    // freed under a caller that has not fetched yet.
    rt_result_source result_source;
    // Drop obligation for a landed, heap-carried AWAIT reply the caller
    // never consumed. Threaded from the far Task<T> await/cancel
    // lowering site (the payload type is known there, mirroring
    // result_type_id on rt_task's own owner-side release path);
    // cleared the moment compiled code actually moves the result out
    // of a resolved pending (rt_remote_task_api.c's finish_retry), so a
    // consumed result is never dropped twice. The free path
    // (rt_remote_task_pending_release) is the single drop site while
    // owned, reached either by the normal reply-delivery consume
    // (dispatch_reply) or by the caller-teardown release
    // (rt_remote_task_release_owned) — same function, two possible
    // callers, exactly-once by construction.
    uint64_t result_type_id;
    _Atomic uint32_t refs;
    uint8_t listed;
    struct rt_remote_task_pending* next;
};

struct rt_remote_task_state {
    rt_executor* executor;
    pthread_mutex_t lock;
    _Atomic uint64_t next_request_id;
    rt_remote_task_pending* pending_head;
    rt_far_task_lease* lease_head;
};

// Result-kind byte for TaskResult<T> replies: 2 = Cancelled, 1 = Success.
// Compiled code tests the same two values on the reply edge (the crossing
// emitters compare the reply kind against 1 to pick the success branch).
#define RT_REMOTE_TASK_REPLY_KIND_SUCCESS 1
#define RT_REMOTE_TASK_REPLY_KIND_CANCELLED 2

static inline uint8_t rt_remote_task_result_kind(const rt_task* task) {
    return task != NULL && task->result_kind == TASK_RESULT_CANCELLED
               ? RT_REMOTE_TASK_REPLY_KIND_CANCELLED
               : RT_REMOTE_TASK_REPLY_KIND_SUCCESS;
}

rt_remote_task_state* rt_remote_task_state_get(rt_executor* ex);
rt_far_task_lease* rt_far_task_lease_find_locked(rt_remote_task_state* state,
                                                 const rt_far_task_handle* handle);
rt_remote_task_pending* rt_remote_task_pending_new(rt_executor* ex,
                                                   const rt_far_task_handle* handle,
                                                   uint32_t source_shard_id,
                                                   rt_remote_task_op op,
                                                   int listed);
void rt_remote_task_pending_add_ref(rt_remote_task_pending* pending);
void rt_remote_task_pending_release(rt_remote_task_pending* pending);
void rt_remote_task_pending_consume(rt_remote_task_pending* pending);
rt_remote_task_status rt_remote_task_pending_snapshot(const rt_remote_task_pending* pending,
                                                      uint8_t* out_kind);
// The capability this reply carries, if any. Copied out under the state lock,
// because the pending is shared with the shard that published it.
rt_result_source rt_remote_task_pending_result_source(const rt_remote_task_pending* pending);
// Clears it after the value has been fetched, so no later path can spend the
// same capability twice.
void rt_remote_task_pending_clear_result_source(rt_remote_task_pending* pending);
void rt_remote_task_pending_set_reply(rt_remote_task_pending* pending,
                                      rt_remote_task_status status,
                                      uint8_t result_kind,
                                      const rt_result_source* result_source);
void rt_remote_task_pending_finish(rt_executor* ex,
                                   rt_remote_task_pending* pending,
                                   rt_remote_task_status status,
                                   uint8_t result_kind,
                                   const rt_result_source* result_source);
void rt_remote_task_pending_retire_reply_wait(rt_executor* ex, rt_remote_task_pending* pending);
void rt_remote_task_pending_set_owner_registered(rt_remote_task_pending* pending, int value);
rt_remote_task_pending* rt_remote_task_pending_take_owner(const rt_task* task);

rt_remote_task_status rt_far_task_lease_consume(const rt_far_task_handle* handle);
void rt_far_task_lease_restore(const rt_far_task_handle* handle);
void rt_far_task_lease_drop_ref(rt_far_task_lease* lease);
void rt_far_task_lease_release_route(rt_far_task_lease* lease);
int rt_far_task_adopt_result(rt_task* producer, rt_task* holder);
// Mints a capability for one task's ready result and PINS the task, so the slot
// it names cannot be reused or freed under a caller that has not fetched yet.
// A task with no ready result answers with a zeroed capability and no pin.
rt_result_source rt_remote_task_pin_result(rt_task* task);
// Moves the value a capability names into `out_dst` and releases the pin.
// Answers 0 when the capability names nothing live -- a result already taken, a
// task already gone, a generation that moved on -- which is a condition of a
// racing teardown and not an error to retry.
int rt_remote_task_take_result_source(rt_executor* ex,
                                      const rt_result_source* source,
                                      void* out_dst);
// Releases the pin without taking the value, for a caller that will never
// fetch. What the slot still holds is destroyed by the producer's own dispose.
void rt_remote_task_release_result_source(rt_executor* ex, const rt_result_source* source);
void rt_far_task_release_result(rt_executor* ex, rt_task* producer);

waker_key rt_remote_task_reply_key(uint64_t request_id, uint32_t source_shard_id);
int rt_remote_task_prepare_reply_wait(rt_executor* ex,
                                      rt_task* current,
                                      rt_remote_task_pending* pending);
int rt_remote_task_select_binding_current(rt_far_channel_select_arm** out_arms,
                                          uint64_t* out_count,
                                          void** out_state);
void rt_far_channel_select_unpin_arms(rt_executor* ex,
                                      const rt_remote_task_pending* pending,
                                      uint64_t pinned_count);
void rt_remote_task_clear_reply_wait(rt_executor* ex,
                                     rt_task* current,
                                     const rt_remote_task_pending* pending);

rt_remote_task_status rt_remote_task_transport_status(rt_transport_status status);
void rt_remote_task_reply_owner_done(rt_executor* ex,
                                     rt_task* task,
                                     rt_remote_task_pending* pending);
void rt_immediate_on_dispatch_execute(rt_executor* ex, const rt_transport_msg* msg);
uint32_t rt_immediate_on_source_shard(const rt_task* current);
rt_remote_task_status
rt_immediate_on_finish_retry(rt_remote_task_pending** slot, uint8_t* out_kind, void* out_dst);
void rt_immediate_on_cancel_inflight(rt_executor* ex, rt_remote_task_pending* pending);
// Gives back an anchored pending's pin on its anchor, once (see anchor_pinned).
void rt_immediate_on_anchor_unpin(rt_executor* ex, rt_remote_task_pending* pending);
// The reply that NAMES a task result rather than carrying one. `result_source`
// may be NULL for replies that carry no value at all.
void rt_remote_task_reply_or_finish_with_result(rt_executor* ex,
                                                rt_remote_task_pending* pending,
                                                rt_remote_task_status status,
                                                uint8_t result_kind,
                                                const rt_result_source* result_source,
                                                rt_transport_msg_kind reply_kind);
void rt_remote_task_reply_or_finish(rt_executor* ex,
                                    rt_remote_task_pending* pending,
                                    rt_remote_task_status status,
                                    uint8_t result_kind,
                                    rt_transport_msg_kind reply_kind);

#endif
