#include "rt_async_internal.h"
#include "rt_sync_point.h"
#include "rt_value_ops.h"

#include <stdlib.h>

typedef struct rt_blocking_worker_ctx {
    rt_executor* ex;
    uint32_t worker_id;
} rt_blocking_worker_ctx;

static void blocking_job_release(rt_blocking_job* job) {
    if (job == NULL) {
        return;
    }
    uint32_t refs = atomic_load_explicit(&job->refs, memory_order_relaxed);
    if (refs == 0) {
        return;
    }
    refs = atomic_fetch_sub_explicit(&job->refs, 1, memory_order_acq_rel);
    if (refs != 1) {
        return;
    }
    // The captures, through their own descriptor. Which half runs is the cell's
    // to say: a state the body never claimed is still initialized, so its
    // members are destroyed and then the block; a state the body took is spent,
    // so only the block goes. Freeing the block alone -- what two integers
    // could express -- abandoned every capture inside it, and the width was the
    // only thing they could get right.
    RT_DEBT080_RELEASE_STATE(&job->state);
    // A result nobody came for -- a cancelled awaiter, a torn-down executor --
    // is destroyed here, exactly once, by the cell that owns it.
    rt_value_cell_dispose(&job->result);
    rt_free((uint8_t*)job, sizeof(rt_blocking_job), _Alignof(rt_blocking_job));
}

static void blocking_queue_push(rt_executor* ex, rt_blocking_job* job) {
    if (ex == NULL || job == NULL) {
        return;
    }
    pthread_mutex_lock(&ex->blocking_lock);
    job->next = NULL;
    if (ex->blocking_tail != NULL) {
        ex->blocking_tail->next = job;
    } else {
        ex->blocking_head = job;
    }
    ex->blocking_tail = job;
    pthread_cond_signal(&ex->blocking_cv);
    pthread_mutex_unlock(&ex->blocking_lock);
}

static rt_blocking_job* blocking_queue_pop(rt_executor* ex) {
    if (ex == NULL) {
        return NULL;
    }
    rt_blocking_job* job = ex->blocking_head;
    if (job == NULL) {
        return NULL;
    }
    ex->blocking_head = job->next;
    if (ex->blocking_head == NULL) {
        ex->blocking_tail = NULL;
    }
    job->next = NULL;
    return job;
}

// A job the pool will not run: cancelled before the worker read its status, or
// popped after the pool's shutdown was published. The status is settled to
// CANCELLED here when nothing settled it earlier, so a cancel that raced the
// pop and a shutdown that found the job queued both leave the same terminal
// state; the awaiter is woken only in that case, because a cancel_task already
// woke its own target. The running count and the pool's reference then go the
// way a completed job's do, and the release walks a state the body never
// claimed.
static void blocking_job_settle_unrun(rt_executor* ex, rt_blocking_job* job) {
    uint8_t expected = BLOCKING_JOB_PENDING;
    if (atomic_compare_exchange_strong_explicit(&job->status,
                                                &expected,
                                                BLOCKING_JOB_CANCELLED,
                                                memory_order_acq_rel,
                                                memory_order_acquire)) {
        (void)atomic_fetch_add_explicit(&ex->blocking_cancel_requested, 1, memory_order_relaxed);
        rt_control_lock(ex);
        wake_key_all(ex, blocking_key(job->task_id));
        rt_control_unlock(ex);
    }
    rt_async_debug_printf("async blocking cancelled task=%llu fn=%llu\n",
                          (unsigned long long)job->task_id,
                          (unsigned long long)job->fn_id);
    (void)atomic_fetch_sub_explicit(&ex->blocking_running, 1, memory_order_release);
    blocking_job_release(job);
}

static void* rt_blocking_worker_main(void* arg) {
    rt_blocking_worker_ctx* ctx = (rt_blocking_worker_ctx*)arg;
    rt_executor* ex = ctx != NULL ? ctx->ex : NULL;
    if (ex == NULL) {
        return NULL;
    }
    rt_heap_accounting* accounting = rt_executor_heap_accounting(ex);
    rt_heap_accounting_set_current_cell(
        rt_heap_accounting_blocking_cell(accounting, ctx->worker_id));
    for (;;) {
        pthread_mutex_lock(&ex->blocking_lock);
        while (ex->blocking_head == NULL && !ex->blocking_shutdown) {
            pthread_cond_wait(&ex->blocking_cv, &ex->blocking_lock);
        }
        if (ex->blocking_shutdown && ex->blocking_head == NULL) {
            pthread_mutex_unlock(&ex->blocking_lock);
            // Anything this thread deferred while holding a scheduler lock:
            // its queue is per-thread and the thread is leaving, so what is
            // still in it is reclaimed here or never.
            rt_lane_run_deferred_now();
            rt_heap_accounting_set_current_cell(NULL);
            return NULL;
        }
        rt_blocking_job* job = blocking_queue_pop(ex);
        // Read under the same lock that guards the pop, so a job and the
        // shutdown it was popped under are one observation.
        int draining = ex->blocking_shutdown;
        if (job != NULL) {
            // Claim the job in the running counter before the queue lock is
            // released: a popped-but-unclaimed job would be invisible to
            // idleness checks that read the queue and the counter together.
            (void)atomic_fetch_add_explicit(&ex->blocking_running, 1, memory_order_relaxed);
        }
        pthread_mutex_unlock(&ex->blocking_lock);
        if (job == NULL) {
            continue;
        }
        rt_async_debug_printf("async blocking pop task=%llu fn=%llu state=%p status=%u\n",
                              (unsigned long long)job->task_id,
                              (unsigned long long)job->fn_id,
                              rt_value_cell_value(&job->state),
                              (unsigned)atomic_load_explicit(&job->status, memory_order_relaxed));
        if (draining) {
            // Shutdown drains the queue by cancelling it: every blocking job is
            // drained or transferred at shutdown (docs/runtime-v2-epics/
            // 23-storage-model-and-typed-carrier-abi.md, section 13), and a
            // body that ran now would execute user code the program has
            // already asked to stop, for a result nothing will come for.
            RT_SYNC_POINT(SP_BLOCKING_SHUTDOWN_BEFORE_DRAIN);
            blocking_job_settle_unrun(ex, job);
            continue;
        }

        RT_SYNC_POINT(SP_BLOCKING_POP_BEFORE_STATUS);
        uint8_t status = atomic_load_explicit(&job->status, memory_order_acquire);
        if (status == BLOCKING_JOB_CANCELLED) {
            blocking_job_settle_unrun(ex, job);
            continue;
        }

        void* captures = rt_value_cell_value(&job->state);
        rt_async_debug_printf("async blocking start task=%llu fn=%llu state=%p\n",
                              (unsigned long long)job->task_id,
                              (unsigned long long)job->fn_id,
                              captures);
        // The body writes its answer straight into the job's cell, at the
        // width its own type asks for. It used to RETURN a machine word, so a
        // result wider than one had to be boxed here and adopted again on the
        // other side -- two representations for one value, on a path where the
        // producing thread and the consuming poll never meet.
        void* destination = rt_value_cell_publish_storage(&job->result);
        // The captures become the body's HERE, before it can consume any of
        // them, and the cell records the handover. A cancel racing the body
        // wins its CAS while the body is running -- the status was read once,
        // above -- so the release that follows must be able to tell a state
        // nobody claimed from one already spent. Claiming after the call would
        // leave that window open for exactly as long as the body takes.
        RT_DEBT080_CLAIM_STATE(&job->state);
        RT_SYNC_POINT(SP_BLOCKING_STATE_BEFORE_BODY);
        __surge_blocking_call(job->fn_id, captures, destination);
        if (destination != NULL) {
            (void)rt_value_cell_commit(&job->result);
        }
        rt_async_debug_printf("async blocking done task=%llu fn=%llu\n",
                              (unsigned long long)job->task_id,
                              (unsigned long long)job->fn_id);
        (void)atomic_fetch_add_explicit(&ex->blocking_completed, 1, memory_order_relaxed);

        uint8_t expected = BLOCKING_JOB_PENDING;
        if (atomic_compare_exchange_strong_explicit(&job->status,
                                                    &expected,
                                                    BLOCKING_JOB_DONE,
                                                    memory_order_acq_rel,
                                                    memory_order_acquire)) {
            rt_control_lock(ex);
            wake_key_all(ex, blocking_key(job->task_id));
            rt_control_unlock(ex);
        }
        // The running counter drops only after the completion wake is
        // delivered: idleness checks treat a nonzero count as pending
        // progress, so a decrement before the wake would let them observe
        // a quiescent pool while the woken task is not yet visible.
        (void)atomic_fetch_sub_explicit(&ex->blocking_running, 1, memory_order_release);
        blocking_job_release(job);
    }
}

void rt_blocking_init(rt_executor* ex) {
    if (ex == NULL || ex->blocking_started) {
        return;
    }
    pthread_mutex_init(&ex->blocking_lock, NULL);
    pthread_cond_init(&ex->blocking_cv, NULL);
    ex->blocking_head = NULL;
    ex->blocking_tail = NULL;
    ex->blocking_shutdown = 0;
    atomic_store_explicit(&ex->blocking_running, 0, memory_order_relaxed);
    atomic_store_explicit(&ex->blocking_submitted, 0, memory_order_relaxed);
    atomic_store_explicit(&ex->blocking_completed, 0, memory_order_relaxed);
    atomic_store_explicit(&ex->blocking_cancel_requested, 0, memory_order_relaxed);

    uint32_t count = ex->blocking_count;
    if (count == 0) {
        count = 1;
        ex->blocking_count = count;
    }
    rt_blocking_worker_ctx* ctxs =
        (rt_blocking_worker_ctx*)calloc((size_t)count, sizeof(rt_blocking_worker_ctx));
    if (ctxs == NULL) {
        fatal_oom_msg("async: blocking context allocation failed");
        return;
    }
    pthread_t* threads =
        (pthread_t*)rt_alloc((uint64_t)count * (uint64_t)sizeof(pthread_t), _Alignof(pthread_t));
    if (threads == NULL) {
        free(ctxs);
        fatal_oom_msg("async: blocking worker allocation failed");
        return;
    }
    ex->blocking_worker_ctxs = ctxs;
    ex->blocking_workers = threads;
    for (uint32_t i = 0; i < count; i++) {
        ctxs[i].ex = ex;
        ctxs[i].worker_id = i;
        if (pthread_create(&threads[i], NULL, rt_blocking_worker_main, &ctxs[i]) != 0) {
            panic_msg("async: blocking worker start failed");
            return;
        }
        (void)pthread_detach(threads[i]);
    }
    ex->blocking_started = 1;
}

void rt_blocking_request_cancel(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL || task->kind != TASK_KIND_BLOCKING) {
        return;
    }
    rt_blocking_job* job = (rt_blocking_job*)task->state;
    if (job == NULL) {
        return;
    }
    (void)atomic_store_explicit(&job->cancel_requested, 1, memory_order_release);
    uint8_t expected = BLOCKING_JOB_PENDING;
    if (atomic_compare_exchange_strong_explicit(&job->status,
                                                &expected,
                                                BLOCKING_JOB_CANCELLED,
                                                memory_order_acq_rel,
                                                memory_order_acquire)) {
        (void)atomic_fetch_add_explicit(&ex->blocking_cancel_requested, 1, memory_order_relaxed);
    }
}

poll_outcome poll_blocking_task(rt_executor* ex, rt_task* task) {
    poll_outcome out = {POLL_NONE, waker_none(), NULL};
    if (ex == NULL || task == NULL) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    rt_blocking_job* job = (rt_blocking_job*)task->state;
    if (job == NULL) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (task_cancelled_load(task) != 0) {
        rt_blocking_request_cancel(ex, task);
        blocking_job_release(job);
        task->state = NULL;
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    uint8_t status;
observe_terminal:
    status = atomic_load_explicit(&job->status, memory_order_acquire);
    if (status == BLOCKING_JOB_DONE) {
        out.kind = POLL_DONE_SUCCESS;
        // The value MOVES from the job's cell into the task's, both bound to
        // the same descriptor: this is the one place the producing thread's
        // storage and the awaiting task's storage meet, and a move is what
        // makes exactly one of them own it afterwards.
        void* destination = rt_value_cell_publish_storage(&task->result);
        void* produced = rt_value_cell_value(&job->result);
        if (destination != NULL && produced != NULL) {
            rt_value_move_init_detached(task->result.operations, destination, produced);
            (void)rt_value_cell_commit(&task->result);
            (void)rt_value_cell_commit_move(&job->result);
        }
        blocking_job_release(job);
        task->state = NULL;
        return out;
    }
    if (status == BLOCKING_JOB_CANCELLED) {
        out.kind = POLL_DONE_CANCELLED;
        blocking_job_release(job);
        task->state = NULL;
        return out;
    }
    waker_key key = blocking_key(task->id);
    RT_SYNC_POINT(SP_BLOCKING_POLL_BEFORE_WAIT_REGISTER);
    prepare_park(ex, task, key, 0);
    // Register-then-verify: the blocking worker publishes the terminal job
    // status before draining this key. If it completed after the first status
    // check but before registration, its drain saw no waiter. Re-checking only
    // after the waiter is visible makes either ordering safe.
    status = atomic_load_explicit(&job->status, memory_order_acquire);
    if (RT_DEBT143_POST_REGISTER_TERMINAL(status)) {
        remove_waiter(ex, key, task->id);
        task->park_prepared = 0;
        task->park_key = waker_none();
        goto observe_terminal;
    }
    out.kind = POLL_PARKED;
    out.park_key = key;
    out.state = task->state;
    return out;
}

void* rt_blocking_submit(uint64_t fn_id,
                         void* state,
                         uint64_t state_type_id,
                         uint64_t result_type_id) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    rt_control_lock(ex);
    uint64_t id = ex->next_id++;
    ensure_task_cap(ex, id);
    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), _Alignof(rt_task));
    if (task == NULL) {
        rt_control_unlock(ex);
        fatal_oom_msg("async: blocking task allocation failed");
        return NULL;
    }
    memset(task, 0, sizeof(rt_task));
    // The blocking body's result type, named by the caller: the same
    // descriptor binds the task's result and the job's, which is what lets the
    // value MOVE between them rather than be rebuilt from a word.
    const rt_value_ops* result_ops = rt_channel_element_ops_for(result_type_id);
    if (rt_value_cell_bind(&task->result, result_ops) != RT_SLOT_CONTROL_OK) {
        rt_control_unlock(ex);
        panic_msg("async: a blocking task's result storage could not be reserved");
        return NULL;
    }
    task->id = id;
    task->generation = id;
    task->poll_fn_id = -1;
    task->state = NULL;
    task_status_store(task, TASK_READY);
    task->kind = TASK_KIND_BLOCKING;
    task_cancel_gate_init(task);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->remote_handle_state, 0, memory_order_relaxed);
    atomic_store_explicit(&task->far_task_result_state, 0, memory_order_relaxed);
    atomic_store_explicit(&task->far_task_result_lease, NULL, memory_order_relaxed);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
    rt_task_entitlements_init(&task->entitlements);
    rt_task_slot_store(ex, id, task);
    rt_task* parent = rt_current_task();
    if (parent != NULL) {
        task_add_child(parent, id);
        rt_task_inherit_placement(task, parent);
    }
    rt_task_assign_spawn_owner(task);

    rt_blocking_job* job =
        (rt_blocking_job*)rt_alloc(sizeof(rt_blocking_job), _Alignof(rt_blocking_job));
    if (job == NULL) {
        rt_task_slot_store(ex, id, NULL);
        rt_free((uint8_t*)task, sizeof(rt_task), _Alignof(rt_task));
        rt_control_unlock(ex);
        fatal_oom_msg("async: blocking job allocation failed");
        return NULL;
    }
    memset(job, 0, sizeof(rt_blocking_job));
    if (rt_value_cell_bind(&job->result, result_ops) != RT_SLOT_CONTROL_OK) {
        rt_task_slot_store(ex, id, NULL);
        rt_value_cell_dispose(&task->result);
        rt_free((uint8_t*)job, sizeof(rt_blocking_job), _Alignof(rt_blocking_job));
        rt_free((uint8_t*)task, sizeof(rt_task), _Alignof(rt_task));
        rt_control_unlock(ex);
        panic_msg("async: a blocking job's result storage could not be reserved");
        return NULL;
    }
    // The captures arrive already built, in a block the submission site
    // reserved: the job ADOPTS that block rather than reserving one of its own,
    // which is the only shape that leaves exactly one owner. A submission with
    // no captures passes no block and the cell stays empty.
    if (rt_value_cell_adopt(&job->state, rt_channel_element_ops_for(state_type_id), state) !=
        RT_SLOT_CONTROL_OK) {
        rt_task_slot_store(ex, id, NULL);
        rt_value_cell_dispose(&job->result);
        rt_value_cell_dispose(&task->result);
        rt_free((uint8_t*)job, sizeof(rt_blocking_job), _Alignof(rt_blocking_job));
        rt_free((uint8_t*)task, sizeof(rt_task), _Alignof(rt_task));
        rt_control_unlock(ex);
        panic_msg("async: a blocking job's captures could not be adopted");
        return NULL;
    }
    job->task_id = id;
    job->fn_id = fn_id;
    atomic_store_explicit(&job->status, BLOCKING_JOB_PENDING, memory_order_relaxed);
    atomic_store_explicit(&job->cancel_requested, 0, memory_order_relaxed);
    atomic_store_explicit(&job->refs, 2, memory_order_relaxed);
    task->state = job;

    (void)atomic_fetch_add_explicit(&ex->blocking_submitted, 1, memory_order_relaxed);
    rt_async_debug_printf("async blocking submit task=%llu fn=%llu state=%p type=%llu\n",
                          (unsigned long long)id,
                          (unsigned long long)fn_id,
                          state,
                          (unsigned long long)state_type_id);
    blocking_queue_push(ex, job);
    ready_push(ex, id);
    rt_control_unlock(ex);
    return task;
}
