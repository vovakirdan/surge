#include "rt_remote_admit.h"
#include "rt_remote_spawn.h"
#include "rt_remote_task_internal.h"
#include "rt_sync_point.h"
#include "rt_transport.h"
#include "rt_transport_internal.h"

// A request's admission onto the transport: the reply-slot reservation on
// its own shard, the request's slot on the target's, and the producer park
// that replaces a caller-visible QUEUE_FULL on either (rt_remote_admit.h).

void rt_remote_admission_init(rt_remote_admission* adm,
                              const rt_transport_msg* msg,
                              int wants_reply) {
    if (adm == NULL || msg == NULL) {
        return;
    }
    adm->msg = *msg;
    adm->source_shard_id = msg->source_shard_id;
    adm->target_shard_id = msg->target_shard_id;
    adm->wants_reply = wants_reply != 0;
    atomic_store_explicit(&adm->reserved, 0, memory_order_relaxed);
    adm->parked = 0;
    adm->parked_on_source = 0;
}

static rt_shard* admission_shard(rt_executor* ex, uint32_t shard_id) {
    return rt_runtime_shard(rt_executor_runtime(ex), shard_id);
}

void rt_transport_wake_slot_waiters(rt_executor* ex, rt_shard* shard) {
    if (ex == NULL || shard == NULL) {
        return;
    }
    if (rt_lane_holds_any_shard()) {
        panic_msg("transport: slot wake while holding a shard lock");
        return;
    }
    size_t woken = wake_key_all_with_policy(ex, transport_slot_key(shard->shard_id), 0);
    if (woken == 0) {
        return;
    }
    rt_shard_lock(shard);
    shard->transport.data_admission_wakes += woken;
    rt_shard_unlock(shard);
}

void rt_transport_release_reply_slot(rt_executor* ex, rt_shard* shard) {
    if (ex == NULL || shard == NULL) {
        return;
    }
    if (rt_lane_holds_any_shard()) {
        panic_msg("transport: reply slot release while holding a shard lock");
        return;
    }
    rt_shard_lock(shard);
    rt_transport_release_reply_slot_locked(&shard->transport);
    rt_shard_unlock(shard);
    // The slot the reservation held is free again, for a producer parked on
    // this lane as much as for anyone.
    rt_transport_wake_slot_waiters(ex, shard);
}

// Register-then-verify, the shape every park in this runtime takes: the task
// registers on the shard's slot key, records the park, and only then tries
// the lane once more, so a slot freed between the refusal and the
// registration is not missed -- the wake for it would have found nobody.
#ifndef RV2_DEBT_031_NEGATIVE_CONTROL
static void slot_register(rt_executor* ex, rt_task* current, rt_shard* shard) {
    waker_key key = transport_slot_key(shard->shard_id);
    prepare_park(ex, current, key, 0);
    // The key the poll's terminator commits the park on: without it the
    // yield is a plain requeue and the "park" is a spin against the lane.
    pending_key = key;
    rt_transport_record_admission_park(shard);
    RT_SYNC_POINT(SP_TRANSPORT_DATA_SLOT_TASK_PARKED);
}
#endif

static void slot_unregister(rt_executor* ex, rt_task* current, uint32_t shard_id) {
    waker_key key = transport_slot_key(shard_id);
    remove_waiter(ex, key, current->id);
    if (current->park_key.kind == key.kind && current->park_key.id == key.id) {
        current->park_key = waker_none();
    }
    current->park_prepared = 0;
    if (pending_key.kind == key.kind && pending_key.id == key.id) {
        pending_key = waker_none();
    }
}

static void admission_unpark(rt_executor* ex, rt_task* current, rt_remote_admission* adm) {
    if (atomic_exchange_explicit(&adm->parked, 0, memory_order_acq_rel) == 0) {
        return;
    }
    slot_unregister(
        ex, current, adm->parked_on_source ? adm->source_shard_id : adm->target_shard_id);
    adm->parked_on_source = 0;
}

static rt_transport_status
admission_reserve(rt_executor* ex, rt_task* current, rt_remote_admission* adm, rt_shard* source) {
    rt_transport_status status = rt_transport_reserve_reply_slot(source);
    if (status == RT_TRANSPORT_STATUS_OK) {
        atomic_store_explicit(&adm->reserved, 1, memory_order_release);
        return status;
    }
    if (status != RT_TRANSPORT_STATUS_QUEUE_FULL) {
        return status;
    }
#ifdef RV2_DEBT_031_NEGATIVE_CONTROL
    (void)ex;
    (void)current;
    return status;
#else
    slot_register(ex, current, source);
    adm->parked = 1;
    adm->parked_on_source = 1;
    if (rt_transport_reserve_reply_slot(source) == RT_TRANSPORT_STATUS_OK) {
        admission_unpark(ex, current, adm);
        atomic_store_explicit(&adm->reserved, 1, memory_order_release);
        return RT_TRANSPORT_STATUS_OK;
    }
    return RT_TRANSPORT_STATUS_UNAVAILABLE;
#endif
}

static rt_transport_status
admission_enqueue(rt_executor* ex, rt_task* current, rt_remote_admission* adm, rt_shard* target) {
    rt_transport_status status = rt_transport_enqueue(target, &adm->msg);
    if (status != RT_TRANSPORT_STATUS_QUEUE_FULL) {
        return status;
    }
#ifdef RV2_DEBT_031_NEGATIVE_CONTROL
    // The shape before the park: drain the target once, retry once, and hand
    // the refusal to the caller.
    (void)current;
    return rt_remote_spawn_enqueue_with_drain(ex, target, &adm->msg);
#else
    slot_register(ex, current, target);
    adm->parked = 1;
    adm->parked_on_source = 0;
    status = rt_transport_enqueue(target, &adm->msg);
    if (status == RT_TRANSPORT_STATUS_QUEUE_FULL) {
        return RT_TRANSPORT_STATUS_UNAVAILABLE;
    }
    admission_unpark(ex, current, adm);
    return status;
#endif
}

rt_transport_status rt_remote_admit(rt_executor* ex, rt_task* current, rt_remote_admission* adm) {
    if (ex == NULL || current == NULL || adm == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    rt_shard* source = admission_shard(ex, adm->source_shard_id);
    rt_shard* target = admission_shard(ex, adm->target_shard_id);
    if (source == NULL || target == NULL) {
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    // A retry of a parked admission drops its registration first; it is
    // taken again below if the lane still refuses.
    admission_unpark(ex, current, adm);
    // A producer woken by shutdown finds nobody to admit it: the request is
    // refused the way the callers refuse before submitting, and the
    // reservation it may hold goes back with the pending.
    if (atomic_load_explicit(&ex->shutdown, memory_order_acquire) != 0 ||
        atomic_load_explicit(&target->transport.park_state, memory_order_acquire) ==
            RT_TRANSPORT_SHARD_SHUTDOWN) {
        adm->refused_by_shutdown = 1;
        return RT_TRANSPORT_STATUS_INVALID_ARGUMENT;
    }
    if (adm->wants_reply && atomic_load_explicit(&adm->reserved, memory_order_acquire) == 0) {
        rt_transport_status reserved = admission_reserve(ex, current, adm, source);
        if (reserved != RT_TRANSPORT_STATUS_OK) {
            return reserved;
        }
    }
    return admission_enqueue(ex, current, adm, target);
}

int rt_remote_admission_take_reservation(rt_remote_admission* adm) {
    if (adm == NULL) {
        return 0;
    }
    return atomic_exchange_explicit(&adm->reserved, 0, memory_order_acq_rel) != 0;
}

void rt_remote_admission_release_reservation(rt_executor* ex, const rt_remote_admission* adm) {
    if (ex == NULL || adm == NULL) {
        return;
    }
    rt_shard* source = admission_shard(ex, adm->source_shard_id);
    if (source != NULL) {
        rt_transport_release_reply_slot(ex, source);
    }
}

static _Atomic uint64_t admission_orphans;

uint64_t rt_remote_admission_orphan_count(void) {
    return atomic_load_explicit(&admission_orphans, memory_order_relaxed);
}

void rt_remote_admission_release_reservation_belt(rt_executor* ex, const rt_remote_admission* adm) {
    if (ex == NULL || adm == NULL) {
        return;
    }
    rt_shard* source = admission_shard(ex, adm->source_shard_id);
    if (source == NULL) {
        return;
    }
    if (!rt_lane_holds_any_shard()) {
        rt_transport_release_reply_slot(ex, source);
        return;
    }
    if (rt_lane_holds_shard(source->shard_id)) {
        rt_transport_release_reply_slot_locked(&source->transport);
        return;
    }
    (void)atomic_fetch_add_explicit(&admission_orphans, 1, memory_order_relaxed);
}

int rt_remote_admission_abandon(rt_executor* ex, uint64_t task_id, rt_remote_admission* adm) {
    if (ex == NULL || adm == NULL) {
        return 0;
    }
    int ended_park = 0;
    if (atomic_exchange_explicit(&adm->parked, 0, memory_order_acq_rel) != 0) {
        uint32_t shard_id = adm->parked_on_source ? adm->source_shard_id : adm->target_shard_id;
        waker_key key = transport_slot_key(shard_id);
        remove_waiter(ex, key, task_id);
        rt_task* current = rt_current_task();
        if (current != NULL && current->id == task_id) {
            if (current->park_key.kind == key.kind && current->park_key.id == key.id) {
                current->park_key = waker_none();
                current->park_prepared = 0;
            }
            // Abandoned from inside the producer's own poll (a shutdown it
            // requested itself): the terminator must not commit a park on a
            // key nobody is registered under any more.
            if (pending_key.kind == key.kind && pending_key.id == key.id) {
                pending_key = waker_none();
            }
        }
        adm->parked_on_source = 0;
        ended_park = 1;
    }
    if (rt_remote_admission_take_reservation(adm)) {
        rt_remote_admission_release_reservation(ex, adm);
    }
    return ended_park;
}

// The remote-task family's two entry points into the admission: the first
// submission, and the poll after a park.

rt_remote_task_status
rt_remote_task_submit(rt_executor* ex, rt_task* current, rt_remote_task_pending* request) {
    rt_transport_status status = rt_remote_admit(ex, current, &request->admission);
    if (status == RT_TRANSPORT_STATUS_OK) {
        (void)rt_remote_task_prepare_reply_wait(ex, current, request);
        return RT_REMOTE_TASK_STATUS_PENDING;
    }
    if (status == RT_TRANSPORT_STATUS_UNAVAILABLE) {
        // Parked on a slot key; the caller suspends and polls again on wake.
        return RT_REMOTE_TASK_STATUS_PENDING;
    }
    return rt_remote_task_transport_status(status);
}

int rt_remote_task_retry_admission(rt_executor* ex,
                                   rt_task* current,
                                   rt_remote_task_pending* pending) {
    if (atomic_load_explicit(&pending->admission.parked, memory_order_acquire) == 0) {
        return RT_REMOTE_ADMISSION_ADMITTED;
    }
    // Shutdown outranks the caller's own cancellation: a carrier exiting
    // cancels what it still holds, and the request's answer is the
    // destination's, not the caller's.
    if (atomic_load_explicit(&ex->shutdown, memory_order_acquire) == 0 &&
        task_cancelled_load(current) != 0) {
        // A cancelled caller stops asking: nothing was sent, so nothing has
        // to be cancelled anywhere else. The message reference the first
        // submission took is released with the request that never left --
        // unless a teardown sweep ended the park first and released it.
        if (rt_remote_admission_abandon(ex, current->id, &pending->admission)) {
            rt_remote_task_pending_release(pending);
        }
        rt_remote_task_pending_finish(ex, pending, RT_REMOTE_TASK_STATUS_OK, 2, NULL);
        return RT_REMOTE_ADMISSION_FINISHED;
    }
    rt_transport_status status = rt_remote_admit(ex, current, &pending->admission);
    if (status == RT_TRANSPORT_STATUS_UNAVAILABLE) {
        return RT_REMOTE_ADMISSION_PARKED;
    }
    if (status != RT_TRANSPORT_STATUS_OK) {
        rt_remote_task_status refusal = pending->admission.refused_by_shutdown
                                            ? RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN
                                            : rt_remote_task_transport_status(status);
        rt_remote_task_pending_finish(ex, pending, refusal, 2, NULL);
        rt_remote_task_pending_release(pending);
        return RT_REMOTE_ADMISSION_FINISHED;
    }
    return RT_REMOTE_ADMISSION_ADMITTED;
}

rt_remote_spawn_status rt_remote_spawn_admission_refusal(const rt_remote_admission* adm,
                                                         rt_transport_status status) {
    if (adm != NULL && adm->refused_by_shutdown) {
        return RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN;
    }
    if (status == RT_TRANSPORT_STATUS_QUEUE_FULL) {
        return RT_REMOTE_SPAWN_STATUS_QUEUE_FULL;
    }
    return RT_REMOTE_SPAWN_STATUS_REFUSED;
}

void rt_remote_admission_wake_all_parked(rt_executor* ex) {
    if (ex == NULL) {
        return;
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    size_t shard_count = rt_runtime_shard_count(runtime);
    for (size_t i = 0; i < shard_count; i++) {
        rt_shard* shard = rt_runtime_shard(runtime, i);
        if (shard != NULL) {
            rt_transport_wake_slot_waiters(ex, shard);
        }
    }
}
