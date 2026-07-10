#include "rt_remote_task_internal.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// Remote-channel self-deadlock detection. A caller suspended on an execute
// reply cannot be woken by anything if the body it shipped is parked on a
// channel waiter while the whole executor is quiescent: every shard parked
// with drained queues, no pending timers, no net waiters, and no blocking
// work queued or running. At that point no future event exists that could
// free the channel, so the runtime reports the deadlock with an actionable
// message instead of hanging silently — the remote analog of the local
// external-await "async deadlock" panic.
//
// The check runs only at a worker's idle-park edge (steady-state cost is
// zero) and takes the double-check shape: scan, find a suspect, then
// re-verify global quiescence before panicking, so a wake that lands
// between the two passes (e.g. a blocking completion in the window between
// its running-counter decrement and its wake) cannot produce a false
// positive: the wake makes some shard non-parked and the re-verify bails.
//
// Coverage note: the single-shard single-worker configuration starts no
// worker threads — the awaiting driver thread polls tasks itself — so this
// park-edge check never runs there. That mode is covered by the driver-side
// "async deadlock" panic (rt_async_poll.c) which fires when an external
// await finds nothing runnable.
//
// Blind spot (shared with the driver-side panic): a non-runtime thread that
// touches a channel through FFI is invisible to the quiescence scan. The
// runtime's own tasks cannot trip this falsely — a running task keeps its
// shard non-parked, so quiescence plus a channel-parked body means no
// in-model waker exists. Embedders whose external threads legitimately
// feed or drain channels can opt out with SURGE_REMOTE_DEADLOCK_DETECT=0;
// the default is on in every build.

static int deadlock_detect_enabled(void) {
    static int enabled = -1;
    if (enabled < 0) {
        const char* value = getenv("SURGE_REMOTE_DEADLOCK_DETECT");
        enabled = value == NULL || strcmp(value, "0") != 0;
    }
    return enabled;
}

static int all_shards_quiescent(rt_executor* ex) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    size_t shard_count = rt_runtime_shard_count(runtime);
    for (size_t i = 0; i < shard_count; i++) {
        rt_shard* shard = rt_runtime_shard(runtime, i);
        if (shard == NULL) {
            return 0;
        }
        if (atomic_load_explicit(&shard->transport.park_state, memory_order_seq_cst) !=
            RT_TRANSPORT_SHARD_PARKED) {
            return 0;
        }
        if (rt_sleep_store_min(&shard->sleep_store) != UINT64_MAX) {
            return 0;
        }
        if (rt_net_has_waiters_on_shard(ex, (uint32_t)i)) {
            return 0;
        }
    }
    if (ex->blocking_head != NULL ||
        atomic_load_explicit(&ex->blocking_running, memory_order_acquire) != 0) {
        return 0;
    }
    return 1;
}

// Returns the parked-on-channel body task of a suspended execute pending,
// or NULL. Fields are read best-effort: at genuine quiescence nothing
// mutates them, and the double-check discards racy sightings.
static const rt_task* find_channel_parked_body(rt_executor* ex, const char** op_name) {
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (state == NULL) {
        return NULL;
    }
    const rt_task* suspect = NULL;
    pthread_mutex_lock(&state->lock);
    for (rt_remote_task_pending* it = state->pending_head; it != NULL; it = it->next) {
        if (it->status != RT_REMOTE_TASK_STATUS_PENDING) {
            continue;
        }
        if (it->op != RT_REMOTE_TASK_OP_EXECUTE && it->op != RT_REMOTE_TASK_OP_EXECUTE_ANCHORED) {
            continue;
        }
        if (it->handle.task_id == 0) {
            continue;
        }
        rt_task* body = get_task(ex, it->handle.task_id);
        if (body == NULL || task_status_load(body) != TASK_WAITING) {
            continue;
        }
        uint8_t kind = body->park_key.kind;
        if (kind == WAKER_CHAN_SEND || kind == WAKER_CHAN_RECV) {
            suspect = body;
            *op_name = kind == WAKER_CHAN_SEND ? "send" : "recv";
            break;
        }
    }
    pthread_mutex_unlock(&state->lock);
    return suspect;
}

void rt_remote_task_deadlock_check(rt_executor* ex) {
    if (ex == NULL || atomic_load_explicit(&ex->shutdown, memory_order_acquire) != 0) {
        return;
    }
    if (!deadlock_detect_enabled()) {
        return;
    }
    if (!all_shards_quiescent(ex)) {
        return;
    }
    const char* op_name = "";
    const rt_task* suspect = find_channel_parked_body(ex, &op_name);
    if (suspect == NULL) {
        return;
    }
    if (!all_shards_quiescent(ex)) {
        return;
    }
    static char message[192];
    (void)snprintf(message,
                   sizeof(message),
                   "remote channel deadlock: an anchored block is parked on channel %s "
                   "(body task %llu, shard %u) while every shard is idle; "
                   "nothing can wake it — the channel's consumer is the suspended caller",
                   op_name,
                   (unsigned long long)suspect->id,
                   (unsigned)suspect->owner_shard_id);
    panic_msg(message);
}
