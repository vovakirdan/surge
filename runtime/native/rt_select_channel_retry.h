#ifndef SURGE_RUNTIME_NATIVE_RT_SELECT_CHANNEL_RETRY_H
#define SURGE_RUNTIME_NATIVE_RT_SELECT_CHANNEL_RETRY_H

#include "rt_channel_lane.h"

// Select's side of the claim-retry budget (rt_channel_retry.h, RV2-DEBT-277).
//
// Select first learns that a claim was refused under the channel owner lane,
// but the compatibility claim helper releases that lane before returning. A
// release or close may therefore cross the empty retry key before select can
// subscribe. So an exhausted select installs the exact refused arm, then
// verifies its exact predicate while holding the same owner lane: still BUSY
// means a future release owns the wake; available or closed self-tokens this
// poll so it cannot sleep.
//
// A select is one logical operation over its whole arm set, and each poll may
// be refused on a different arm. At exhaustion it therefore registers on EVERY
// arm the budget remembers (the prefix ring), not only on the arm whose
// refusal was the eighth: a release on an earlier arm is as much its wake.

static inline int select_retry_key_recorded(const rt_task* task, waker_key key) {
    for (size_t i = 0; task != NULL && i < task->wait_keys_len; i++) {
        if (task->wait_keys[i].kind == key.kind && task->wait_keys[i].id == key.id) {
            return 1;
        }
    }
    return 0;
}

static inline int select_retry_ensure_wait_key_cap(rt_task* task, size_t want) {
    if (task == NULL) {
        return 0;
    }
    if (task->wait_keys_cap >= want) {
        return 1;
    }
    size_t next_cap = task->wait_keys_cap == 0 ? 4 : task->wait_keys_cap;
    while (next_cap < want) {
        next_cap *= 2;
    }
    size_t old_size = task->wait_keys_cap * sizeof(waker_key);
    size_t new_size = next_cap * sizeof(waker_key);
    waker_key* next = (waker_key*)rt_realloc(
        (uint8_t*)task->wait_keys, (uint64_t)old_size, (uint64_t)new_size, _Alignof(waker_key));
    if (next == NULL) {
        fatal_oom_msg("async: select retry-key allocation failed");
        return 0;
    }
    task->wait_keys = next;
    task->wait_keys_cap = next_cap;
    return 1;
}

// The exact predicate the refusal was observed under. A park-take refusal has
// no channel-wide predicate to re-read (the slot belongs to one sender), so it
// is treated as released and the select re-polls. Today neither core hands a
// select that cause -- the cores answer REFUSED only for the ring's push and
// pop, and a park take is counted only by the staged send -- so this branch
// is defensive: it exists so that a cause the cores may name later can never
// put a select to sleep on a slot it cannot name.
static inline int select_retry_refusal_still_blocks(const rt_channel* ch,
                                                    rt_channel_retry_operation operation,
                                                    rt_channel_claim_refusal_cause cause) {
    if (ch == NULL || ch->closed) {
        return 0;
    }
    int exact_ring_refusal =
        (operation == RT_CHANNEL_RETRY_SEND && cause == RT_CHANNEL_CLAIM_REFUSAL_RING_PUSH) ||
        (operation == RT_CHANNEL_RETRY_RECV && cause == RT_CHANNEL_CLAIM_REFUSAL_RING_POP);
    return exact_ring_refusal && ch->ring.reserved;
}

// Answers non-zero when the verify self-tokened: the claim went away between
// the refusal and this registration, and this poll republishes.
static inline int select_retry_register_then_verify(rt_executor* ex,
                                                    rt_task* task,
                                                    const rt_channel* ch,
                                                    rt_channel_retry_operation operation,
                                                    rt_channel_claim_refusal_cause cause) {
    if (ex == NULL || task == NULL || ch == NULL) {
        return 0;
    }
    waker_key key = operation == RT_CHANNEL_RETRY_SEND ? channel_send_retry_key(ch)
                                                       : channel_recv_retry_key(ch);
    int recorded = select_retry_key_recorded(task, key);
    if (!recorded && !select_retry_ensure_wait_key_cap(task, task->wait_keys_len + 1)) {
        return 0;
    }

    rt_shard* owner = channel_owner_shard(ex, ch);
    rt_shard_lock(owner);
    if (!recorded) {
        rt_waiter_store* store = &owner->waiter_store;
        if (rt_waiter_store_ensure_cap(store) != RT_RUNTIME_STATUS_OK) {
            rt_shard_unlock(owner);
            fatal_oom_msg("async: select retry waiter allocation failed");
            return 0;
        }
        uint32_t owner_hint = task->owner_shard_valid != 0 ? task->owner_shard_id : 0;
        task->wait_keys[task->wait_keys_len++] = key;
        // seq == 0: a select subscription, drained whole by a release rather
        // than popped as the channel's oldest waiter (rt_channel_retry.c).
        store->entries[store->len++] = (waiter){key, task->id, owner_hint, 0};
        rt_channel_key_registered(key);
    }

    int still_blocked = select_retry_refusal_still_blocks(ch, operation, cause);
    int republished = 0;
#ifdef RV2_DEBT_277_SELECT_VERIFY_NEGATIVE_CONTROL
    // Rule 13: registration without the owner-lane predicate check loses a
    // release/close that crossed the empty retry key immediately beforehand.
    (void)still_blocked;
#else
    if (!still_blocked) {
        // The claim went away between the refusal and this registration (or
        // never had a predicate to re-read): this poll republishes.
        (void)task_wake_token_exchange(task, 1);
        republished = 1;
    }
#endif
    rt_shard_unlock(owner);
    return republished;
}

// Counts one refused channel arm against the select's budget. The select's
// identity is `key_id`; the arm is what the prefix remembers.
static inline int select_retry_note_refusal(rt_task* task,
                                            uint64_t key_id,
                                            const void* handle,
                                            rt_channel_retry_operation arm,
                                            rt_channel_claim_refusal_cause cause) {
    return rt_channel_retry_refused(
        task, RT_CHANNEL_RETRY_SELECT, key_id, (const rt_channel*)handle, arm, cause);
}

// Exhausted: register on every remembered refusing arm that is part of THIS
// poll's arm set. Membership is checked against the handles the caller holds
// live, so a channel remembered from an earlier poll of the same select is
// never dereferenced through the prefix alone.
static inline void select_retry_register_prefix(
    rt_executor* ex, rt_task* task, uint64_t count, const uint8_t* kinds, void** handles) {
    if (task == NULL || handles == NULL) {
        return;
    }
    const rt_channel_retry_state* st = &task->channel_retry;
#ifdef RV2_DEBT_277_PREFIX_NEGATIVE_CONTROL
    // Rule 13: only the arm whose refusal exhausted the budget is registered,
    // and a release on an earlier refusing arm never wakes the select.
    uint8_t first = st->prefix_len > 0 ? (uint8_t)(st->prefix_len - 1) : 0;
#else
    uint8_t first = 0;
#endif
    int republished = 0;
    for (uint8_t p = first; p < st->prefix_len; p++) {
        const rt_channel_retry_refusal* r = &st->prefix[p];
        uint8_t want_kind =
            r->operation == (uint8_t)RT_CHANNEL_RETRY_SEND ? SELECT_CHAN_SEND : SELECT_CHAN_RECV;
        for (uint64_t i = 0; i < count; i++) {
            uint8_t kind = kinds != NULL ? kinds[i] : SELECT_TASK;
            if (kind != want_kind || (uint64_t)(uintptr_t)handles[i] != r->channel) {
                continue;
            }
            republished |=
                select_retry_register_then_verify(ex,
                                                  task,
                                                  (const rt_channel*)handles[i],
                                                  (rt_channel_retry_operation)r->operation,
                                                  (rt_channel_claim_refusal_cause)r->cause);
            break;
        }
    }
    if (republished) {
        // Once per poll, however many arms let go: the same unit as the
        // seven republications before exhaustion.
        rt_channel_retry_republished();
    }
}

#endif // SURGE_RUNTIME_NATIVE_RT_SELECT_CHANNEL_RETRY_H
