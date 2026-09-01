#include "rt_channel_lane.h"
#include "rt_remote_task.h"

#include <stddef.h>

// Select and timeout arms. Registration is control-serialized (spike D12):
// these paths register one task under several key owners, so they stay on
// the slow lane while single-key awaits run shard-local.

// Arm-kind ABI values live in rt_async_internal.h (rt_select_arm_kind): the
// compiler emits them into rt_select_poll calls, and the remote proxy
// selector ships them across the transport.

static void ensure_select_timers_cap(rt_task* task, size_t want) {
    if (task == NULL) {
        return;
    }
    if (task->select_timers_cap >= want) {
        return;
    }
    size_t next_cap = task->select_timers_cap == 0 ? 4 : task->select_timers_cap;
    while (next_cap < want) {
        next_cap *= 2;
    }
    size_t old_size = task->select_timers_cap * sizeof(uint64_t);
    size_t new_size = next_cap * sizeof(uint64_t);
    uint64_t* next = (uint64_t*)rt_realloc(
        (uint8_t*)task->select_timers, (uint64_t)old_size, (uint64_t)new_size, _Alignof(uint64_t));
    if (next == NULL) {
        fatal_oom_msg("async: select timer allocation failed");
        return;
    }
    task->select_timers = next;
    task->select_timers_cap = next_cap;
}

uint8_t rt_timeout_poll(void* task, uint64_t ms, void* out_dst) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return 2;
    }
    rt_control_lock(ex);
    if (rt_current_task_id() == 0) {
        rt_control_unlock(ex);
        panic_msg("async timeout outside task");
        return 2;
    }
    rt_task* current = rt_current_task();
    if (current == NULL) {
        rt_control_unlock(ex);
        panic_msg("async: missing current task");
        return 2;
    }
    clear_wait_keys(ex, current);
    if (current_task_cancelled(ex)) {
        pending_key = waker_none();
        rt_control_unlock(ex);
        return 0;
    }

    rt_task* target = task_from_handle(task);
    if (target == NULL) {
        rt_control_unlock(ex);
        return 2;
    }

    uint64_t timeout_id = current->timeout_task_id;
    rt_task* timeout_task = NULL;
    if (timeout_id != 0) {
        timeout_task = get_task(ex, timeout_id);
        if (timeout_task == NULL) {
            timeout_id = 0;
            current->timeout_task_id = 0;
        }
    }
    if (timeout_id == 0) {
        timeout_task = rt_spawn_sleep_task_locked(ex, ms);
        if (timeout_task == NULL) {
            rt_control_unlock(ex);
            return 2;
        }
        current->timeout_task_id = timeout_task->id;
    }

    int move_pending = 0;
    if (task_status_load(target) == TASK_DONE) {
        rt_control_unlock(ex);
        // The take MOVES or CLONES the value, and both run generated code that
        // may not run under a runtime lock. The target and the timer are still
        // alive across the gap: this caller holds a handle reference to each,
        // released below.
        uint8_t kind = rt_far_task_take_result(target, current, out_dst);
        rt_control_lock(ex);
        if (kind != 0) {
            current->timeout_task_id = 0;
            pending_key = waker_none();
            if (timeout_task != NULL) {
                task_release(ex, timeout_task);
            }
            task_release(ex, target);
            rt_control_unlock(ex);
            return kind;
        }
        // The last asker with a reader still out: park on the target's join
        // key as if it were not done yet; the reader that retires last wakes
        // this asker, and the timer keeps its say.
        move_pending = 1;
    }
    if (timeout_task != NULL && task_status_load(timeout_task) == TASK_DONE) {
        if (move_pending) {
            // The timer won the race the reader was holding up: this asker
            // never takes, so its entitlement retires as dropped.
            rt_task_entitlement_drop(ex, target);
        }
        cancel_task(ex, target->id);
        current->timeout_task_id = 0;
        task_release(ex, timeout_task);
        task_release(ex, target);
        pending_key = waker_none();
        rt_control_unlock(ex);
        return 2;
    }

    if (task_status_load(target) != TASK_WAITING) {
        wake_task(ex, target->id, 1);
    }
    if (timeout_task != NULL && task_status_load(timeout_task) != TASK_WAITING &&
        task_status_load(timeout_task) != TASK_DONE) {
        wake_task(ex, timeout_task->id, 1);
    }

    waker_key first_key = join_key(target->id);
    int first_added = 0;
    {
        size_t prev_len = current->wait_keys_len;
        add_wait_key(ex, current, first_key);
        first_added = current->wait_keys_len > prev_len;
    }
    if (timeout_task != NULL) {
        add_wait_key(ex, current, join_key(timeout_task->id));
    }
    prepare_park(ex, current, first_key, first_added);
    pending_key = first_key;
    // Register-then-verify: an arm may complete on its own shard between the
    // scans above and the registrations; self-token so the park aborts and
    // the next poll observes the completion (clear_wait_keys cleans up).
    if (task_status_load(target) == TASK_DONE ||
        (timeout_task != NULL && task_status_load(timeout_task) == TASK_DONE)) {
        (void)task_wake_token_exchange(current, 1);
    }
    rt_control_unlock(ex);
    return 0;
}

int64_t rt_select_poll(uint64_t count,
                       const uint8_t* kinds,
                       void** handles,
                       void* const* values,
                       const uint64_t* ms,
                       int64_t default_index) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return default_index >= 0 ? default_index : -1;
    }
    rt_control_lock(ex);
    if (rt_current_task_id() == 0) {
        rt_control_unlock(ex);
        panic_msg("async select outside task");
        return -1;
    }
    rt_task* current = rt_current_task();
    if (current == NULL) {
        rt_control_unlock(ex);
        panic_msg("async: missing current task");
        return -1;
    }
    clear_wait_keys(ex, current);
    if (current_task_cancelled(ex)) {
        clear_select_timers(ex, current);
        pending_key = waker_none();
        rt_control_unlock(ex);
        return -1;
    }

    int has_timeout = 0;
    for (uint64_t i = 0; i < count; i++) {
        if (kinds != NULL && kinds[i] == SELECT_TIMEOUT) {
            has_timeout = 1;
            break;
        }
    }
    if (!has_timeout && current->select_timers_len > 0) {
        clear_select_timers(ex, current);
    }
    if (has_timeout && current->select_timers_len != count) {
        clear_select_timers(ex, current);
        if (count > 0) {
            ensure_select_timers_cap(current, (size_t)count);
        }
        if (current->select_timers_cap < count) {
            pending_key = waker_none();
            rt_control_unlock(ex);
            return -1;
        }
        current->select_timers_len = (size_t)count;
        if (current->select_timers != NULL) {
            for (uint64_t i = 0; i < count; i++) {
                current->select_timers[i] = 0;
            }
        }
    }

    int64_t selected = -1;
    int selected_timeout = 0;
    uint64_t selected_task_id = 0;
    // A won recv arm's value, held until the control lock is gone (see the
    // SELECT_CHAN_RECV scan below). Only ever set on the iteration that also
    // sets `selected`, and the scan stops there, so at most one arm's value is
    // ever in hand.
    //
    // The operation stages it in storage of its OWN, sized and aligned for the
    // element: an arm binds nothing, so the value has no destination, and the
    // storage model puts the staging on the operation rather than letting a
    // payload ride a scheduler word. The buffer is deliberately a local: the
    // scan takes at most one value, and it is destroyed before this frame
    // returns.
    void* taken_channel = NULL;
    _Alignas(max_align_t) unsigned char taken_storage[RT_SELECT_STAGING_BYTES];

    for (uint64_t i = 0; i < count; i++) {
        uint8_t kind = kinds != NULL ? kinds[i] : SELECT_TASK;
        void* handle = handles != NULL ? handles[i] : NULL;
        switch (kind) {
            case SELECT_DEFAULT:
                break;
            case SELECT_TASK: {
                const rt_task* target = task_from_handle(handle);
                if (target == NULL) {
                    rt_control_unlock(ex);
                    return -1;
                }
                if (task_status_load(target) != TASK_WAITING &&
                    task_status_load(target) != TASK_DONE) {
                    wake_task(ex, target->id, 1);
                }
                if (task_status_load(target) == TASK_DONE) {
                    selected = (int64_t)i;
                }
                break;
            }
            case SELECT_CHAN_RECV: {
                // Winning this arm consumes one value — that is the contract
                // the arm dispatch is written against, and what makes a losing
                // arm's value stay put. The value itself has no destination:
                // a recv arm binds nothing. Take it into a real sink anyway,
                // because the alternative is the core destroying it where
                // nobody can see, and destroy it below once the control lock
                // is released — compiled drop glue must not run under a
                // runtime lock.
                const rt_value_ops* arm_ops = rt_channel_element_ops(handle);
                if (arm_ops != NULL && arm_ops->layout.size > sizeof(taken_storage)) {
                    rt_control_unlock(ex);
                    panic_msg("select: channel element is wider than the operation's staging");
                    return -1;
                }
                // Claim under control, move with control RELEASED, finish with
                // it held again. The claim is what makes the release safe: the
                // cell or slot the take named cannot be reused while the
                // reservation stands.
                //
                // The PIN is what makes the channel itself safe across the same
                // release. This bracket is one of section 7's claimed detached
                // operations, drawn one level further out than the others, and
                // the hold has to span it: between the unlock and the relock
                // another lane may retire the last handle, and without a pin the
                // reclaim would free the ring the move is reading from. A
                // winning arm carries the pin further still -- past the commit,
                // to the release of the payload below, which reads the
                // channel's descriptor with no lock held at all.
                rt_channel_pin(handle);
                rt_channel_take take;
                uint8_t status = rt_channel_claim_recv_locked(ex, handle, &take);
                if (status == 1) {
                    rt_control_unlock(ex);
                    rt_value_move_init_detached(arm_ops, taken_storage, take.address);
                    rt_control_lock(ex);
                    rt_channel_finish_recv_locked(ex, handle, &take);
                    taken_channel = handle;
                } else {
                    rt_channel_unpin(handle);
                }
                if (status == 1 || status == 2) {
                    selected = (int64_t)i;
                }
                break;
            }
            case SELECT_CHAN_SEND: {
                void* value_src = values != NULL ? values[i] : NULL;
                rt_channel_put put;
                rt_channel_pin(handle);
                uint8_t status = rt_channel_claim_send_locked(ex, handle, &put);
                if (status == 1) {
                    rt_control_unlock(ex);
                    rt_value_move_init_detached(
                        rt_channel_element_ops(handle), put.address, value_src);
                    rt_control_lock(ex);
                    rt_channel_finish_send_locked(ex, handle, &put);
                    selected = (int64_t)i;
                } else if (status == 2) {
                    rt_control_unlock(ex);
                    panic_msg("send on closed channel");
                    return -1;
                }
                // The send arm's value is the caller's and stays the caller's,
                // so nothing outlives the commit here and the hold ends with it.
                rt_channel_unpin(handle);
                break;
            }
            case SELECT_TIMEOUT: {
                const rt_task* target = task_from_handle(handle);
                if (target == NULL) {
                    rt_control_unlock(ex);
                    return -1;
                }
                if (task_status_load(target) != TASK_WAITING &&
                    task_status_load(target) != TASK_DONE) {
                    wake_task(ex, target->id, 1);
                }
                if (task_status_load(target) == TASK_DONE) {
                    selected = (int64_t)i;
                    break;
                }
                if (current->select_timers_len == count && current->select_timers != NULL) {
                    uint64_t timer_id = current->select_timers[i];
                    if (timer_id != 0) {
                        const rt_task* timer_task = get_task(ex, timer_id);
                        if (timer_task != NULL && task_status_load(timer_task) == TASK_DONE) {
                            selected = (int64_t)i;
                            selected_timeout = 1;
                            selected_task_id = target->id;
                        }
                    }
                }
                break;
            }
            default:
                break;
        }
        if (selected >= 0) {
            break;
        }
    }

    if (selected < 0 && default_index >= 0) {
        selected = default_index;
    }

    if (selected >= 0) {
        if (selected_timeout) {
            cancel_task(ex, selected_task_id);
            wake_task(ex, selected_task_id, 1);
        }
        clear_select_timers(ex, current);
        pending_key = waker_none();
        rt_control_unlock(ex);
        if (taken_channel != NULL) {
            // Still on the pin the winning recv arm took above: the destination
            // for this value is nowhere, and destroying it reads the channel's
            // descriptor. The unpin is last, so a reclaim it sets off runs on a
            // lane holding nothing.
            rt_channel_release_payload(taken_channel, taken_storage);
            rt_channel_unpin(taken_channel);
        }
        return selected;
    }

    waker_key first_key = waker_none();
    int first_added = 0;
    for (uint64_t i = 0; i < count; i++) {
        uint8_t kind = kinds != NULL ? kinds[i] : SELECT_TASK;
        void* handle = handles != NULL ? handles[i] : NULL;
        switch (kind) {
            case SELECT_TASK: {
                const rt_task* target = task_from_handle(handle);
                if (target == NULL) {
                    rt_control_unlock(ex);
                    return -1;
                }
                waker_key key = join_key(target->id);
                if (!waker_valid(first_key)) {
                    size_t prev_len = current->wait_keys_len;
                    add_wait_key(ex, current, key);
                    first_key = key;
                    first_added = current->wait_keys_len > prev_len;
                } else {
                    add_wait_key(ex, current, key);
                }
                break;
            }
            case SELECT_CHAN_RECV: {
                waker_key key = channel_recv_key((rt_channel*)handle);
                if (!waker_valid(first_key)) {
                    size_t prev_len = current->wait_keys_len;
                    add_wait_key(ex, current, key);
                    first_key = key;
                    first_added = current->wait_keys_len > prev_len;
                } else {
                    add_wait_key(ex, current, key);
                }
                break;
            }
            case SELECT_CHAN_SEND: {
                waker_key key = channel_send_key((rt_channel*)handle);
                if (!waker_valid(first_key)) {
                    size_t prev_len = current->wait_keys_len;
                    add_wait_key(ex, current, key);
                    first_key = key;
                    first_added = current->wait_keys_len > prev_len;
                } else {
                    add_wait_key(ex, current, key);
                }
                break;
            }
            case SELECT_TIMEOUT: {
                const rt_task* target = task_from_handle(handle);
                if (target == NULL) {
                    rt_control_unlock(ex);
                    return -1;
                }
                waker_key key = join_key(target->id);
                if (!waker_valid(first_key)) {
                    size_t prev_len = current->wait_keys_len;
                    add_wait_key(ex, current, key);
                    first_key = key;
                    first_added = current->wait_keys_len > prev_len;
                } else {
                    add_wait_key(ex, current, key);
                }

                uint64_t timer_id = 0;
                if (current->select_timers_len == count && current->select_timers != NULL) {
                    timer_id = current->select_timers[i];
                }
                if (timer_id == 0) {
                    uint64_t delay = ms != NULL ? ms[i] : 0;
                    const rt_task* timer_task = rt_spawn_sleep_task_locked(ex, delay);
                    if (timer_task != NULL && current->select_timers != NULL &&
                        current->select_timers_len == count) {
                        current->select_timers[i] = timer_task->id;
                        timer_id = timer_task->id;
                    }
                }
                if (timer_id != 0) {
                    const rt_task* timer_task = get_task(ex, timer_id);
                    if (timer_task != NULL) {
                        waker_key timer_key_join = join_key(timer_task->id);
                        if (!waker_valid(first_key)) {
                            size_t prev_timer_len = current->wait_keys_len;
                            add_wait_key(ex, current, timer_key_join);
                            first_key = timer_key_join;
                            first_added = current->wait_keys_len > prev_timer_len;
                        } else {
                            add_wait_key(ex, current, timer_key_join);
                        }
                    }
                }
                break;
            }
            case SELECT_DEFAULT:
            default:
                break;
        }
    }

    if (waker_valid(first_key)) {
        prepare_park(ex, current, first_key, first_added);
    }
    pending_key = first_key;
    // Register-then-verify (see rt_timeout_poll): covers task and timeout
    // arms; channel arms stay control-serialized with channel wakes until
    // the channel lane migration revisits this scan.
    for (uint64_t i = 0; i < count; i++) {
        uint8_t kind = kinds != NULL ? kinds[i] : SELECT_TASK;
        if (kind != SELECT_TASK && kind != SELECT_TIMEOUT) {
            continue;
        }
        void* handle = handles != NULL ? handles[i] : NULL;
        const rt_task* target = handle != NULL ? task_from_handle(handle) : NULL;
        int timer_done = 0;
        if (kind == SELECT_TIMEOUT && current->select_timers != NULL &&
            current->select_timers_len == count && current->select_timers[i] != 0) {
            const rt_task* timer_task = get_task(ex, current->select_timers[i]);
            timer_done = timer_task != NULL && task_status_load(timer_task) == TASK_DONE;
        }
        if ((target != NULL && task_status_load(target) == TASK_DONE) || timer_done) {
            (void)task_wake_token_exchange(current, 1);
            break;
        }
    }
    rt_control_unlock(ex);
    return -1;
}
