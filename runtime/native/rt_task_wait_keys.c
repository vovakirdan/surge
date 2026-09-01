#include "rt_async_internal.h"

// The task's wait-key set: task->wait_keys / wait_keys_len / wait_keys_cap.
// This is the record of which keys a task currently has a waiter-store entry
// under, and it is what makes cancellation and completion able to retract those
// entries without re-deriving them. It is distinct from the waiter store itself
// (rt_async_waiter.c owns that, plus the fd-registry bridge over it): the store
// answers "who is waiting on this key", this file answers "what is this task
// waiting on". Every mutation here goes through add_waiter / remove_waiter, so
// the store's own locking rules are unchanged by the set living apart from it.
//
// Caller lane: the same one add_waiter and remove_waiter document -- either the
// control lock or nothing, never a shard lock. The set itself is task-local and
// takes no lock of its own.
//
// ensure_waiter_cap lived above ensure_wait_keys_cap before both files were one
// and was deleted: it grew shard 0's store (rt_executor_waiter_store) and
// panicked on allocation failure, holding no lock of its own, and nothing
// called it -- every insert grows the store it is about to append to, under
// that store's lock, through rt_waiter_store_ensure_cap. It was held up by the
// static pin in the waiter boundary check, and its panic -- which no live path
// could reach -- by a row in the panic ledger; both went with it.

static void ensure_wait_keys_cap(rt_task* task, size_t want) {
    if (task == NULL) {
        return;
    }
    if (task->wait_keys_cap >= want) {
        return;
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
        fatal_oom_msg("async: wait key allocation failed");
        return;
    }
    task->wait_keys = next;
    task->wait_keys_cap = next_cap;
}

void clear_wait_keys(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL || task->wait_keys_len == 0) {
        return;
    }
    for (size_t i = 0; i < task->wait_keys_len; i++) {
        remove_waiter(ex, task->wait_keys[i], task->id);
    }
    task->wait_keys_len = 0;
}

void add_wait_key(rt_executor* ex, rt_task* task, waker_key key) {
    if (ex == NULL || task == NULL || !waker_valid(key)) {
        return;
    }
    ensure_wait_keys_cap(task, task->wait_keys_len + 1);
    if (task->wait_keys == NULL) {
        return;
    }
    task->wait_keys[task->wait_keys_len++] = key;
    add_waiter(ex, key, task->id);
}
