#include "remote_task_behavior.h"
#include "rt_task_refs.h"
#include "rt_value_ops.h"

#include <string.h>

// RV2-DEBT-053a owner-side result reclamation rows. These bypass the async
// scheduler and drive free_task directly on a body task that completed with a
// heap-carried RESULT, which is the exact leak site: a completed owner task
// freed (release-while-DONE / cancel-after-done) before any consumer took its
// reply. A real far-task heap result is unreachable from compiled Surge today
// (the non-copy reply gate is still closed), so a direct free-path drive is
// the deterministic proof of the owner-side machinery.
enum { RTB_RESULT_DROP_MARK_ID = 53 };

// The owner-side RESULT rows, split from the shipped-STATE rows they used to
// sit beside. The two families ask different questions: a state is what a
// crossing hands the runtime on the way in, a result is what a task leaves
// behind on the way out.

// The result descriptor these rows drive free_task with: one machine word
// holding a heap block, whose drop frees it and is counted.
//
// It is what result_type_id used to say. The obligation travels with the
// value's TYPE now rather than in a number beside it, so the row that proves
// "an unconsumed result is destroyed exactly once" drives the same behaviour
// through the descriptor the task was created with.
static void rtb_result_block_move(void* destination, void* source) {
    *(void**)destination = *(void**)source;
    *(void**)source = NULL;
}

static void rtb_result_block_drop(void* value) {
    void* block = *(void**)value;
    *(void**)value = NULL;
    atomic_fetch_add_explicit(&rtb_result_drop_calls, 1, memory_order_acq_rel);
    atomic_store_explicit(&rtb_result_drop_last_id, RTB_RESULT_DROP_MARK_ID, memory_order_release);
    atomic_store_explicit(&rtb_result_drop_last_value, block, memory_order_release);
    if (block != NULL) {
        rt_free((uint8_t*)block, RTB_RESULT_BLOCK_SIZE, RTB_RESULT_BLOCK_ALIGN);
    }
}

static rt_carrier_status
rtb_result_block_plan(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

static const rt_value_ops rtb_result_block_ops = {
    .layout = {.size = sizeof(void*),
               .align = _Alignof(void*),
               .stride = sizeof(void*),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = rtb_result_block_move,
    .copy_init = NULL,
    .clone_init = NULL,
    .drop_in_place = rtb_result_block_drop,
    .trace = NULL,
    .plan_cross = rtb_result_block_plan,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

// Publishes one heap block as the task's canonical result.
static int rtb_publish_result_block(rt_task* task, void* block) {
    if (rt_value_cell_bind(&task->result, &rtb_result_block_ops) != RT_SLOT_CONTROL_OK) {
        return 0;
    }
    void* destination = rt_value_cell_publish_storage(&task->result);
    if (destination == NULL) {
        return 0;
    }
    *(void**)destination = block;
    return rt_value_cell_commit(&task->result) == RT_SLOT_CONTROL_OK;
}

static void rtb_result_drop_reset(void) {
    atomic_store_explicit(&rtb_result_drop_calls, 0, memory_order_release);
    atomic_store_explicit(&rtb_result_drop_last_id, 0, memory_order_release);
    atomic_store_explicit(&rtb_result_drop_last_value, NULL, memory_order_release);
}

// Row: a DONE owner task whose heap result nobody consumed is reclaimed by
// free_task exactly once, with the registered id and the actual result_bits
// pointer.
int rtb_mode_result_owner_release(void) {
    rt_executor* ex = ensure_exec();
    rtb_result_drop_reset();
    rt_task* task = NULL;
    if (rt_remote_spawn_create_body_task(ex, POLL_RTB_DROP_BODY, NULL, 0, 0, &task) !=
            RT_REMOTE_SPAWN_STATUS_OK ||
        task == NULL) {
        return rtb_fail("result-owner-release: body task creation failed");
    }
    void* block = rt_alloc(RTB_RESULT_BLOCK_SIZE, RTB_RESULT_BLOCK_ALIGN);
    if (block == NULL) {
        return rtb_fail("result-owner-release: block alloc failed");
    }
    task->result_kind = 1;
    if (!rtb_publish_result_block(task, block)) {
        return rtb_fail("result-owner-release: result publication failed");
    }
    task_status_store(task, TASK_DONE);
    // Completing is what sets the completion flag in the task's reference word,
    // and the release below decides the free from its own decrement plus that
    // flag -- asking the status afterwards was a double free. A stand that
    // stores the status by hand performs only half of a completion, so it seals
    // the other half here; without this the release finds an uncompleted task,
    // declines the free, and the result this row is about is never dropped.
    task_mark_completed(task);
    // The create-time reference is the only one: releasing it frees the DONE
    // task through free_task, whose dispose owns the unconsumed-result drop.
    task_release_lane_aware(ex, task);
    if (atomic_load_explicit(&rtb_result_drop_calls, memory_order_acquire) != 1) {
        return rtb_fail("result-owner-release: result not dropped exactly once");
    }
    if (atomic_load_explicit(&rtb_result_drop_last_id, memory_order_acquire) !=
            RTB_RESULT_DROP_MARK_ID ||
        atomic_load_explicit(&rtb_result_drop_last_value, memory_order_acquire) != block) {
        return rtb_fail("result-owner-release: drop carried the wrong id or value");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Negative control: a Copy result keeps result_type_id 0, so inert bits are
// never handed to the result-drop dispatch.
int rtb_mode_result_copy_inert(void) {
    rt_executor* ex = ensure_exec();
    rtb_result_drop_reset();
    rt_task* task = NULL;
    if (rt_remote_spawn_create_body_task(ex, POLL_RTB_DROP_BODY, NULL, 0, 0, &task) !=
            RT_REMOTE_SPAWN_STATUS_OK ||
        task == NULL) {
        return rtb_fail("result-copy-inert: body task creation failed");
    }
    task->result_kind = 1;
    // Inert Copy bits (fixnum-shaped), not a heap pointer: the opaque-word
    // descriptor carries them and owns nothing, which is what "no obligation"
    // is now spelled as.
    (void)rt_value_cell_bind(&task->result, rt_channel_opaque_word_ops());
    void* inert = rt_value_cell_publish_storage(&task->result);
    if (inert == NULL) {
        return rtb_fail("result-copy-inert: result publication failed");
    }
    *(uint64_t*)inert = 42;
    (void)rt_value_cell_commit(&task->result);
    task_status_store(task, TASK_DONE);
    // Completing is what sets the completion flag in the task's reference word,
    // and the release below decides the free from its own decrement plus that
    // flag -- asking the status afterwards was a double free. A stand that
    // stores the status by hand performs only half of a completion, so it seals
    // the other half here; without this the release finds an uncompleted task,
    // declines the free, and the result this row is about is never dropped.
    task_mark_completed(task);
    task_release_lane_aware(ex, task);
    if (atomic_load_explicit(&rtb_result_drop_calls, memory_order_acquire) != 0) {
        return rtb_fail("result-copy-inert: inert Copy result reached the drop dispatch");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Negative control: a consumed result cleared the obligation when ownership
// transferred to the caller; free_task must NOT drop again (no double-free).
int rtb_mode_result_consumed_no_double_drop(void) {
    rt_executor* ex = ensure_exec();
    rtb_result_drop_reset();
    rt_task* task = NULL;
    if (rt_remote_spawn_create_body_task(ex, POLL_RTB_DROP_BODY, NULL, 0, 0, &task) !=
            RT_REMOTE_SPAWN_STATUS_OK ||
        task == NULL) {
        return rtb_fail("result-consumed: body task creation failed");
    }
    void* block = rt_alloc(RTB_RESULT_BLOCK_SIZE, RTB_RESULT_BLOCK_ALIGN);
    if (block == NULL) {
        return rtb_fail("result-consumed: block alloc failed");
    }
    task->result_kind = 1;
    if (!rtb_publish_result_block(task, block)) {
        return rtb_fail("result-consumed: result publication failed");
    }
    task_status_store(task, TASK_DONE);
    // Completing is what sets the completion flag in the task's reference word,
    // and the release below decides the free from its own decrement plus that
    // flag -- asking the status afterwards was a double free. A stand that
    // stores the status by hand performs only half of a completion, so it seals
    // the other half here; without this the release finds an uncompleted task,
    // declines the free, and the result this row is about is never dropped.
    task_mark_completed(task);
    // Simulate the compiled consume path: the value MOVES to the caller, which
    // leaves the slot with nothing to destroy, and the caller frees it.
    void* taken = NULL;
    rtb_result_block_move((void*)&taken, rt_value_cell_value(&task->result));
    (void)rt_value_cell_commit_move(&task->result);
    rt_free((uint8_t*)taken, RTB_RESULT_BLOCK_SIZE, RTB_RESULT_BLOCK_ALIGN);
    task_release_lane_aware(ex, task);
    if (atomic_load_explicit(&rtb_result_drop_calls, memory_order_acquire) != 0) {
        return rtb_fail("result-consumed: free_task double-dropped a consumed result");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
