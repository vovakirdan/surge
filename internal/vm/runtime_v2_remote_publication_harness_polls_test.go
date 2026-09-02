//go:build runtime_v2_pending

package vm_test

// The stand's poll bodies: the one __surge_poll_call every mode's tasks run
// through, and the driver's await of the publisher. Split out of the common
// prologue (runtime_v2_remote_publication_harness_common_test.go) with no
// change in the C text: the two constants are concatenated where the program
// is assembled, and the file-size gate reads each half on its own.
const remotePublicationHarnessPolls = `
void __surge_poll_call(uint64_t id) {
    if (id == POLL_REMOTE_CHILD) {
        remote_state_box* box = (remote_state_box*)__task_state();
        remote_child_state* child = box->child;
        const rt_task* task = rt_current_task();
        atomic_store_explicit(&child->owner,
                              task != NULL ? task->owner_shard_id : UINT32_MAX,
                              memory_order_release);
        atomic_store_explicit(&child->worker, rt_debug_current_worker_shard_id(), memory_order_release);
        // A counter, not a flag: the duplicate/stale rows assert that a
        // redelivered request never creates a SECOND body.
        atomic_fetch_add_explicit(&child->ran, 1, memory_order_acq_rel);
        rt_async_return(child, &(uint64_t){77});
        return;
    }
    if (id == POLL_IMMEDIATE_CALLER) {
        immediate_exec_state* st = (immediate_exec_state*)__task_state();
        rt_executor* ex = ensure_exec();
        if (st->shutdown_first && !st->shutdown_done) {
            st->shutdown_done = 1;
            (void)rt_executor_request_shutdown(ex);
        }
        if (st->fill_queue && !st->filled) {
            st->filled = 1;
            if (!fill_data_lane(ex, 0)) {
                st->status = RT_REMOTE_TASK_STATUS_REFUSED;
                rt_async_return(st, &(uint64_t){(uint64_t)st->status});
                return;
            }
        }
        rt_remote_task_status status = st->anchored
            ? rt_immediate_on_execute_anchored(&st->anchor, st->droppable ? DROP_REMOTE_STATE : 0, 0, POLL_REMOTE_CHILD, st->child, &st->pending, &st->out_kind, &st->out_bits)
            : rt_immediate_on_execute(st->placement, st->droppable ? DROP_REMOTE_STATE : 0, 0, POLL_REMOTE_CHILD, st->child, &st->pending, &st->out_kind, &st->out_bits);
        if (status == RT_REMOTE_TASK_STATUS_PENDING) {
            st->saw_pending = 1;
            atomic_store_explicit(&st->pending_shared, st->pending, memory_order_release);
            rt_async_yield(st, 0);
            return;
        }
        st->status = status;
        rt_async_return(st, &(uint64_t){(uint64_t)status});
        return;
    }
    if (id == POLL_SELECT_CALLER) {
        select_exec_state* st = (select_exec_state*)__task_state();
        if (st->fill_queue && !st->filled) {
            st->filled = 1;
            if (!fill_data_lane(ensure_exec(), st->anchors[0].owner_shard_id)) {
                st->status = RT_REMOTE_TASK_STATUS_REFUSED;
                rt_async_return(st, &(uint64_t){(uint64_t)st->status});
                return;
            }
        }
		const rt_far_task_handle* const* anchors =
			st->null_anchor_array ? NULL : st->anchor_ptrs;
		for (uint64_t i = 0; i < st->count; i++) {
			st->send_addrs[i] = st->kinds[i] == 2 ? (void*)&st->send_bits[i] : NULL;
		}
		if (st->wide_arm != 0) {
			st->send_addrs[st->wide_arm - 1] = (void*)st->wide_send;
		}
		rt_remote_task_status status = rt_far_channel_select(
			anchors, st->kinds, st->send_addrs, st->send_type_ids, st->count,
            st->droppable ? DROP_REMOTE_STATE : 0, POLL_SELECT_BODY, st->body_state,
            &st->pending, &st->out_kind, &st->out_bits);
        if (status == RT_REMOTE_TASK_STATUS_PENDING) {
            st->saw_pending = 1;
            atomic_store_explicit(&st->pending_shared, st->pending, memory_order_release);
            rt_async_yield(st, 0);
            return;
        }
        st->status = status;
        rt_async_return(st, &(uint64_t){(uint64_t)status});
        return;
    }
    if (id == POLL_SELECT_BODY) {
        // The compiled lowering calls rt_anchored_channel_select and stores
        // the winner into the block's select_index destination; the
        // harness body mirrors that exactly, since the select machinery
        // itself is production runtime code under test, not a stand-in.
        uint64_t winner = rt_anchored_channel_select();
        rt_async_return(__task_state(), &(uint64_t){winner});
        return;
    }
    if (id == POLL_REMOTE_PUBLISHER) {
        remote_publish_state* st = (remote_publish_state*)__task_state();
        rt_executor* ex = ensure_exec();
        if (st->abandon_mode && st->saw_pending) {
            // The abandon rows model a departed caller: after the driver
            // abandons the handle the pending may be freed at any moment,
            // so this task must never touch it again.
            rt_async_yield(st, 0);
            return;
        }
        if (st->shutdown_first && !st->shutdown_done) {
            st->shutdown_done = 1;
            (void)rt_executor_request_shutdown(ex);
        }
        if (st->fill_queue && !st->filled) {
            st->filled = 1;
            if (!fill_data_lane(ex, st->dst)) {
                rt_async_return(st, &(uint64_t){RT_REMOTE_SPAWN_STATUS_REFUSED});
                return;
            }
        }
        rt_remote_spawn_status status = rt_remote_spawn_publish(
            st->dst, st->droppable ? DROP_REMOTE_STATE : 0, 0, POLL_REMOTE_CHILD,
            st->child, &st->pending, &st->handle);
        if (status == RT_REMOTE_SPAWN_STATUS_PENDING) {
            st->saw_pending = 1;
            st->request_id = rt_remote_spawn_pending_request_id(st->pending);
            atomic_store_explicit(&st->pending_shared, st->pending, memory_order_release);
            rt_async_yield(st, 0);
            return;
        }
        st->status = status;
        st->children_after = rt_current_task() != NULL ? rt_current_task()->children_len : 999;
        st->validate_status = status == RT_REMOTE_SPAWN_STATUS_OK
                                  ? rt_remote_spawn_handle_validate(ex, &st->handle)
                                  : status;
        rt_async_return(st, &(uint64_t){(uint64_t)status});
        return;
    }
    rt_async_return(NULL, &(uint64_t){0});
}

static int await_parent(remote_publish_state* st) {
    void* task = __task_create(POLL_REMOTE_PUBLISHER, st, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(task, &kind, &bits);
    if (kind != 1) {
        return 0;
    }
    return bits == (uint64_t)st->status;
}
`
