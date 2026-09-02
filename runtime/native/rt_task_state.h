#ifndef SURGE_RUNTIME_NATIVE_RT_TASK_STATE_H
#define SURGE_RUNTIME_NATIVE_RT_TASK_STATE_H
// The per-task state words and the inline helpers that move them: status,
// enqueued, the cancel gate, the wake token, the polling site, and the
// done-waiter count on the executor that pairs with status.
//
// This is a fragment of rt_async_internal.h, not a standalone header. It is
// included from there exactly once, after `struct rt_task` and `rt_executor`
// are complete, and it includes nothing itself: every name it uses is defined
// above its inclusion point. It was split out with no change in behaviour so
// that the header carrying the task and executor definitions stays under the
// closeout's file-size gate while those definitions grow.

static inline uint8_t task_status_load(const rt_task* task) {
    return task == NULL ? TASK_DONE : atomic_load_explicit(&task->status, memory_order_acquire);
}

static inline void task_status_store(rt_task* task, uint8_t status) {
    if (task == NULL) {
        return;
    }
    atomic_store_explicit(&task->status, status, memory_order_release);
}

static inline uint8_t task_status_load_seq_cst(const rt_task* task) {
    return task == NULL ? TASK_DONE : atomic_load_explicit(&task->status, memory_order_seq_cst);
}

static inline void task_status_store_seq_cst(rt_task* task, uint8_t status) {
    if (task == NULL) {
        return;
    }
    atomic_store_explicit(&task->status, status, memory_order_seq_cst);
}

static inline uint32_t rt_done_waiters_load_before_done(const rt_executor* ex) {
    if (ex == NULL) {
        return 0;
    }
#ifdef RV2_DEBT_022_NEGATIVE_CONTROL
    // Deterministic negative control: force the old "missed waiter" branch
    // that the unfenced StoreLoad protocol allowed.
    const volatile uint32_t missed = 0;
    return missed;
#else
    return atomic_load_explicit(&ex->done_waiters, memory_order_acquire);
#endif
}

static inline void rt_done_waiters_increment_for_external_await(rt_executor* ex) {
    if (ex == NULL) {
        return;
    }
#ifdef RV2_DEBT_022_NEGATIVE_CONTROL
    (void)atomic_fetch_add_explicit(&ex->done_waiters, 1, memory_order_acq_rel);
#else
    (void)atomic_fetch_add_explicit(&ex->done_waiters, 1, memory_order_seq_cst);
#endif
}

static inline void rt_done_waiters_decrement_for_external_await(rt_executor* ex) {
    if (ex == NULL) {
        return;
    }
    (void)atomic_fetch_sub_explicit(&ex->done_waiters, 1, memory_order_acq_rel);
}

static inline uint8_t rt_task_status_load_for_external_await(const rt_task* task) {
#ifdef RV2_DEBT_022_NEGATIVE_CONTROL
    return task_status_load(task);
#else
    return task_status_load_seq_cst(task);
#endif
}

static inline void rt_task_status_store_done_for_external_awaiters(rt_task* task) {
#ifdef RV2_DEBT_022_NEGATIVE_CONTROL
    task_status_store(task, TASK_DONE);
#else
    task_status_store_seq_cst(task, TASK_DONE);
#endif
}

static inline uint32_t rt_done_waiters_load_after_done(const rt_executor* ex) {
    if (ex == NULL) {
        return 0;
    }
#ifdef RV2_DEBT_022_NEGATIVE_CONTROL
    return atomic_load_explicit(&ex->done_waiters, memory_order_acquire);
#else
    return atomic_load_explicit(&ex->done_waiters, memory_order_seq_cst);
#endif
}

static inline uint8_t task_enqueued_load(const rt_task* task) {
    return task == NULL ? 0 : atomic_load_explicit(&task->enqueued, memory_order_acquire);
}

static inline void task_enqueued_store(rt_task* task, uint8_t value) {
    if (task == NULL) {
        return;
    }
    atomic_store_explicit(&task->enqueued, value, memory_order_release);
}

// The cancel gate (RV2-DEBT-263). `task->cancelled` is not a flag but a
// three-state word, and a cancel and a completion each move it with ONE
// compare-and-swap out of OPEN: whichever gets there first wins, and the loser's
// CAS fails. rt_task_complete.c owns both transitions and states the argument.
// REQUESTED is the only state that means "a cancel is outstanding", so every
// reader of task_cancelled_load below keeps the meaning it always had: a task
// whose completion sealed the word is committing its answer, not cancelled.
enum { RT_TASK_CANCEL_OPEN = 0, RT_TASK_CANCEL_REQUESTED = 1, RT_TASK_CANCEL_SEALED = 2 };

static inline uint8_t task_cancelled_load(const rt_task* task) {
    return task == NULL ||
           atomic_load_explicit(&task->cancelled, memory_order_acquire) == RT_TASK_CANCEL_REQUESTED;
}

// Opens the gate on a task being created. Every caller has just allocated the
// task and dereferenced it, so there is no NULL case to answer for here.
static inline void task_cancel_gate_init(rt_task* task) {
    atomic_store_explicit(&task->cancelled, RT_TASK_CANCEL_OPEN, memory_order_release);
}

static inline uint8_t task_wake_token_exchange(rt_task* task, uint8_t value) {
    if (task == NULL) {
        return 0;
    }
    return atomic_exchange_explicit(&task->wake_token, value, memory_order_acq_rel);
}

// Poll-entry sites: the polling byte stores WHERE the current poller
// entered, so a double-poll collision can name both sides instead of just
// aborting. Codes are nonzero; zero means "not polling".
typedef enum rt_poll_entry_site {
    POLL_SITE_NONE = 0,
    POLL_SITE_WORKER_LOOP = 1,           // rt_worker_main turn
    POLL_SITE_CONTROL_RUNNER_SYSTEM = 2, // run_ready_one, non-user task
    POLL_SITE_CONTROL_RUNNER_USER = 3,   // run_ready_one, user task
    POLL_SITE_NOWAIT_RUNNER_SYSTEM = 4,  // rt_run_ready_one_nowait_locked, non-user
    POLL_SITE_NOWAIT_RUNNER_USER = 5,    // rt_run_ready_one_nowait_locked, user
    POLL_SITE_INLINE_CHILD = 6,          // poll_ready_child_inline (rt_task_poll)
} rt_poll_entry_site;

void rt_double_poll_panic(const rt_task* task, uint8_t holder_site, uint8_t entrant_site);

static inline void task_polling_enter(rt_task* task, uint8_t site) {
    if (task == NULL) {
        return;
    }
    uint8_t holder = atomic_exchange_explicit(&task->polling, site, memory_order_acq_rel);
    if (holder != 0) {
        rt_double_poll_panic(task, holder, site);
    }
}

static inline void task_polling_exit(rt_task* task) {
    if (task == NULL) {
        return;
    }
    atomic_store_explicit(&task->polling, 0, memory_order_release);
}

#endif
