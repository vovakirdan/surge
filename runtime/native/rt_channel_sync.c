#include "rt_channel_lane.h"

// Channel sync lanes (peel B2): try/compat wrappers, the blocking helper
// loops, and close. Same owner-lock protocol as the async fast lanes; the
// control-era wrappers keep the select slow lane's lock order (control ->
// channel shard) until select migrates.

bool rt_channel_try_send(void* channel, uint64_t value_bits) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL || ch->closed) {
        return 0;
    }
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    rt_shard_lock(ch_shard);
    uint8_t status = rt_channel_try_send_status_owner_locked(ex, ch_shard, ch, value_bits);
    rt_shard_unlock(ch_shard);
    return status == 1;
}

bool rt_channel_try_recv(void* channel, uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return 0;
    }
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    rt_shard_lock(ch_shard);
    uint8_t status = rt_channel_try_recv_status_owner_locked(ex, ch_shard, ch, out_bits);
    rt_shard_unlock(ch_shard);
    return status == 1;
}

// Owner-locked non-blocking recv core: 0 = not ready, 1 = value, 2 = closed.
// Caller holds the channel owner's shard lock. Foreign parked senders are
// woken to retry rather than consumed (their value stays under their own
// lock), so a foreign-only sender queue reports "not ready".
//
// `out_bits` is where the value goes and is required. This core is the only
// place that can take a buffered entry or ack a parked sender, and both are
// irreversible: a caller with no sink was not asking a question, it was
// destroying the answer. Callers that do not want the value must still take it
// and then release it (rt_channel_release_payload), which is a decision the
// caller can see and this core cannot make — it does not know what the bits
// own, and it holds a lock under which compiled drop glue must not run.
uint8_t rt_channel_try_recv_status_owner_locked(rt_executor* ex,
                                                rt_shard* ch_shard,
                                                rt_channel* ch,
                                                uint64_t* out_bits) {
    if (out_bits == NULL) {
        panic_msg("async: channel try-recv with nowhere to put the value");
        return 0;
    }
    uint64_t val = 0;
    if (buf_pop(ch, &val)) {
        waiter cand;
        while (ch->buf_len < ch->capacity &&
               channel_pop_candidate_locked(ch_shard, channel_send_key(ch), &cand)) {
            if (cand.seq == 0) {
                channel_wake_only(ex, ch_shard, &cand);
                continue;
            }
            if (cand.owner_hint == ch->owner_shard_id) {
                rt_task* sender = get_task(ex, cand.task_id);
                if (!channel_candidate_valid(sender, &cand)) {
                    continue;
                }
                if (buf_push(ch, sender->resume_bits)) {
                    sender->resume_kind = RESUME_CHAN_SEND_ACK;
                    sender->resume_bits = 0;
                    (void)wake_task_on_shard_locked(
                        ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
                }
                break;
            }
            rt_shard_unlock(ch_shard);
            (void)channel_deliver_foreign(ex, &cand, RESUME_NONE, 0);
            rt_shard_lock(ch_shard);
        }
        *out_bits = val;
        return 1;
    }
    waiter cand;
    while (channel_pop_candidate_locked(ch_shard, channel_send_key(ch), &cand)) {
        if (cand.seq == 0) {
            channel_wake_only(ex, ch_shard, &cand);
            continue;
        }
        if (cand.owner_hint == ch->owner_shard_id) {
            rt_task* sender = get_task(ex, cand.task_id);
            if (!channel_candidate_valid(sender, &cand)) {
                continue;
            }
            *out_bits = sender->resume_bits;
            sender->resume_kind = RESUME_CHAN_SEND_ACK;
            sender->resume_bits = 0;
            int pushed = wake_task_on_shard_locked(
                ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
            if (!pushed) {
                rt_shard_unlock(ch_shard);
                channel_compat_broadcast_if_needed(ex, 0);
                rt_shard_lock(ch_shard);
            }
            return 1;
        }
        rt_shard_unlock(ch_shard);
        (void)channel_deliver_foreign(ex, &cand, RESUME_NONE, 0);
        rt_shard_lock(ch_shard);
    }
    if (ch->closed) {
        return 2;
    }
    return 0;
}

// Owner-locked non-blocking send core: 0 = not ready, 1 = sent, 2 = closed.
uint8_t rt_channel_try_send_status_owner_locked(rt_executor* ex,
                                                rt_shard* ch_shard,
                                                rt_channel* ch,
                                                uint64_t value_bits) {
    if (ch->closed) {
        return 2;
    }
    waiter cand;
    while (channel_pop_candidate_locked(ch_shard, channel_recv_key(ch), &cand)) {
        if (cand.seq == 0) {
            channel_wake_only(ex, ch_shard, &cand);
            continue;
        }
        if (cand.owner_hint == ch->owner_shard_id) {
            int pushed = 0;
            if (channel_deliver_same_shard_locked(
                    ex, ch_shard, &cand, RESUME_CHAN_RECV_VALUE, value_bits, 1, &pushed)) {
                if (!pushed) {
                    rt_shard_unlock(ch_shard);
                    channel_compat_broadcast_if_needed(ex, 0);
                    rt_shard_lock(ch_shard);
                }
                return 1;
            }
            continue;
        }
        rt_shard_unlock(ch_shard);
        int live = channel_deliver_foreign(ex, &cand, RESUME_CHAN_RECV_VALUE, value_bits);
        rt_shard_lock(ch_shard);
        if (live) {
            return 1;
        }
    }
    if (ch->capacity > 0 && ch->buf_len < ch->capacity && buf_push(ch, value_bits)) {
        return 1;
    }
    return 0;
}

// Control-era compatibility wrappers for the select slow lane: callers hold
// the control lock and no shard lock; the channel lock nests here.
uint8_t rt_channel_try_recv_status_locked(rt_executor* ex, void* channel, uint64_t* out_bits) {
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return 0;
    }
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    rt_shard_lock(ch_shard);
    uint8_t status = rt_channel_try_recv_status_owner_locked(ex, ch_shard, ch, out_bits);
    rt_shard_unlock(ch_shard);
    return status;
}

uint8_t rt_channel_try_send_status_locked(rt_executor* ex, void* channel, uint64_t value_bits) {
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return 0;
    }
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    rt_shard_lock(ch_shard);
    uint8_t status = rt_channel_try_send_status_owner_locked(ex, ch_shard, ch, value_bits);
    rt_shard_unlock(ch_shard);
    return status;
}

static void channel_blocking_yield(void) {
    rt_executor* ex = ensure_exec();
    rt_task* current = rt_current_task();
    if (rt_wait_current_worker_wakeup(ex, current)) {
        return;
    }
    void* task = checkpoint();
    if (task == NULL) {
        return;
    }
    rt_task_await(task, NULL, NULL);
}

void rt_channel_send_blocking(void* channel, uint64_t value_bits) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return;
    }
    rt_async_debug_printf(
        "async chan send start ch=%p bits=%llu\n", (void*)ch, (unsigned long long)value_bits);
    if (rt_current_task() != NULL) {
        rt_trace_channel_task_blocking_send();
        while (!rt_channel_send(channel, value_bits)) {
            if (current_task_cancelled(ex)) {
                pending_key = waker_none();
                return;
            }
            channel_blocking_yield();
        }
        pending_key = waker_none();
        rt_async_debug_printf("async chan send ok ch=%p\n", (void*)ch);
        return;
    }
    for (;;) {
        rt_control_lock(ex);
        uint8_t status = rt_channel_try_send_status_locked(ex, channel, value_bits);
        rt_control_unlock(ex);
        if (status == 1) {
            rt_async_debug_printf("async chan send ok ch=%p\n", (void*)ch);
            return;
        }
        if (status == 2) {
            rt_async_debug_printf("async chan send closed ch=%p\n", (void*)ch);
            panic_msg("send on closed channel");
            return;
        }
        channel_blocking_yield();
    }
}

uint8_t rt_channel_recv_blocking(void* channel, uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return 2;
    }
    rt_async_debug_printf("async chan recv start ch=%p\n", (void*)ch);
    if (rt_current_task() != NULL) {
        rt_trace_channel_task_blocking_recv();
        for (;;) {
            uint8_t status = rt_channel_recv(channel, out_bits);
            if (status != 0) {
                pending_key = waker_none();
                if (status == 1 && out_bits != NULL) {
                    rt_async_debug_printf("async chan recv ok ch=%p bits=%llu\n",
                                          (void*)ch,
                                          (unsigned long long)*out_bits);
                } else if (status == 2) {
                    rt_async_debug_printf("async chan recv closed ch=%p\n", (void*)ch);
                }
                return status;
            }
            if (current_task_cancelled(ex)) {
                pending_key = waker_none();
                return 2;
            }
            channel_blocking_yield();
        }
    }
    for (;;) {
        rt_control_lock(ex);
        uint8_t status = rt_channel_try_recv_status_locked(ex, channel, out_bits);
        rt_control_unlock(ex);
        if (status == 1 || status == 2) {
            if (status == 1 && out_bits != NULL) {
                rt_async_debug_printf("async chan recv ok ch=%p bits=%llu\n",
                                      (void*)ch,
                                      (unsigned long long)*out_bits);
            } else if (status == 2) {
                rt_async_debug_printf("async chan recv closed ch=%p\n", (void*)ch);
            }
            return status;
        }
        channel_blocking_yield();
    }
}

void rt_channel_close(void* channel) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL || ch->closed) {
        return;
    }
    rt_async_debug_printf("async chan close ch=%p\n", (void*)ch);
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    waker_key keys[2] = {channel_recv_key(ch), channel_send_key(ch)};
    const uint8_t resumes[2] = {RESUME_CHAN_RECV_CLOSED, RESUME_CHAN_SEND_CLOSED};
    rt_shard_lock(ch_shard);
    if (ch->closed) {
        rt_shard_unlock(ch_shard);
        return;
    }
    ch->closed = 1;
    for (size_t k = 0; k < 2; k++) {
        waiter cand;
        while (channel_pop_candidate_locked(ch_shard, keys[k], &cand)) {
            if (cand.seq == 0) {
                channel_wake_only(ex, ch_shard, &cand);
                continue;
            }
            if (cand.owner_hint == ch->owner_shard_id) {
                int pushed = 0;
                int live = channel_deliver_same_shard_locked(
                    ex, ch_shard, &cand, resumes[k], 0, 1, &pushed);
                if (live && !pushed) {
                    rt_shard_unlock(ch_shard);
                    channel_compat_broadcast_if_needed(ex, 0);
                    rt_shard_lock(ch_shard);
                }
                continue;
            }
            rt_shard_unlock(ch_shard);
            (void)channel_deliver_foreign(ex, &cand, resumes[k], 0);
            rt_shard_lock(ch_shard);
        }
    }
    rt_shard_unlock(ch_shard);
}
