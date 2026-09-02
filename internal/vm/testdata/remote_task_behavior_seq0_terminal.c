#include "remote_task_behavior.h"

#include <stdio.h>
#include <string.h>

static size_t seq0_entries(rt_executor* ex, waker_key key, uint64_t task_id) {
    rt_shard* shard = rt_waiter_key_shard(ex, key);
    if (shard == NULL) {
        return 0;
    }
    size_t count = 0;
    rt_shard_lock(shard);
    const rt_waiter_store* store = rt_waiter_store_for_key(ex, key);
    if (store != NULL) {
        for (size_t i = 0; i < store->len; i++) {
            const waiter* entry = &store->entries[i];
            if (entry->key.kind == key.kind && entry->key.id == key.id &&
                entry->key.owner_shard_id == key.owner_shard_id && entry->task_id == task_id &&
                entry->seq == 0) {
                count++;
            }
        }
    }
    rt_shard_unlock(shard);
    return count;
}

static int add_retry_pair(rt_executor* ex, waker_key key, const rt_task* task) {
    add_waiter(ex, key, task->id);
    add_waiter(ex, key, task->id);
    return seq0_entries(ex, key, task->id) == 2;
}

// Returns one only in the Rule-13 build, after proving the omitted terminal
// owner action left the exact two seq=0 registrations behind.  It removes them
// only after the observation so shutdown can still finish cleanly.
static int
observe_terminal_drain(rt_executor* ex, waker_key key, const rt_task* task, const char* row) {
    size_t entries = seq0_entries(ex, key, task->id);
#ifdef RV2_SEQ0_TERMINAL_RETIRE_NEGATIVE_CONTROL
    if (entries != 2) {
        fprintf(stderr, "seq0 terminal mutant mismatch: row=%s entries=%zu want=2\n", row, entries);
        return -1;
    }
    fprintf(stderr, "seq0 terminal mutant stranded: row=%s entries=2\n", row);
    remove_waiter(ex, key, task->id);
    return 1;
#else
    if (entries != 0) {
        fprintf(stderr, "seq0 terminal drain mismatch: row=%s entries=%zu want=0\n", row, entries);
        return -1;
    }
    fprintf(stderr, "seq0 terminal drain: row=%s entries=0\n", row);
    return 0;
#endif
}

static int finish_mode(rt_executor* ex, int observed) {
    (void)rt_executor_request_shutdown(ex);
    if (observed < 0) {
        return 1;
    }
#ifdef RV2_SEQ0_TERMINAL_RETIRE_NEGATIVE_CONTROL
    if (observed == 1) {
        return rtb_fail("seq0 terminal-retire negative control stranded registrations");
    }
#endif
    return 0;
}

int rtb_mode_seq0_blocking_cancel_drain(void) {
    rt_executor* ex = ensure_exec();
    unsigned before = rt_sync_point_reached_count(RT_SYNC_POINT_SP_BLOCKING_POP_BEFORE_STATUS);
    rt_task* task = (rt_task*)rt_blocking_submit(9911, NULL, 0, 0);
    if (task == NULL) {
        return rtb_fail("seq0 blocking submit failed");
    }
    waker_key key = blocking_key(task->id);
    for (uint32_t i = 0; i < 4000; i++) {
        if (rt_sync_point_reached_count(RT_SYNC_POINT_SP_BLOCKING_POP_BEFORE_STATUS) > before &&
            task_status_load(task) == TASK_WAITING && seq0_entries(ex, key, task->id) == 1) {
            break;
        }
        if (i == 3999) {
            rt_sync_point_open();
            return finish_mode(ex, -1);
        }
        rtb_sleep_us(1000);
    }
    add_waiter(ex, key, task->id);
    if (seq0_entries(ex, key, task->id) != 2) {
        rt_sync_point_open();
        return finish_mode(ex, -1);
    }
    rt_blocking_request_cancel(ex, task);
    int observed = observe_terminal_drain(ex, key, task, "blocking-cancel");
    rt_sync_point_open();
#ifdef RV2_SEQ0_TERMINAL_RETIRE_NEGATIVE_CONTROL
    rtb_wake(ex, task->id);
#endif
    (void)rtb_wait_task_done(ex, task->id, 4000);
    return finish_mode(ex, observed);
}

static rt_task* new_live_waiter(void) {
    static rtb_share_state spinner;
    memset(&spinner, 0, sizeof(spinner));
    return (rt_task*)__task_create(POLL_RTB_SPINNER, &spinner, rt_channel_opaque_word_ops());
}

static rt_remote_spawn_pending* new_spawn_pending(rt_executor* ex, rt_far_task_handle* output) {
    rt_remote_spawn_pending* pending =
        (rt_remote_spawn_pending*)rt_alloc(sizeof(*pending), _Alignof(rt_remote_spawn_pending));
    if (pending == NULL) {
        return NULL;
    }
    memset(pending, 0, sizeof(*pending));
    pending->executor = ex;
    pending->source_shard_id = 0;
    pending->status = RT_REMOTE_SPAWN_STATUS_PENDING;
    pending->out_handle = output;
    atomic_store_explicit(&pending->refs, 1, memory_order_relaxed);
    remote_spawn_pending_link(pending);
    return pending;
}

int rtb_mode_seq0_spawn_abandon_drain(void) {
    rt_executor* ex = ensure_exec();
    const rt_task* task = new_live_waiter();
    rt_far_task_handle output = {0};
    rt_remote_spawn_pending* pending = new_spawn_pending(ex, &output);
    if (task == NULL || pending == NULL) {
        return finish_mode(ex, -1);
    }
    waker_key key = remote_spawn_reply_key(pending->request_id, pending->source_shard_id);
    if (!add_retry_pair(ex, key, task) || !rt_remote_spawn_abandon_handle(&output)) {
        remote_spawn_pending_finish(ex, pending, RT_REMOTE_SPAWN_STATUS_REFUSED, NULL);
        return finish_mode(ex, -1);
    }
    int observed = observe_terminal_drain(ex, key, task, "spawn-abandon");
    remote_spawn_pending_finish(ex, pending, RT_REMOTE_SPAWN_STATUS_REFUSED, NULL);

    // Prove the opposite serialized order too: finish owns the drain when its
    // PENDING->terminal transition precedes abandon, and abandon must only
    // unlink that already-terminal pending.
    rt_far_task_handle second_output = {0};
    rt_remote_spawn_pending* second = new_spawn_pending(ex, &second_output);
    if (second == NULL) {
        return finish_mode(ex, -1);
    }
    waker_key second_key = remote_spawn_reply_key(second->request_id, second->source_shard_id);
    if (!add_retry_pair(ex, second_key, task)) {
        remote_spawn_pending_finish(ex, second, RT_REMOTE_SPAWN_STATUS_REFUSED, NULL);
        return finish_mode(ex, -1);
    }
    remote_spawn_pending_finish(ex, second, RT_REMOTE_SPAWN_STATUS_REFUSED, NULL);
    if (seq0_entries(ex, second_key, task->id) != 0 ||
        !rt_remote_spawn_abandon_handle(&second_output)) {
        return finish_mode(ex, -1);
    }
    fputs("seq0 terminal order: row=spawn-finish-before-abandon entries=0\n", stderr);
    return finish_mode(ex, observed);
}

static int remote_teardown_row(rt_executor* ex, const rt_task* task, rt_remote_task_op op) {
    rt_far_task_handle handle = {
        .task_id = 700 + (uint64_t)op,
        .generation = 1,
        .owner_shard_id = 1,
        .kind = RT_FAR_HANDLE_KIND_TASK,
    };
    rt_remote_task_pending* pending = rt_remote_task_pending_new(ex, &handle, 0, op, 1);
    if (pending == NULL) {
        return -1;
    }
    pending->caller_task_id = task->id;
    waker_key key = rt_remote_task_reply_key(pending->request_id, pending->source_shard_id);
    if (!add_retry_pair(ex, key, task)) {
        rt_remote_task_pending_consume(pending);
        return -1;
    }
    rt_remote_task_release_owned(ex, task);
    return observe_terminal_drain(ex,
                                  key,
                                  task,
                                  op == RT_REMOTE_TASK_OP_AWAIT ? "remote-await-teardown"
                                                                : "remote-cancel-teardown");
}

int rtb_mode_seq0_remote_teardown_drain(void) {
    rt_executor* ex = ensure_exec();
    const rt_task* task = new_live_waiter();
    if (task == NULL) {
        return finish_mode(ex, -1);
    }
    int await_observed = remote_teardown_row(ex, task, RT_REMOTE_TASK_OP_AWAIT);
    int cancel_observed = remote_teardown_row(ex, task, RT_REMOTE_TASK_OP_CANCEL);
    if (await_observed < 0 || cancel_observed < 0) {
        return finish_mode(ex, -1);
    }
    return finish_mode(ex, await_observed || cancel_observed);
}

int rtb_mode_seq0_remote_shutdown_drain(void) {
    rt_executor* ex = ensure_exec();
    const rt_task* task = new_live_waiter();
    rt_far_task_handle handle = {
        .task_id = 800,
        .generation = 1,
        .owner_shard_id = 1,
        .kind = RT_FAR_HANDLE_KIND_TASK,
    };
    rt_remote_task_pending* pending =
        rt_remote_task_pending_new(ex, &handle, 0, RT_REMOTE_TASK_OP_AWAIT, 1);
    if (task == NULL || pending == NULL) {
        return finish_mode(ex, -1);
    }
    pending->caller_task_id = task->id;
    waker_key key = rt_remote_task_reply_key(pending->request_id, pending->source_shard_id);
    if (!add_retry_pair(ex, key, task)) {
        rt_remote_task_pending_consume(pending);
        return finish_mode(ex, -1);
    }
    rt_remote_task_pending_add_ref(pending);
    rt_transport_msg queued = {
        .kind = RT_TRANSPORT_MSG_REMOTE_TASK_AWAIT_REQUEST,
        .payload = pending,
    };
    rt_remote_task_release_msg_payload(&queued);
    rt_remote_task_fail_all_pending(ex, RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN);
    int observed = observe_terminal_drain(ex, key, task, "remote-queued-shutdown");
    rt_remote_task_pending_consume(pending);
    return finish_mode(ex, observed);
}
