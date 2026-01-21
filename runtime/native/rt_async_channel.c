#include "rt_async_internal.h"

// Async runtime channel support.

struct rt_channel {
    uint64_t capacity;
    uint8_t closed;
    uint64_t* buf;
    size_t buf_len;
    size_t buf_head;
};

static rt_channel* channel_from_handle(void* handle) {
    if (handle == NULL) {
        panic_msg("async: null channel handle");
        return NULL;
    }
    return (rt_channel*)handle;
}

static int buf_push(rt_channel* ch, uint64_t value_bits) {
    if (ch == NULL || ch->capacity == 0 || ch->buf_len >= ch->capacity || ch->buf == NULL) {
        return 0;
    }
    size_t idx = (ch->buf_head + ch->buf_len) % ch->capacity;
    ch->buf[idx] = value_bits;
    ch->buf_len++;
    return 1;
}

static int buf_pop(rt_channel* ch, uint64_t* out_bits) {
    if (ch == NULL || ch->buf_len == 0 || ch->buf == NULL) {
        return 0;
    }
    if (out_bits != NULL) {
        *out_bits = ch->buf[ch->buf_head];
    }
    ch->buf_head = (ch->buf_head + 1) % ch->capacity;
    ch->buf_len--;
    return 1;
}

static void refill_buffer_from_sender(rt_executor* ex, rt_channel* ch) {
    if (ch == NULL || ch->capacity == 0 || ch->buf_len >= ch->capacity) {
        return;
    }
    uint64_t sender_id = 0;
    if (!pop_waiter(ex, channel_send_key(ch), &sender_id)) {
        return;
    }
    rt_task* sender = get_task(ex, sender_id);
    if (sender == NULL || task_status_load(sender) == TASK_DONE) {
        return;
    }
    if (!buf_push(ch, sender->resume_bits)) {
        return;
    }
    sender->resume_kind = RESUME_CHAN_SEND_ACK;
    sender->resume_bits = 0;
    wake_task(ex, sender_id, 1);
}

void* rt_channel_new(uint64_t capacity) {
    rt_channel* ch = (rt_channel*)rt_alloc(sizeof(rt_channel), _Alignof(rt_channel));
    if (ch == NULL) {
        panic_msg("async: channel allocation failed");
        return NULL;
    }
    memset(ch, 0, sizeof(rt_channel));
    ch->capacity = capacity;
    if (capacity > 0) {
        uint64_t bytes = capacity * sizeof(uint64_t);
        ch->buf = (uint64_t*)rt_alloc(bytes, _Alignof(uint64_t));
        if (ch->buf == NULL) {
            panic_msg("async: channel buffer allocation failed");
            rt_free((uint8_t*)ch, sizeof(rt_channel), _Alignof(rt_channel));
            return NULL;
        }
    }
    rt_async_debug_printf(
        "async chan new ch=%p cap=%llu\n", (void*)ch, (unsigned long long)capacity);
    return ch;
}

bool rt_channel_send(void* channel, uint64_t value_bits) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return 1;
    }
    rt_lock(ex);
    if (rt_current_task_id() == 0) {
        rt_unlock(ex);
        panic_msg("async channel send outside task");
        return 1;
    }
    rt_task* task = rt_current_task();
    if (task == NULL) {
        rt_unlock(ex);
        panic_msg("async: missing current task");
        return 1;
    }
    if (task_cancelled_load(task) != 0) {
        task->resume_kind = RESUME_NONE;
        task->resume_bits = 0;
        rt_unlock(ex);
        return 0;
    }
    if (task->resume_kind == RESUME_CHAN_SEND_ACK) {
        task->resume_kind = RESUME_NONE;
        task->resume_bits = 0;
        rt_unlock(ex);
        return 1;
    }
    if (task->resume_kind == RESUME_CHAN_SEND_CLOSED) {
        task->resume_kind = RESUME_NONE;
        task->resume_bits = 0;
        rt_unlock(ex);
        panic_msg("send on closed channel");
        return 1;
    }
    if (ch->closed) {
        rt_unlock(ex);
        panic_msg("send on closed channel");
        return 1;
    }
    uint64_t recv_id = 0;
    waker_key recv_key = channel_recv_key(ch);
    if (pop_waiter(ex, recv_key, &recv_id)) {
        rt_task* recv_task = get_task(ex, recv_id);
        if (recv_task != NULL && task_status_load(recv_task) != TASK_DONE) {
            recv_task->resume_kind = RESUME_CHAN_RECV_VALUE;
            recv_task->resume_bits = value_bits;
            wake_task(ex, recv_id, 1);
        }
        rt_unlock(ex);
        return 1;
    }
    if (ch->capacity > 0 && ch->buf_len < ch->capacity && buf_push(ch, value_bits)) {
        rt_unlock(ex);
        return 1;
    }
    task->resume_kind = RESUME_NONE;
    task->resume_bits = value_bits;
    waker_key send_key = channel_send_key(ch);
    prepare_park(ex, task, send_key, 0);
    pending_key = send_key;
    rt_unlock(ex);
    return 0;
}

uint8_t rt_channel_recv(void* channel, uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return 2;
    }
    rt_lock(ex);
    if (rt_current_task_id() == 0) {
        rt_unlock(ex);
        panic_msg("async channel recv outside task");
        return 2;
    }
    rt_task* task = rt_current_task();
    if (task == NULL) {
        rt_unlock(ex);
        panic_msg("async: missing current task");
        return 2;
    }
    if (task_cancelled_load(task) != 0) {
        task->resume_kind = RESUME_NONE;
        task->resume_bits = 0;
        rt_unlock(ex);
        return 0;
    }
    if (task->resume_kind == RESUME_CHAN_RECV_VALUE) {
        if (out_bits != NULL) {
            *out_bits = task->resume_bits;
        }
        task->resume_kind = RESUME_NONE;
        task->resume_bits = 0;
        rt_unlock(ex);
        return 1;
    }
    if (task->resume_kind == RESUME_CHAN_RECV_CLOSED) {
        task->resume_kind = RESUME_NONE;
        task->resume_bits = 0;
        rt_unlock(ex);
        return 2;
    }
    uint64_t val = 0;
    if (buf_pop(ch, &val)) {
        if (out_bits != NULL) {
            *out_bits = val;
        }
        refill_buffer_from_sender(ex, ch);
        rt_unlock(ex);
        return 1;
    }
    uint64_t sender_id = 0;
    waker_key send_key = channel_send_key(ch);
    if (pop_waiter(ex, send_key, &sender_id)) {
        rt_task* sender = get_task(ex, sender_id);
        if (sender != NULL && task_status_load(sender) != TASK_DONE) {
            if (out_bits != NULL) {
                *out_bits = sender->resume_bits;
            }
            sender->resume_kind = RESUME_CHAN_SEND_ACK;
            sender->resume_bits = 0;
            wake_task(ex, sender_id, 1);
        }
        rt_unlock(ex);
        return 1;
    }
    if (ch->closed) {
        rt_unlock(ex);
        return 2;
    }
    task->resume_kind = RESUME_NONE;
    task->resume_bits = 0;
    waker_key recv_key = channel_recv_key(ch);
    prepare_park(ex, task, recv_key, 0);
    pending_key = recv_key;
    rt_unlock(ex);
    return 0;
}

bool rt_channel_try_send(void* channel, uint64_t value_bits) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL || ch->closed) {
        return 0;
    }
    rt_lock(ex);
    uint64_t recv_id = 0;
    if (pop_waiter(ex, channel_recv_key(ch), &recv_id)) {
        rt_task* recv_task = get_task(ex, recv_id);
        if (recv_task != NULL && task_status_load(recv_task) != TASK_DONE) {
            recv_task->resume_kind = RESUME_CHAN_RECV_VALUE;
            recv_task->resume_bits = value_bits;
            wake_task(ex, recv_id, 1);
        }
        rt_unlock(ex);
        return 1;
    }
    if (ch->capacity > 0 && ch->buf_len < ch->capacity) {
        if (buf_push(ch, value_bits)) {
            rt_unlock(ex);
            return 1;
        }
    }
    rt_unlock(ex);
    return 0;
}

bool rt_channel_try_recv(void* channel, uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return 0;
    }
    rt_lock(ex);
    uint64_t val = 0;
    if (buf_pop(ch, &val)) {
        if (out_bits != NULL) {
            *out_bits = val;
        }
        refill_buffer_from_sender(ex, ch);
        rt_unlock(ex);
        return 1;
    }
    uint64_t sender_id = 0;
    if (pop_waiter(ex, channel_send_key(ch), &sender_id)) {
        rt_task* sender = get_task(ex, sender_id);
        if (sender != NULL && task_status_load(sender) != TASK_DONE) {
            if (out_bits != NULL) {
                *out_bits = sender->resume_bits;
            }
            sender->resume_kind = RESUME_CHAN_SEND_ACK;
            sender->resume_bits = 0;
            wake_task(ex, sender_id, 1);
        }
        rt_unlock(ex);
        return 1;
    }
    rt_unlock(ex);
    return 0;
}

// rt_channel_try_recv_status_locked is the locked, non-blocking recv with closed status.
// Returns 0 = not ready, 1 = received value, 2 = channel closed.
uint8_t rt_channel_try_recv_status_locked(rt_executor* ex, void* channel, uint64_t* out_bits) {
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return 0;
    }
    uint64_t val = 0;
    if (buf_pop(ch, &val)) {
        if (out_bits != NULL) {
            *out_bits = val;
        }
        refill_buffer_from_sender(ex, ch);
        return 1;
    }
    uint64_t sender_id = 0;
    if (pop_waiter(ex, channel_send_key(ch), &sender_id)) {
        rt_task* sender = get_task(ex, sender_id);
        if (sender != NULL && task_status_load(sender) != TASK_DONE) {
            if (out_bits != NULL) {
                *out_bits = sender->resume_bits;
            }
            sender->resume_kind = RESUME_CHAN_SEND_ACK;
            sender->resume_bits = 0;
            wake_task(ex, sender_id, 1);
        }
        return 1;
    }
    if (ch->closed) {
        return 2;
    }
    return 0;
}

// rt_channel_try_send_status_locked is the locked, non-blocking send with closed status.
// Returns 0 = not ready, 1 = sent, 2 = channel closed.
uint8_t rt_channel_try_send_status_locked(rt_executor* ex, void* channel, uint64_t value_bits) {
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return 0;
    }
    if (ch->closed) {
        return 2;
    }
    uint64_t recv_id = 0;
    if (pop_waiter(ex, channel_recv_key(ch), &recv_id)) {
        rt_task* recv_task = get_task(ex, recv_id);
        if (recv_task != NULL && task_status_load(recv_task) != TASK_DONE) {
            recv_task->resume_kind = RESUME_CHAN_RECV_VALUE;
            recv_task->resume_bits = value_bits;
            wake_task(ex, recv_id, 1);
        }
        return 1;
    }
    if (ch->capacity > 0 && ch->buf_len < ch->capacity && buf_push(ch, value_bits)) {
        return 1;
    }
    return 0;
}

static void channel_blocking_yield(void) {
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
    for (;;) {
        rt_lock(ex);
        uint8_t status = rt_channel_try_send_status_locked(ex, channel, value_bits);
        rt_unlock(ex);
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
    for (;;) {
        rt_lock(ex);
        uint8_t status = rt_channel_try_recv_status_locked(ex, channel, out_bits);
        rt_unlock(ex);
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
    if (ch == NULL) {
        return;
    }
    if (ch->closed) {
        return;
    }
    rt_async_debug_printf("async chan close ch=%p\n", (void*)ch);
    rt_lock(ex);
    ch->closed = 1;
    if (ex == NULL) {
        rt_unlock(ex);
        return;
    }

    uint64_t task_id = 0;
    waker_key recv_key = channel_recv_key(ch);
    while (pop_waiter(ex, recv_key, &task_id)) {
        rt_task* task = get_task(ex, task_id);
        if (task == NULL || task_status_load(task) == TASK_DONE) {
            continue;
        }
        task->resume_kind = RESUME_CHAN_RECV_CLOSED;
        task->resume_bits = 0;
        wake_task(ex, task_id, 1);
    }

    waker_key send_key = channel_send_key(ch);
    while (pop_waiter(ex, send_key, &task_id)) {
        rt_task* task = get_task(ex, task_id);
        if (task == NULL || task_status_load(task) == TASK_DONE) {
            continue;
        }
        task->resume_kind = RESUME_CHAN_SEND_CLOSED;
        task->resume_bits = 0;
        wake_task(ex, task_id, 1);
    }
    rt_unlock(ex);
}
