#include "rt_async_internal.h"

// Async runtime scope management.

void* rt_scope_enter(bool failfast) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    rt_lock(ex);
    if (rt_current_task_id() == 0) {
        rt_unlock(ex);
        panic_msg("rt_scope_enter without current task");
        return NULL;
    }
    uint64_t id = ex->next_scope_id++;
    ensure_scope_cap(ex, id);
    rt_scope* scope = (rt_scope*)rt_alloc(sizeof(rt_scope), _Alignof(rt_scope));
    if (scope == NULL) {
        rt_unlock(ex);
        panic_msg("async: scope allocation failed");
        return NULL;
    }
    memset(scope, 0, sizeof(rt_scope));
    scope->id = id;
    scope->owner = rt_current_task_id();
    scope->failfast = failfast ? 1 : 0;
    scope->failfast_triggered = 0;
    ex->scopes[id] = scope;
    rt_task* owner = rt_current_task();
    if (owner != NULL) {
        owner->scope_id = id;
    }
    rt_unlock(ex);
    return (void*)(uintptr_t)id;
}

void rt_scope_register_child(void* scope_handle, void* task) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    rt_lock(ex);
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        rt_unlock(ex);
        return;
    }
    uint64_t child_id = task_id_from_handle(task);
    scope_add_child(scope, child_id);
    rt_task* child = get_task(ex, child_id);
    if (child != NULL) {
        child->parent_scope_id = scope_id;
    }
    rt_unlock(ex);
}

void rt_scope_cancel_all(void* scope_handle) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    rt_lock(ex);
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    const rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        rt_unlock(ex);
        return;
    }
    for (size_t i = 0; i < scope->children_len; i++) {
        cancel_task(ex, scope->children[i]);
    }
    rt_unlock(ex);
}

bool rt_scope_join_all(void* scope_handle, uint64_t* pending, bool* failfast) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return true;
    }
    rt_lock(ex);
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        rt_unlock(ex);
        return true;
    }
    if (failfast != NULL) {
        *failfast = scope->failfast_triggered ? true : false;
    }
    for (size_t i = 0; i < scope->children_len; i++) {
        uint64_t child_id = scope->children[i];
        const rt_task* child = get_task(ex, child_id);
        if (child == NULL || task_status_load(child) == TASK_DONE) {
            continue;
        }
        if (pending != NULL) {
            *pending = child_id;
        }
        pending_key = join_key(child_id);
        rt_unlock(ex);
        return false;
    }
    rt_unlock(ex);
    return true;
}

void rt_scope_exit(void* scope_handle) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    rt_lock(ex);
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        rt_unlock(ex);
        return;
    }
    if (scope->owner != 0) {
        rt_task* owner = get_task(ex, scope->owner);
        if (owner != NULL && owner->scope_id == scope_id) {
            owner->scope_id = 0;
        }
    }
    if (scope->children != NULL && scope->children_cap > 0) {
        rt_free((uint8_t*)scope->children,
                (uint64_t)(scope->children_cap * sizeof(uint64_t)),
                _Alignof(uint64_t));
    }
    rt_free((uint8_t*)scope, sizeof(rt_scope), _Alignof(rt_scope));
    if (scope_id < ex->scopes_cap) {
        ex->scopes[scope_id] = NULL;
    }
    rt_unlock(ex);
}
