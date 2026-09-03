#include "rt_async_internal.h"

// Ready-queue deque storage. Callers own the locking: today every deque is
// mutated under the executor lock; the lock split moves ownership to
// the owning shard's lock without changing these helpers.

static int
deque_reserve(rt_deque* dq, size_t want, const char* overflow_msg, const char* alloc_msg) {
    if (dq == NULL) {
        return 0;
    }
    if (want <= dq->cap) {
        return 1;
    }
    size_t next_cap = dq->cap == 0 ? 16 : dq->cap;
    while (next_cap < want) {
        if (next_cap > SIZE_MAX / 2) {
            panic_msg(overflow_msg);
            return 0;
        }
        next_cap *= 2;
    }
    if (next_cap > SIZE_MAX / sizeof(uint64_t)) {
        panic_msg(overflow_msg);
        return 0;
    }
    size_t old_size = dq->cap * sizeof(uint64_t);
    size_t new_size = next_cap * sizeof(uint64_t);
    if (old_size > UINT64_MAX || new_size > UINT64_MAX) {
        panic_msg(overflow_msg);
        return 0;
    }
    uint64_t* next = (uint64_t*)rt_alloc((uint64_t)new_size, _Alignof(uint64_t));
    if (next == NULL) {
        fatal_oom_msg(alloc_msg);
        return 0;
    }
    if (dq->len > 0 && dq->task_ids != NULL) {
        memcpy(next, dq->task_ids + dq->head, dq->len * sizeof(uint64_t));
    }
    if (dq->task_ids != NULL && dq->cap > 0) {
        rt_free((uint8_t*)dq->task_ids, (uint64_t)old_size, _Alignof(uint64_t));
    }
    dq->task_ids = next;
    dq->cap = next_cap;
    dq->head = 0;
    return 1;
}

static int
deque_ensure_space(rt_deque* dq, size_t extra, const char* overflow_msg, const char* alloc_msg) {
    if (dq == NULL) {
        return 0;
    }
    if (dq->len == 0) {
        dq->head = 0;
    }
    if (dq->head > SIZE_MAX - dq->len) {
        panic_msg(overflow_msg);
        return 0;
    }
    size_t used = dq->head + dq->len;
    if (extra > SIZE_MAX - used) {
        panic_msg(overflow_msg);
        return 0;
    }
    size_t want = used + extra;
    if (want <= dq->cap) {
        return 1;
    }
    if (dq->head > 0 && dq->len > 0 && dq->task_ids != NULL) {
        memmove(dq->task_ids, dq->task_ids + dq->head, dq->len * sizeof(uint64_t));
        dq->head = 0;
        used = dq->len;
        if (extra > SIZE_MAX - used) {
            panic_msg(overflow_msg);
            return 0;
        }
        want = used + extra;
        if (want <= dq->cap) {
            return 1;
        }
    }
    return deque_reserve(dq, want, overflow_msg, alloc_msg);
}

int deque_push_tail(rt_deque* dq, uint64_t id, const char* overflow_msg, const char* alloc_msg) {
    if (dq == NULL) {
        return 0;
    }
    if (!deque_ensure_space(dq, 1, overflow_msg, alloc_msg)) {
        return 0;
    }
    dq->task_ids[dq->head + dq->len] = id;
    dq->len++;
    return 1;
}

int deque_push_head(rt_deque* dq, uint64_t id, const char* overflow_msg, const char* alloc_msg) {
    if (dq == NULL) {
        return 0;
    }
    if (dq->len == 0) {
        return deque_push_tail(dq, id, overflow_msg, alloc_msg);
    }
    if (dq->head > 0) {
        dq->head--;
        dq->task_ids[dq->head] = id;
        dq->len++;
        return 1;
    }
    if (!deque_ensure_space(dq, 1, overflow_msg, alloc_msg)) {
        return 0;
    }
    memmove(dq->task_ids + 1, dq->task_ids, dq->len * sizeof(uint64_t));
    dq->task_ids[0] = id;
    dq->len++;
    return 1;
}

int deque_pop_head(rt_deque* dq, uint64_t* out_id) {
    if (dq == NULL || dq->len == 0) {
        return 0;
    }
    uint64_t id = dq->task_ids[dq->head];
    dq->head++;
    dq->len--;
    if (dq->len == 0) {
        dq->head = 0;
    }
    if (out_id != NULL) {
        *out_id = id;
    }
    return 1;
}

int deque_pop_tail(rt_deque* dq, uint64_t* out_id) {
    if (dq == NULL || dq->len == 0) {
        return 0;
    }
    size_t idx = dq->head + dq->len - 1;
    uint64_t id = dq->task_ids[idx];
    dq->len--;
    if (dq->len == 0) {
        dq->head = 0;
    }
    if (out_id != NULL) {
        *out_id = id;
    }
    return 1;
}
