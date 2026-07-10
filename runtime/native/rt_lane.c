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

void rt_control_unlock(rt_executor* ex) {
    if (ex == NULL) {
        return;
    }
    lane_state.holds_control = 0;
    pthread_mutex_unlock(&ex->lock);
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
