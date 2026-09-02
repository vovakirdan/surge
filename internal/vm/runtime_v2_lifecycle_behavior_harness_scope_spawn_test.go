//go:build runtime_v2_pending

package vm_test

// lifecycleHarnessScopeSpawn is the harness helper for stands whose scope
// owner must CREATE its child rather than adopt one. It follows
// lifecycleHarnessCommon in the composed source (see
// buildRuntimeV2LifecycleHarnessWithFlags) and lives apart from it only so
// that neither file crosses the size gate.
const lifecycleHarnessScopeSpawn = `
// spawn_pinned_in_scope creates a child AS A MEMBER of the scope the calling
// poll has entered. Creation is the sole writer of a task's scope identity: a
// task spawned by the driver and handed to a scope afterwards is never that
// scope's child, and rt_scope_register_child refuses it. So a stand whose scope
// must count a child creates that child from inside the owner's poll, exactly
// as publish_created_task does for compiled code -- provenance sealed and
// membership published on the scope's lane before the task is published on
// its own, one shard lock at a time, never two.
//
// The push is FORCED onto the inject queue. A poll that pushes to its own
// worker's local deque signals nobody for a single entry, so a child pushed by
// an owner about to be held at a sync point would never be popped: the pusher
// is the held worker. The inject queue signals the shard's sleepers, which is
// what the driver's spawn used to buy and what this keeps.
//
// Only the stands whose owner must create inside its scope call this; the
// rest compile the harness under -Werror without a reference to it.
__attribute__((unused))
static rt_task* spawn_pinned_in_scope(rt_executor* ex, int64_t poll_fn_id, uint32_t wanted_shard) {
    rt_task* owner = rt_current_task();
    if (owner == NULL || !waker_valid(owner->active_scope_key)) {
        return NULL;
    }
    rt_control_lock(ex);
    rt_task* task = alloc_ready_task(ex, poll_fn_id);
    if (task == NULL) {
        rt_control_unlock(ex);
        return NULL;
    }
    rt_task_set_placement(task, pin_shard(ex, wanted_shard), TASK_PLACEMENT_CONNECTION);
    task->creation_scope_key = owner->active_scope_key;
    rt_runtime* runtime = rt_executor_runtime(ex);
    rt_shard* scope_shard = rt_runtime_shard(runtime, task->creation_scope_key.owner_shard_id);
    rt_shard* owner_shard = rt_task_owner_shard(ex, task);
    if (scope_shard != NULL && scope_shard != owner_shard) {
        rt_shard_lock(scope_shard);
        rt_scope_publish_creation_locked(ex, task);
        rt_shard_unlock(scope_shard);
    }
    rt_shard_lock(owner_shard);
    if (scope_shard == NULL || scope_shard == owner_shard) {
        rt_scope_publish_creation_locked(ex, task);
    }
    (void)ready_push_task_locked(ex, owner_shard, task, 1, 0, 1);
    rt_shard_unlock(owner_shard);
    rt_control_unlock(ex);
    return task;
}
`
