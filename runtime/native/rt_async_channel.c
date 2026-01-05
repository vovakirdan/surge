#include "rt_async_internal.h"

// Async runtime channel support.

typedef struct {
    uint64_t task_id;
    uint64_t value_bits;
} rt_chan_send_waiter;

struct rt_channel {
    uint64_t capacity;
    uint8_t closed;
    uint64_t* buf;
    size_t buf_len;
    size_t buf_head;

    rt_chan_send_waiter* sendq;
    size_t send_len;
    size_t send_head;
    size_t send_cap;

    uint64_t* recvq;
    size_t recv_len;
    size_t recv_head;
    size_t recv_cap;
};

static rt_channel* channel_from_handle(void* handle) {
    if (handle == NULL) {
        panic_msg("async: null channel handle");
        return NULL;
    }
    return (rt_channel*)handle;
}

static void ensure_sendq_cap(rt_channel* ch, size_t want) {
    if (ch == NULL) {
        return;
    }
    if (ch->send_cap >= want) {
        return;
    }
    size_t next_cap = ch->send_cap == 0 ? 4 : ch->send_cap;
    while (next_cap < want) {
        next_cap *= 2;
    }
    size_t old_size = ch->send_cap * sizeof(rt_chan_send_waiter);
    size_t new_size = next_cap * sizeof(rt_chan_send_waiter);
    rt_chan_send_waiter* next = (rt_chan_send_waiter*)rt_realloc(
        (uint8_t*)ch->sendq, (uint64_t)old_size, (uint64_t)new_size, _Alignof(rt_chan_send_waiter));
    if (next == NULL) {
        panic_msg("async: channel send queue allocation failed");
        return;
    }
    ch->sendq = next;
    ch->send_cap = next_cap;
}

static void ensure_recvq_cap(rt_channel* ch, size_t want) {
    if (ch == NULL) {
        return;
    }
    if (ch->recv_cap >= want) {
        return;
    }
    size_t next_cap = ch->recv_cap == 0 ? 4 : ch->recv_cap;
    while (next_cap < want) {
        next_cap *= 2;
    }
    size_t old_size = ch->recv_cap * sizeof(uint64_t);
    size_t new_size = next_cap * sizeof(uint64_t);
    uint64_t* next = (uint64_t*)rt_realloc(
        (uint8_t*)ch->recvq, (uint64_t)old_size, (uint64_t)new_size, _Alignof(uint64_t));
    if (next == NULL) {
        panic_msg("async: channel recv queue allocation failed");
        return;
    }
    ch->recvq = next;
    ch->recv_cap = next_cap;
}

static void compact_sendq(rt_channel* ch) {
    if (ch == NULL) {
        return;
    }
    if (ch->sendq == NULL) {
        ch->send_head = 0;
        return;
    }
    if (ch->send_head > 256 && ch->send_head * 2 >= ch->send_len) {
        memmove(ch->sendq, ch->sendq + ch->send_head, ch->send_len * sizeof(rt_chan_send_waiter));
        ch->send_head = 0;
    }
}

static void compact_recvq(rt_channel* ch) {
    if (ch == NULL) {
        return;
    }
    if (ch->recvq == NULL) {
        ch->recv_head = 0;
        return;
    }
    if (ch->recv_head > 256 && ch->recv_head * 2 >= ch->recv_len) {
        memmove(ch->recvq, ch->recvq + ch->recv_head, ch->recv_len * sizeof(uint64_t));
        ch->recv_head = 0;
    }
}

static void sendq_push(rt_channel* ch, uint64_t task_id, uint64_t value_bits) {
    if (ch == NULL) {
        return;
    }
    if (ch->send_head > 0 && ch->send_head + ch->send_len >= ch->send_cap) {
        memmove(ch->sendq, ch->sendq + ch->send_head, ch->send_len * sizeof(rt_chan_send_waiter));
        ch->send_head = 0;
    }
    ensure_sendq_cap(ch, ch->send_head + ch->send_len + 1);
    if (ch->sendq == NULL) {
        return;
    }
    size_t idx = ch->send_head + ch->send_len;
    ch->sendq[idx] = (rt_chan_send_waiter){task_id, value_bits};
    ch->send_len++;
}

static void recvq_push(rt_channel* ch, uint64_t task_id) {
    if (ch == NULL) {
        return;
    }
    if (ch->recv_head > 0 && ch->recv_head + ch->recv_len >= ch->recv_cap) {
        memmove(ch->recvq, ch->recvq + ch->recv_head, ch->recv_len * sizeof(uint64_t));
        ch->recv_head = 0;
    }
    ensure_recvq_cap(ch, ch->recv_head + ch->recv_len + 1);
    if (ch->recvq == NULL) {
        return;
    }
    size_t idx = ch->recv_head + ch->recv_len;
    ch->recvq[idx] = task_id;
    ch->recv_len++;
}

static int pop_recv_waiter(rt_executor* ex, rt_channel* ch, uint64_t* out_id) {
    if (ch == NULL) {
        return 0;
    }
    while (ch->recv_len > 0) {
        uint64_t task_id = ch->recvq[ch->recv_head++];
        ch->recv_len--;
        compact_recvq(ch);
        if (ex != NULL) {
            rt_task* task = get_task(ex, task_id);
            if (task == NULL || task->status == TASK_DONE) {
                continue;
            }
        }
        if (out_id != NULL) {
            *out_id = task_id;
        }
        return 1;
    }
    return 0;
}

static int pop_send_waiter(rt_executor* ex, rt_channel* ch, rt_chan_send_waiter* out_waiter) {
    if (ch == NULL) {
        return 0;
    }
    while (ch->send_len > 0) {
        rt_chan_send_waiter send_waiter = ch->sendq[ch->send_head++];
        ch->send_len--;
        compact_sendq(ch);
        if (ex != NULL) {
            rt_task* task = get_task(ex, send_waiter.task_id);
            if (task == NULL || task->status == TASK_DONE) {
                continue;
            }
        }
        if (out_waiter != NULL) {
            *out_waiter = send_waiter;
        }
        return 1;
    }
    return 0;
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
    rt_chan_send_waiter send_waiter;
    if (!pop_send_waiter(ex, ch, &send_waiter)) {
        return;
    }
    if (!buf_push(ch, send_waiter.value_bits)) {
        return;
    }
    rt_task* sender = get_task(ex, send_waiter.task_id);
    if (sender == NULL || sender->status == TASK_DONE) {
        return;
    }
    sender->resume_kind = RESUME_CHAN_SEND_ACK;
    sender->resume_bits = 0;
    wake_task(ex, send_waiter.task_id, 1);
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
    return ch;
}

bool rt_channel_send(void* channel, uint64_t value_bits) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return 1;
    }
    if (ex->current == 0) {
        panic_msg("async channel send outside task");
        return 1;
    }
    rt_task* task = get_task(ex, ex->current);
    if (task == NULL) {
        panic_msg("async: missing current task");
        return 1;
    }
    if (task->cancelled) {
        task->resume_kind = RESUME_NONE;
        task->resume_bits = 0;
        return 0;
    }
    if (task->resume_kind == RESUME_CHAN_SEND_ACK) {
        task->resume_kind = RESUME_NONE;
        task->resume_bits = 0;
        return 1;
    }
    if (task->resume_kind == RESUME_CHAN_SEND_CLOSED) {
        task->resume_kind = RESUME_NONE;
        task->resume_bits = 0;
        panic_msg("send on closed channel");
        return 1;
    }
    if (ch->closed) {
        panic_msg("send on closed channel");
        return 1;
    }
    uint64_t recv_id = 0;
    if (pop_recv_waiter(ex, ch, &recv_id)) {
        rt_task* recv_task = get_task(ex, recv_id);
        if (recv_task != NULL && recv_task->status != TASK_DONE) {
            recv_task->resume_kind = RESUME_CHAN_RECV_VALUE;
            recv_task->resume_bits = value_bits;
            wake_task(ex, recv_id, 1);
        }
        return 1;
    }
    if (ch->capacity > 0 && ch->buf_len < ch->capacity && buf_push(ch, value_bits)) {
        return 1;
    }
    sendq_push(ch, task->id, value_bits);
    pending_key = channel_send_key(ch);
    return 0;
}

uint8_t rt_channel_recv(void* channel, uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return 2;
    }
    if (ex->current == 0) {
        panic_msg("async channel recv outside task");
        return 2;
    }
    rt_task* task = get_task(ex, ex->current);
    if (task == NULL) {
        panic_msg("async: missing current task");
        return 2;
    }
    if (task->cancelled) {
        task->resume_kind = RESUME_NONE;
        task->resume_bits = 0;
        return 0;
    }
    if (task->resume_kind == RESUME_CHAN_RECV_VALUE) {
        if (out_bits != NULL) {
            *out_bits = task->resume_bits;
        }
        task->resume_kind = RESUME_NONE;
        task->resume_bits = 0;
        return 1;
    }
    if (task->resume_kind == RESUME_CHAN_RECV_CLOSED) {
        task->resume_kind = RESUME_NONE;
        task->resume_bits = 0;
        return 2;
    }
    uint64_t val = 0;
    if (buf_pop(ch, &val)) {
        if (out_bits != NULL) {
            *out_bits = val;
        }
        refill_buffer_from_sender(ex, ch);
        return 1;
    }
    rt_chan_send_waiter send_waiter;
    if (pop_send_waiter(ex, ch, &send_waiter)) {
        rt_task* sender = get_task(ex, send_waiter.task_id);
        if (sender != NULL && sender->status != TASK_DONE) {
            sender->resume_kind = RESUME_CHAN_SEND_ACK;
            sender->resume_bits = 0;
            wake_task(ex, send_waiter.task_id, 1);
        }
        if (out_bits != NULL) {
            *out_bits = send_waiter.value_bits;
        }
        return 1;
    }
    if (ch->closed) {
        return 2;
    }
    recvq_push(ch, task->id);
    pending_key = channel_recv_key(ch);
    return 0;
}

bool rt_channel_try_send(void* channel, uint64_t value_bits) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL || ch->closed) {
        return 0;
    }
    uint64_t recv_id = 0;
    if (pop_recv_waiter(ex, ch, &recv_id)) {
        rt_task* recv_task = get_task(ex, recv_id);
        if (recv_task != NULL && recv_task->status != TASK_DONE) {
            recv_task->resume_kind = RESUME_CHAN_RECV_VALUE;
            recv_task->resume_bits = value_bits;
            wake_task(ex, recv_id, 1);
        }
        return 1;
    }
    if (ch->capacity > 0 && ch->buf_len < ch->capacity) {
        if (buf_push(ch, value_bits)) {
            return 1;
        }
    }
    return 0;
}

bool rt_channel_try_recv(void* channel, uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
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
    rt_chan_send_waiter send_waiter;
    if (pop_send_waiter(ex, ch, &send_waiter)) {
        rt_task* sender = get_task(ex, send_waiter.task_id);
        if (sender != NULL && sender->status != TASK_DONE) {
            sender->resume_kind = RESUME_CHAN_SEND_ACK;
            sender->resume_bits = 0;
            wake_task(ex, send_waiter.task_id, 1);
        }
        if (out_bits != NULL) {
            *out_bits = send_waiter.value_bits;
        }
        return 1;
    }
    return 0;
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
    ch->closed = 1;
    if (ex == NULL) {
        return;
    }

    while (ch->recv_len > 0) {
        uint64_t task_id = ch->recvq[ch->recv_head++];
        ch->recv_len--;
        compact_recvq(ch);
        rt_task* task = get_task(ex, task_id);
        if (task == NULL || task->status == TASK_DONE) {
            continue;
        }
        task->resume_kind = RESUME_CHAN_RECV_CLOSED;
        task->resume_bits = 0;
        wake_task(ex, task_id, 1);
    }
    ch->recv_len = 0;
    ch->recv_head = 0;

    while (ch->send_len > 0) {
        uint64_t task_id = ch->sendq[ch->send_head++].task_id;
        ch->send_len--;
        compact_sendq(ch);
        rt_task* task = get_task(ex, task_id);
        if (task == NULL || task->status == TASK_DONE) {
            continue;
        }
        task->resume_kind = RESUME_CHAN_SEND_CLOSED;
        task->resume_bits = 0;
        wake_task(ex, task_id, 1);
    }
    ch->send_len = 0;
    ch->send_head = 0;
}
