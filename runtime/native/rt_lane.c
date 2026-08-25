#include "rt_async_internal.h"

#include <stdlib.h>

// Lock-lane discipline for the executor lock split (spike decision
// D2): a thread may hold the control lock and then at most one shard lock.
// Taking the control lock while holding a shard lock, taking a second shard
// lock, or re-entering the control lock are ownership-model violations, not
// recoverable errors, so they panic. The thread-local record is two plain
// stores per acquisition; it stays on in every build.

typedef struct {
    uint8_t holds_control;
    // 0 = none; otherwise shard_id + 1, so shard 0 is distinguishable.
    uint32_t shard_id_plus_one;
} rt_lane_tls_state;

static _Thread_local rt_lane_tls_state lane_state;

static int lane_debug_cached = -1;

int rt_lane_debug_enabled(void) {
    if (lane_debug_cached >= 0) {
        return lane_debug_cached;
    }
    const char* value = getenv("SURGE_LANE_DEBUG");
    if (value == NULL || value[0] == '\0' || (value[0] == '0' && value[1] == '\0')) {
        lane_debug_cached = 0;
        return 0;
    }
    lane_debug_cached = 1;
    return 1;
}

int rt_lane_holds_control(void) {
    return lane_state.holds_control != 0;
}

int rt_lane_holds_any_shard(void) {
    return lane_state.shard_id_plus_one != 0;
}

int rt_lane_holds_shard(uint32_t shard_id) {
    return lane_state.shard_id_plus_one == shard_id + 1U;
}

void rt_control_lock(rt_executor* ex) {
    if (ex == NULL) {
        return;
    }
    if (lane_state.holds_control) {
        panic_msg("lane: control lock is not reentrant");
        return;
    }
    if (lane_state.shard_id_plus_one != 0) {
        panic_msg("lane: control lock requested while holding a shard lock");
        return;
    }
    pthread_mutex_lock(&ex->lock);
    lane_state.holds_control = 1;
    rt_trace_control_lock_acquired();
}

// Work that may not run while this lane holds a scheduler lock.
//
// The lane knows only that such work exists, never what it is: the queue lives
// with whoever defers, and this file stays free of allocation so the minimal
// C stands that link only the lane keep linking. The symbol is weak for the
// same reason -- a stand without the channel lane resolves it to null and asks
// nothing of it.
extern void rt_channel_reclaim_drain(void) __attribute__((weak));
extern void rt_task_reclaim_drain(void) __attribute__((weak));

static void lane_run_deferred(void) {
    if (rt_channel_reclaim_drain != NULL) {
        rt_channel_reclaim_drain();
    }
    if (rt_task_reclaim_drain != NULL) {
        rt_task_reclaim_drain();
    }
}

// Runs the deferred work now, for a lane that is about to stop existing: a
// worker thread on its way out holds no lock and owns a queue nobody else can
// reach, so what it deferred has to be done here or not at all.
void rt_lane_run_deferred_now(void) {
    if (!lane_state.holds_control && !rt_lane_holds_any_shard()) {
        lane_run_deferred();
    }
}

void rt_control_unlock(rt_executor* ex) {
    if (ex == NULL) {
        return;
    }
    lane_state.holds_control = 0;
    pthread_mutex_unlock(&ex->lock);
    // Work that had to wait for the lane to be free runs here, at the point
    // the lane actually becomes free -- not at the call site that happened to
    // request it, which cannot know whether an outer frame still holds a lock.
    if (!rt_lane_holds_any_shard()) {
        lane_run_deferred();
    }
}

void rt_shard_lock(rt_shard* shard) {
    if (shard == NULL) {
        return;
    }
    if (lane_state.shard_id_plus_one != 0) {
        panic_msg("lane: a second shard lock is forbidden");
        return;
    }
    pthread_mutex_lock(&shard->lock);
    lane_state.shard_id_plus_one = shard->shard_id + 1U;
}

void rt_shard_unlock(rt_shard* shard) {
    if (shard == NULL) {
        return;
    }
    lane_state.shard_id_plus_one = 0;
    pthread_mutex_unlock(&shard->lock);
    // Same rule as the control lane: work deferred under this shard runs once
    // the lane holds nothing at all.
    if (!rt_lane_holds_control()) {
        lane_run_deferred();
    }
}

rt_runtime_status rt_shard_sync_init(rt_shard* shard) {
    if (shard == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    if (pthread_mutex_init(&shard->lock, NULL) != 0) {
        return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
    }
    if (pthread_cond_init(&shard->worker_cv, NULL) != 0) {
        (void)pthread_mutex_destroy(&shard->lock);
        return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
    }
    if (pthread_cond_init(&shard->poller_cv, NULL) != 0) {
        (void)pthread_cond_destroy(&shard->worker_cv);
        (void)pthread_mutex_destroy(&shard->lock);
        return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
    }
    return RT_RUNTIME_STATUS_OK;
}

void rt_shard_sync_destroy(rt_shard* shard) {
    if (shard == NULL) {
        return;
    }
    (void)pthread_cond_destroy(&shard->poller_cv);
    (void)pthread_cond_destroy(&shard->worker_cv);
    (void)pthread_mutex_destroy(&shard->lock);
}
