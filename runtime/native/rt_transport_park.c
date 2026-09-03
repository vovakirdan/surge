#include "rt_sync_point.h"
#include "rt_transport_internal.h"

// The transport's shard-facing half, split out of rt_transport.c with no
// change in behaviour: admission (enqueue against the class budgets), the
// park/wake protocol a shard's worker runs against the queues, shutdown, and
// the drains. The queues themselves and the wake pipe stay in rt_transport.c
// (rt_transport_internal.h is the seam).

#ifdef RT_TRANSPORT_NEG_RELAXED_PARK_ORDER
static uint8_t rt_transport_negative_relaxed_state(void) {
    volatile uint8_t state = RT_TRANSPORT_SHARD_RUNNING;
    return state;
}
#endif

#ifdef RT_TRANSPORT_NEG_SHUTDOWN_NO_WAKE
static void rt_transport_negative_shutdown_no_wake_touch(rt_executor* ex) {
    uint8_t shutdown = atomic_load_explicit(&ex->shutdown, memory_order_relaxed);
    atomic_store_explicit(&ex->shutdown, shutdown, memory_order_relaxed);
}
#endif

static void rt_transport_worker_wake_locked(rt_shard* shard) {
    rt_scheduler* scheduler = shard != NULL ? &shard->scheduler : NULL;
    if (shard == NULL || scheduler == NULL) {
        return;
    }
    if (scheduler->wake_pending < UINT32_MAX) {
        scheduler->wake_pending++;
    }
    pthread_cond_signal(&shard->worker_cv);
}

static rt_transport_status
rt_transport_enqueue_locked(rt_shard* shard, const rt_transport_msg* msg, int reserved_reply) {
    if (shard == NULL || msg == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    rt_transport_msg_class cls = rt_transport_msg_class_of(msg->kind);
    if (cls == RT_TRANSPORT_MSG_CLASS_INVALID) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    rt_transport_status status =
        reserved_reply ? rt_transport_push_reserved_reply_locked(&shard->transport, msg)
                       : rt_transport_push_locked(
                             &shard->transport, msg, cls == RT_TRANSPORT_MSG_CLASS_CONTROL);
    if (status != RT_TRANSPORT_STATUS_OK) {
        return status;
    }
    atomic_thread_fence(memory_order_seq_cst);
    RT_SYNC_POINT(SP_TRANSPORT_AFTER_PUBLISH_BEFORE_STATE_LOAD);
#ifdef RT_TRANSPORT_NEG_RELAXED_PARK_ORDER
    (void)atomic_load_explicit(&shard->transport.park_state, memory_order_relaxed);
    uint8_t park_state = rt_transport_negative_relaxed_state();
#else
    uint8_t park_state = atomic_load_explicit(&shard->transport.park_state, memory_order_seq_cst);
#endif
    RT_SYNC_POINT(SP_TRANSPORT_AFTER_STATE_LOAD_BEFORE_WAKE);
    if (park_state == RT_TRANSPORT_SHARD_PARKED) {
#ifndef RT_TRANSPORT_NEG_SKIP_PARKED_WAKE
        shard->transport.transport_wake_writes++;
        rt_transport_wake_write(&shard->transport);
        rt_transport_worker_wake_locked(shard);
#endif
        return RT_TRANSPORT_STATUS_OK;
    }
#ifdef RT_TRANSPORT_NEG_WRITE_RUNNING_WAKE
    shard->transport.transport_wake_writes++;
    rt_transport_wake_write(&shard->transport);
    rt_transport_worker_wake_locked(shard);
#else
    shard->transport.transport_wake_elisions++;
#endif
    return RT_TRANSPORT_STATUS_OK;
}

static rt_transport_status
rt_transport_enqueue_with(rt_shard* shard, const rt_transport_msg* msg, int reserved_reply) {
    if (shard == NULL || msg == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    if (rt_lane_holds_shard(shard->shard_id)) {
        return rt_transport_enqueue_locked(shard, msg, reserved_reply);
    }
    if (rt_lane_holds_any_shard()) {
        panic_msg("transport: enqueue while holding another shard lock");
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    rt_shard_lock(shard);
    rt_transport_status status = rt_transport_enqueue_locked(shard, msg, reserved_reply);
    rt_shard_unlock(shard);
    return status;
}

rt_transport_status rt_transport_enqueue(rt_shard* shard, const rt_transport_msg* msg) {
    return rt_transport_enqueue_with(shard, msg, 0);
}

rt_transport_status rt_transport_enqueue_reserved_reply(rt_shard* shard,
                                                        const rt_transport_msg* msg) {
    return rt_transport_enqueue_with(shard, msg, 1);
}

rt_transport_status rt_transport_reserve_reply_slot(rt_shard* shard) {
    if (shard == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    if (rt_lane_holds_shard(shard->shard_id)) {
        return rt_transport_reserve_reply_slot_locked(&shard->transport);
    }
    if (rt_lane_holds_any_shard()) {
        panic_msg("transport: reply slot reservation while holding another shard lock");
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    rt_shard_lock(shard);
    rt_transport_status status = rt_transport_reserve_reply_slot_locked(&shard->transport);
    rt_shard_unlock(shard);
    return status;
}

int rt_transport_msg_is_data(rt_transport_msg_kind kind) {
    return rt_transport_msg_class_of(kind) == RT_TRANSPORT_MSG_CLASS_DATA;
}

// rt_transport_wake_slot_waiters and rt_transport_release_reply_slot live in
// rt_remote_admit.c: they wake tasks through the waiter store, and the
// transport's own stands link the queues without a scheduler.

void rt_transport_record_admission_park(rt_shard* shard) {
    if (shard == NULL) {
        return;
    }
    int need_lock = !rt_lane_holds_shard(shard->shard_id);
    if (need_lock) {
        rt_shard_lock(shard);
    }
    shard->transport.data_admission_parks++;
    if (need_lock) {
        rt_shard_unlock(shard);
    }
}

// A wake byte is written only under this shard's lock, and only immediately
// after a message was pushed -- so a byte can exist only while the queue is
// non-empty, and whoever removes the last message owns the drain. A caller
// that removed nothing cannot be holding an unconsumed byte and has nothing to
// read. Draining anyway spends a read(2) inside the shard's critical section
// on every worker turn, and under the default one-shard-by-N-carriers topology
// that critical section is the one every carrier is queued for.
//
// RT_TRANSPORT_NEG_DRAIN_EMPTY_QUEUE restores the unconditional drain, which
// MUST make the empty-queue row count reads that answer nothing.
static int rt_transport_should_drain_wake(const rt_shard* shard, int removed) {
#ifdef RT_TRANSPORT_NEG_DRAIN_EMPTY_QUEUE
    (void)removed;
    return rt_transport_inbound_len_locked(shard) == 0;
#else
    return removed != 0 && rt_transport_inbound_len_locked(shard) == 0;
#endif
}

rt_transport_status rt_transport_try_drain_one(rt_shard* shard, rt_transport_msg* out) {
    if (shard == NULL || out == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    if (rt_lane_holds_shard(shard->shard_id)) {
        rt_transport_status status = rt_transport_pop_locked(&shard->transport, out, 1);
        if (status == RT_TRANSPORT_STATUS_UNAVAILABLE) {
            status = rt_transport_pop_locked(&shard->transport, out, 0);
        }
        if (rt_transport_should_drain_wake(shard, status == RT_TRANSPORT_STATUS_OK)) {
            rt_transport_wake_drain(&shard->transport);
        }
        return status;
    }
    if (rt_lane_holds_any_shard()) {
        panic_msg("transport: drain while holding another shard lock");
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    rt_shard_lock(shard);
    rt_transport_status status = rt_transport_pop_locked(&shard->transport, out, 1);
    if (status == RT_TRANSPORT_STATUS_UNAVAILABLE) {
        status = rt_transport_pop_locked(&shard->transport, out, 0);
    }
    if (rt_transport_should_drain_wake(shard, status == RT_TRANSPORT_STATUS_OK)) {
        rt_transport_wake_drain(&shard->transport);
    }
    rt_shard_unlock(shard);
    return status;
}

static rt_transport_status rt_transport_prepare_shard_park_locked(rt_shard* shard) {
    if (shard == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    RT_SYNC_POINT(SP_TRANSPORT_AFTER_DRAIN_BEFORE_PARK);
#ifdef RT_TRANSPORT_NEG_RELAXED_PARK_ORDER
    atomic_store_explicit(
        &shard->transport.park_state, RT_TRANSPORT_SHARD_PARKED, memory_order_relaxed);
#else
    atomic_store_explicit(
        &shard->transport.park_state, RT_TRANSPORT_SHARD_PARKED, memory_order_seq_cst);
#endif
    RT_SYNC_POINT(SP_TRANSPORT_AFTER_PARK_BEFORE_RECHECK);
#ifdef RT_TRANSPORT_NEG_SKIP_RECHECK
    return RT_TRANSPORT_STATUS_OK;
#else
    if (rt_transport_inbound_len_locked(shard) != 0) {
        atomic_store_explicit(
            &shard->transport.park_state, RT_TRANSPORT_SHARD_RUNNING, memory_order_seq_cst);
        return RT_TRANSPORT_STATUS_UNAVAILABLE;
    }
    return RT_TRANSPORT_STATUS_OK;
#endif
}

rt_transport_status rt_transport_prepare_shard_park(rt_shard* shard) {
    if (shard == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    if (rt_lane_holds_shard(shard->shard_id)) {
        return rt_transport_prepare_shard_park_locked(shard);
    }
    if (rt_lane_holds_any_shard()) {
        panic_msg("transport: park while holding another shard lock");
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    rt_shard_lock(shard);
    rt_transport_status status = rt_transport_prepare_shard_park_locked(shard);
    rt_shard_unlock(shard);
    return status;
}

void rt_transport_mark_shard_running(rt_shard* shard) {
    if (shard == NULL) {
        return;
    }
    if (rt_lane_holds_shard(shard->shard_id)) {
        if (atomic_load_explicit(&shard->transport.park_state, memory_order_seq_cst) !=
            RT_TRANSPORT_SHARD_SHUTDOWN) {
            atomic_store_explicit(
                &shard->transport.park_state, RT_TRANSPORT_SHARD_RUNNING, memory_order_seq_cst);
        }
        return;
    }
    if (rt_lane_holds_any_shard()) {
        panic_msg("transport: mark running while holding another shard lock");
        return;
    }
    rt_shard_lock(shard);
    rt_transport_mark_shard_running(shard);
    rt_shard_unlock(shard);
}

uint64_t rt_transport_shutdown_wake_all(rt_executor* ex) {
    if (ex == NULL || ex->runtime == NULL) {
        return 0;
    }
    RT_SYNC_POINT(SP_TRANSPORT_SHUTDOWN_BEFORE_WAKE);
#ifdef RT_TRANSPORT_NEG_SHUTDOWN_NO_WAKE
    rt_transport_negative_shutdown_no_wake_touch(ex);
    return 0;
#else
    rt_runtime* runtime = ex->runtime;
    uint64_t wakes = 0;
    size_t count = runtime->shard_count;
    if (count > RT_RUNTIME_MAX_SHARDS) {
        count = RT_RUNTIME_MAX_SHARDS;
    }
    for (size_t i = 0; i < count; i++) {
        rt_shard* shard = &runtime->shards[i];
        rt_shard_lock(shard);
        uint8_t state = atomic_load_explicit(&shard->transport.park_state, memory_order_seq_cst);
        atomic_store_explicit(
            &shard->transport.park_state, RT_TRANSPORT_SHARD_SHUTDOWN, memory_order_seq_cst);
        shard->transport.shutdown_wakes++;
        shard->transport.transport_wake_writes++;
        rt_transport_wake_write(&shard->transport);
        rt_transport_worker_wake_locked(shard);
        wakes++;
        if (state == RT_TRANSPORT_SHARD_PARKED) {
            rt_transport_wake_drain(&shard->transport);
        }
        rt_shard_unlock(shard);
    }
    return wakes;
#endif
}

size_t rt_transport_drain_inbound_locked(rt_shard* shard, size_t limit) {
    if (shard == NULL) {
        return 0;
    }
    size_t drained = 0;
    rt_transport_msg msg = {0};
    while ((limit == 0 || drained < limit) &&
           rt_transport_pop_locked(&shard->transport, &msg, 1) == RT_TRANSPORT_STATUS_OK) {
        drained++;
    }
    while ((limit == 0 || drained < limit) &&
           rt_transport_pop_locked(&shard->transport, &msg, 0) == RT_TRANSPORT_STATUS_OK) {
        drained++;
    }
    if (rt_transport_should_drain_wake(shard, drained > 0)) {
        rt_transport_wake_drain(&shard->transport);
    }
    return drained;
}

int rt_transport_reply_wait_before_task_suspend(void) {
    RT_SYNC_POINT(SP_TRANSPORT_REPLY_WAIT_BEFORE_TASK_SUSPEND);
#ifdef RT_TRANSPORT_NEG_REPLY_WAIT_PARKS_SHARD
    return 0;
#else
    return 1;
#endif
}
