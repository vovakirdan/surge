#include "rt_async_internal.h"

// Async runtime scope management.

void* rt_scope_enter(bool failfast) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL || ex->current == 0) {
        panic_msg("rt_scope_enter without current task");
        return NULL;
    }
    uint64_t id = ex->next_scope_id++;
    ensure_scope_cap(ex, id);
    rt_scope* scope = (rt_scope*)rt_alloc(sizeof(rt_scope), _Alignof(rt_scope));
    if (scope == NULL) {
        panic_msg("async: scope allocation failed");
        return NULL;
    }
    memset(scope, 0, sizeof(rt_scope));
    scope->id = id;
    scope->owner = ex->current;
    scope->failfast = failfast ? 1 : 0;
    scope->failfast_triggered = 0;
    ex->scopes[id] = scope;
    rt_task* owner = get_task(ex, ex->current);
    if (owner != NULL) {
        owner->scope_id = id;
    }
    return (void*)(uintptr_t)id;
}

void rt_scope_register_child(void* scope_handle, void* task) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        return;
    }
    uint64_t child_id = task_id_from_handle(task);
    scope_add_child(scope, child_id);
    rt_task* child = get_task(ex, child_id);
    if (child != NULL) {
        child->parent_scope_id = scope_id;
    }
}

void rt_scope_cancel_all(void* scope_handle) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        return;
    }
    for (size_t i = 0; i < scope->children_len; i++) {
        cancel_task(ex, scope->children[i]);
    }
}

bool rt_scope_join_all(void* scope_handle, uint64_t* pending, bool* failfast) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return true;
    }
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        return true;
    }
    if (failfast != NULL) {
        *failfast = scope->failfast_triggered ? true : false;
    }
    for (size_t i = 0; i < scope->children_len; i++) {
        uint64_t child_id = scope->children[i];
        rt_task* child = get_task(ex, child_id);
        if (child == NULL || child->status == TASK_DONE) {
            continue;
        }
        if (pending != NULL) {
            *pending = child_id;
        }
        pending_key = join_key(child_id);
        return false;
    }
    return true;
}

void rt_scope_exit(void* scope_handle) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    uint64_t scope_id = (uint64_t)(uintptr_t)scope_handle;
    rt_scope* scope = get_scope(ex, scope_id);
    if (scope == NULL) {
        return;
    }
    if (scope->owner != 0) {
        rt_task* owner = get_task(ex, scope->owner);
        if (owner != NULL && owner->scope_id == scope_id) {
            owner->scope_id = 0;
        }
    }
    if (scope->children != NULL && scope->children_cap > 0) {
        rt_free((uint8_t*)scope->children, (uint64_t)(scope->children_cap * sizeof(uint64_t)), _Alignof(uint64_t));
    }
    rt_free((uint8_t*)scope, sizeof(rt_scope), _Alignof(rt_scope));
    if (scope_id < ex->scopes_cap) {
        ex->scopes[scope_id] = NULL;
    }
}
