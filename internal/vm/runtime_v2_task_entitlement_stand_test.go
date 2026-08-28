//go:build runtime_v2_pending

package vm_test

// lifecycleHarnessTaskEntitlementModes carries the stands for the task's
// canonical result and the entitlements that may ask for it. It is concatenated
// into the shared lifecycle harness translation unit by
// buildRuntimeV2LifecycleHarnessWithFlags
// (runtime_v2_lifecycle_behavior_harness_test.go), after
// lifecycleHarnessSyncPointModes (whose wait_sync_point_count these reuse) and
// lifecycleHarnessStandHelpers, and before lifecycleHarnessMain.
//
// Every other lifecycle stand gives its tasks a machine-word result, and a word
// owns nothing: a take of one is a COPY that leaves the slot alone, so those
// stands never reach the entitlement machinery at all. The value below OWNS a
// heap block and can duplicate itself, which is what puts a take on the
// clone/move/refuse path where the counts decide the value's fate.
const lifecycleHarnessTaskEntitlementModes = `
#ifdef RT_TEST_SYNC_POINTS
#include "rt_remote_task_internal.h"

// 4042 and 4043 are the scope-membership pair's. Two lanes written apart both
// reached for 4042 and the harness compiles every mode into one switch, so the
// collision is a duplicate case label rather than a wrong answer -- which is
// why it only appears under the gate that builds the whole harness.
#define POLL_ENTITLEMENT_OWNING_RESULT 4044

// A value that is whole, then dead, and never both: the marker says which, so a
// second visit is COUNTED instead of being a double free the stand cannot see,
// and a reader that was handed something already destroyed is a number rather
// than a guess about freed memory.
#define ENTITLEMENT_VALUE_LIVE UINT64_C(0x5ACEF00D)
#define ENTITLEMENT_VALUE_DEAD UINT64_C(0xD15CA5ED)

typedef struct {
    uint64_t marker;
    char* text;
} entitlement_value;

static _Atomic uint32_t g_ent_drops;
static _Atomic uint32_t g_ent_double_drops;
static _Atomic uint32_t g_ent_duplications;
static _Atomic uint32_t g_ent_reader_kind;
static _Atomic uint32_t g_ent_reader_intact;
static _Atomic uint32_t g_ent_late_taken;

static char* entitlement_text(const char* literal) {
    size_t bytes = strlen(literal) + 1;
    char* copy = (char*)malloc(bytes);
    if (copy != NULL) {
        memcpy(copy, literal, bytes);
    }
    return copy;
}

static void entitlement_move(void* dst, void* src) {
    memcpy(dst, src, sizeof(entitlement_value));
    memset(src, 0, sizeof(entitlement_value));
}

static void entitlement_drop(void* value) {
    entitlement_value* held = (entitlement_value*)value;
    if (held->marker != ENTITLEMENT_VALUE_LIVE) {
        atomic_fetch_add_explicit(&g_ent_double_drops, 1, memory_order_acq_rel);
        return;
    }
    held->marker = ENTITLEMENT_VALUE_DEAD;
    free(held->text);
    held->text = NULL;
    atomic_fetch_add_explicit(&g_ent_drops, 1, memory_order_acq_rel);
}

// The type's own duplication, the one a take decided CLONE runs with no lock
// held. It copies the block rather than the pointer, so two askers own two
// things and neither can free what the other holds.
static void entitlement_clone(void* dst, const void* src) {
    const entitlement_value* from = (const entitlement_value*)src;
    entitlement_value* to = (entitlement_value*)dst;
    to->marker = from->marker;
    to->text = from->text != NULL ? entitlement_text(from->text) : NULL;
    atomic_fetch_add_explicit(&g_ent_duplications, 1, memory_order_acq_rel);
}

static rt_carrier_status
entitlement_plan_cross(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

static const rt_value_ops entitlement_value_ops = {
    .layout = {.size = sizeof(entitlement_value),
               .align = _Alignof(entitlement_value),
               .stride = sizeof(entitlement_value),
               .flags = RT_VALUE_FLAG_DROPPABLE | RT_VALUE_FLAG_CLONABLE},
    .move_init = entitlement_move,
    .copy_init = NULL,
    .clone_init = entitlement_clone,
    .drop_in_place = entitlement_drop,
    .trace = NULL,
    .plan_cross = entitlement_plan_cross,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

static void entitlement_reset(void) {
    atomic_store_explicit(&g_ent_drops, 0, memory_order_release);
    atomic_store_explicit(&g_ent_double_drops, 0, memory_order_release);
    atomic_store_explicit(&g_ent_duplications, 0, memory_order_release);
    atomic_store_explicit(&g_ent_reader_kind, 0, memory_order_release);
    atomic_store_explicit(&g_ent_reader_intact, 0, memory_order_release);
    atomic_store_explicit(&g_ent_late_taken, 0, memory_order_release);
}

static void poll_entitlement_owning_result(void) {
    entitlement_value produced;
    produced.marker = ENTITLEMENT_VALUE_LIVE;
    produced.text = entitlement_text("canonical");
    rt_async_return(NULL, &produced);
}

// The owner of the canonical result these rows are about.
//
// alloc_ready_task binds every stand task's result to the opaque word, so the
// descriptor is replaced here -- under the same control lock, before the task
// is pushed, so nothing can look at the slot in between. The rebind is also
// what makes the slot's generation move, which is the third row's subject.
static rt_task* entitlement_spawn_owner(rt_executor* ex) {
    if (current_worker_scheduler(ex) != NULL) {
        fputs("entitlement stand: the owner must be spawned from the driver\n", stderr);
        return NULL;
    }
    rt_control_lock(ex);
    rt_task* task = alloc_ready_task(ex, POLL_ENTITLEMENT_OWNING_RESULT);
    if (task != NULL) {
        (void)rt_value_cell_bind(&task->result, &entitlement_value_ops);
        rt_task_set_placement(task, pin_shard(ex, 0), TASK_PLACEMENT_CONNECTION);
        ready_push(ex, task->id);
    }
    rt_control_unlock(ex);
    return task;
}

// An asker that is no task: rt_task_await from a plain thread, which is the
// path that names its asker by thread rather than by task. It is used here
// because a held asker must not occupy a worker -- the racing action the driver
// performs needs the executor to still be able to run.
static void* entitlement_reader_thread(void* handle) {
    uint8_t kind = 0;
    entitlement_value served;
    memset(&served, 0, sizeof(served));
    rt_task_await(handle, &kind, &served);
    atomic_store_explicit(&g_ent_reader_kind, (uint32_t)kind, memory_order_release);
    if (kind == 1) {
        atomic_store_explicit(
            &g_ent_reader_intact,
            (served.marker == ENTITLEMENT_VALUE_LIVE && served.text != NULL) ? 1u : 0u,
            memory_order_release);
        entitlement_drop(&served);
    }
    return NULL;
}

typedef struct {
    rt_task* target;
    void* sibling;
    pthread_t reader;
    int reader_running;
} entitlement_reader_hold;

// Brings the stand to the window both the shutdown row and the cancel row race
// against: the task has completed with an owning result, three entitlements
// name it, and one of them is inside a take that was decided CLONE -- counted
// into clone_readers, holding no lock, about to duplicate the canonical value
// where it lies.
static int entitlement_hold_reader(rt_executor* ex, entitlement_reader_hold* hold) {
    memset(hold, 0, sizeof(*hold));
    unsigned before = rt_sync_point_reached_count(RT_SYNC_POINT_SP_CLONE_READER_OUT_OF_LOCK);
    hold->target = entitlement_spawn_owner(ex);
    if (hold->target == NULL) {
        fputs("entitlement stand: owner allocation failed\n", stderr);
        return 0;
    }
    if (!wait_task_status(hold->target, TASK_DONE, 8000)) {
        fputs("entitlement stand: the owner never completed\n", stderr);
        return 0;
    }
    // Two more entitlements on one task. rt_task_clone answers with the same
    // pointer every time -- a handle is a refcount and a count of who may still
    // ask, never a second object.
    void* reader_handle = rt_task_clone(hold->target, NULL);
    hold->sibling = rt_task_clone(hold->target, NULL);
    if (reader_handle == NULL || hold->sibling == NULL) {
        fputs("entitlement stand: a handle clone failed\n", stderr);
        return 0;
    }
    if (pthread_create(&hold->reader, NULL, entitlement_reader_thread, reader_handle) != 0) {
        fputs("entitlement stand: the reader thread failed to start\n", stderr);
        return 0;
    }
    hold->reader_running = 1;
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_CLONE_READER_OUT_OF_LOCK, before, 4000)) {
        fputs("entitlement stand: no take was decided CLONE at the out-of-lock window\n", stderr);
        return 0;
    }
    return 1;
}

static void entitlement_release_reader(entitlement_reader_hold* hold) {
    if (hold->reader_running) {
        rt_sync_point_open();
        pthread_join(hold->reader, NULL);
        hold->reader_running = 0;
    }
}

// The remaining askers, each in turn, and what the cohort cost.
//
// Every take is asserted to answer Success with a whole value, because the
// point of the two rows that call this is that nothing which happened at the
// window turned an entitlement's answer into anything else.
static int entitlement_drain_remaining(rt_executor* ex, rt_task* target, unsigned askers) {
    for (unsigned i = 0; i < askers; i++) {
        uint8_t kind = 0;
        entitlement_value served;
        memset(&served, 0, sizeof(served));
        // The window stays armed for the whole process, so a take here that is
        // decided CLONE stops at it too -- and this is the thread that would
        // stop, so the permit has to be granted before the take rather than
        // after. A take decided MOVE leaves the permit unspent, which nothing
        // else in these stands is waiting for.
        rt_sync_point_open();
        rt_task_await(target, &kind, &served);
        if (kind != 1) {
            (void)rt_executor_request_shutdown(ex);
            fprintf(stderr, "entitlement stand: asker %u was answered kind=%u, not Success\n",
                    i, (unsigned)kind);
            return 0;
        }
        if (served.marker != ENTITLEMENT_VALUE_LIVE || served.text == NULL) {
            (void)rt_executor_request_shutdown(ex);
            fprintf(stderr, "entitlement stand: asker %u was served a value that is not whole\n", i);
            return 0;
        }
        entitlement_drop(&served);
    }
    return 1;
}

static void entitlement_report(const char* label) {
    fprintf(stderr, "%s: duplications=%u drops=%u double_drops=%u reader_kind=%u reader_intact=%u\n",
            label,
            atomic_load_explicit(&g_ent_duplications, memory_order_acquire),
            atomic_load_explicit(&g_ent_drops, memory_order_acquire),
            atomic_load_explicit(&g_ent_double_drops, memory_order_acquire),
            atomic_load_explicit(&g_ent_reader_kind, memory_order_acquire),
            atomic_load_explicit(&g_ent_reader_intact, memory_order_acquire));
}

// ---- Shutdown against a claimed clone ----
static int mode_entitlement_shutdown_vs_claimed_clone(rt_executor* ex) {
    entitlement_reset();
    entitlement_reader_hold hold;
    if (!entitlement_hold_reader(ex, &hold)) {
        entitlement_release_reader(&hold);
        (void)rt_executor_request_shutdown(ex);
        return fail("entitlement shutdown stand: the window was never built");
    }

    // The racing action, both halves of it: the executor is told to stop, and
    // then one of the entitlements that could still have asked lets go. What is
    // left naming the canonical value is the claim that is OUT and the driver's
    // own handle, so nothing here may destroy it.
    (void)rt_executor_request_shutdown(ex);
    rt_task_handle_drop(hold.sibling);
    unsigned drops_at_window = atomic_load_explicit(&g_ent_drops, memory_order_acquire);
    fprintf(stderr, "entitlement shutdown window: canonical_drops=%u\n", drops_at_window);
    if (drops_at_window != 0) {
        // The reader is left at the window on purpose. The slot it was about to
        // read no longer exists, so releasing it would replace a stated verdict
        // with a crash somewhere else.
        return fail("entitlement shutdown stand: the canonical result was destroyed while a "
                    "claimed clone reader was still out");
    }

    entitlement_release_reader(&hold);
    if (atomic_load_explicit(&g_ent_reader_kind, memory_order_acquire) != 1 ||
        atomic_load_explicit(&g_ent_reader_intact, memory_order_acquire) != 1) {
        entitlement_report("entitlement shutdown stand");
        (void)rt_executor_request_shutdown(ex);
        return fail("entitlement shutdown stand: the claimed clone reader was not served a whole "
                    "value across the shutdown");
    }
    // One entitlement is left, the driver's own, and it is the last that can
    // ask: it moves the canonical value out rather than duplicating it.
    if (!entitlement_drain_remaining(ex, hold.target, 1)) {
        return 1;
    }
    entitlement_report("entitlement shutdown stand");
    if (atomic_load_explicit(&g_ent_duplications, memory_order_acquire) != 1 ||
        atomic_load_explicit(&g_ent_drops, memory_order_acquire) != 2 ||
        atomic_load_explicit(&g_ent_double_drops, memory_order_acquire) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("entitlement shutdown stand: the cohort did not cost one duplication, one "
                    "move and two drops");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// ---- Cancel against a result the task has already committed ----
static int mode_entitlement_cancel_vs_committed_result(rt_executor* ex) {
    entitlement_reset();
    entitlement_reader_hold hold;
    if (!entitlement_hold_reader(ex, &hold)) {
        entitlement_release_reader(&hold);
        (void)rt_executor_request_shutdown(ex);
        return fail("entitlement cancel stand: the window was never built");
    }

    unsigned committed_before =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_CANCEL_AT_COMMITTED_RESULT);
    rt_task_cancel(hold.sibling);
    unsigned committed_after =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_CANCEL_AT_COMMITTED_RESULT);
    unsigned drops_at_window = atomic_load_explicit(&g_ent_drops, memory_order_acquire);
    fprintf(stderr, "entitlement cancel window: at_committed_result=%u canonical_drops=%u\n",
            committed_after - committed_before, drops_at_window);
    // Non-vacuity: the cancel has to have arrived at a task whose answer was
    // already decided AND whose result slot still held the value, or there was
    // nothing for it to revoke and the row proves nothing.
    if (committed_after == committed_before) {
        entitlement_release_reader(&hold);
        (void)rt_executor_request_shutdown(ex);
        return fail("entitlement cancel stand: the cancel never reached a committed result");
    }
    if (drops_at_window != 0) {
        return fail("entitlement cancel stand: a cancel revoked a result the task had already "
                    "committed, while a claimed clone reader was still out");
    }

    entitlement_release_reader(&hold);
    if (atomic_load_explicit(&g_ent_reader_kind, memory_order_acquire) != 1 ||
        atomic_load_explicit(&g_ent_reader_intact, memory_order_acquire) != 1) {
        entitlement_report("entitlement cancel stand");
        (void)rt_executor_request_shutdown(ex);
        return fail("entitlement cancel stand: a cancel through a sibling handle was answered to "
                    "the claimed clone reader");
    }
    // Both remaining entitlements -- the driver's own and the one the cancel was
    // issued through -- still get their independent value. A cohort of three
    // costs two duplications and one move, cancel or no cancel.
    if (!entitlement_drain_remaining(ex, hold.target, 2)) {
        return 1;
    }
    entitlement_report("entitlement cancel stand");
    if (atomic_load_explicit(&g_ent_duplications, memory_order_acquire) != 2 ||
        atomic_load_explicit(&g_ent_drops, memory_order_acquire) != 3 ||
        atomic_load_explicit(&g_ent_double_drops, memory_order_acquire) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("entitlement cancel stand: the cohort did not cost two duplications, one "
                    "move and three drops");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// ---- A capability spent after its occupant is gone ----

static rt_result_source g_ent_late_source;

// A holder that arrives late: it is handed a capability naming one occupant of
// one result slot and asks for it after the slot has moved on. It is the
// runtime's own spender, not a hand-written check, so what the row measures is
// the answer the production path gives.
static void* entitlement_late_holder_thread(void* unused) {
    (void)unused;
    rt_executor* ex = ensure_exec();
    entitlement_value served;
    memset(&served, 0, sizeof(served));
    int taken = rt_remote_task_take_result_source(ex, &g_ent_late_source, &served);
    atomic_store_explicit(&g_ent_late_taken, taken != 0 ? 1u : 0u, memory_order_release);
    if (taken != 0) {
        entitlement_drop(&served);
    }
    return NULL;
}

static int mode_entitlement_stale_result_capability(rt_executor* ex) {
    entitlement_reset();
    unsigned before = rt_sync_point_reached_count(RT_SYNC_POINT_SP_RESULT_CAPABILITY_BEFORE_MATCH);
    rt_task* target = entitlement_spawn_owner(ex);
    if (target == NULL || !wait_task_status(target, TASK_DONE, 8000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("entitlement capability stand: the owner never completed");
    }
    // The capability names THIS occupant: the task, the task's generation, the
    // slot's generation, and the shard that owns the lifecycle.
    g_ent_late_source = rt_remote_task_pin_result(target);
    if (g_ent_late_source.result_generation == 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("entitlement capability stand: the published result could not be named");
    }
    pthread_t holder;
    if (pthread_create(&holder, NULL, entitlement_late_holder_thread, NULL) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("entitlement capability stand: the late holder thread failed to start");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_RESULT_CAPABILITY_BEFORE_MATCH, before, 4000)) {
        rt_sync_point_open();
        pthread_join(holder, NULL);
        (void)rt_executor_request_shutdown(ex);
        return fail("entitlement capability stand: the late holder never reached the match window");
    }

    // The slot moves on underneath the held holder: the occupant it named is
    // destroyed, the same bytes are rebound, and a different value is published
    // into them. Every field the capability recorded still matches except the
    // one the rebind moved.
    rt_value_cell_dispose(&target->result);
    if (rt_value_cell_bind(&target->result, &entitlement_value_ops) != RT_SLOT_CONTROL_OK) {
        rt_sync_point_open();
        pthread_join(holder, NULL);
        (void)rt_executor_request_shutdown(ex);
        return fail("entitlement capability stand: the slot could not be rebound");
    }
    void* destination = rt_value_cell_publish_storage(&target->result);
    if (destination == NULL) {
        rt_sync_point_open();
        pthread_join(holder, NULL);
        (void)rt_executor_request_shutdown(ex);
        return fail("entitlement capability stand: the rebound slot took no value");
    }
    entitlement_value second;
    second.marker = ENTITLEMENT_VALUE_LIVE;
    second.text = entitlement_text("second-occupant");
    entitlement_move(destination, &second);
    (void)rt_value_cell_commit(&target->result);
    unsigned drops_after_rebind = atomic_load_explicit(&g_ent_drops, memory_order_acquire);

    rt_sync_point_open();
    pthread_join(holder, NULL);

    unsigned taken = atomic_load_explicit(&g_ent_late_taken, memory_order_acquire);
    int second_still_there = rt_value_cell_is_ready(&target->result);
    fprintf(stderr,
            "entitlement capability window: rebind_drops=%u late_taken=%u second_occupant_ready=%d\n",
            drops_after_rebind, taken, second_still_there);
    if (drops_after_rebind != 1) {
        (void)rt_executor_request_shutdown(ex);
        return fail("entitlement capability stand: the first occupant was not destroyed exactly "
                    "once before the rebind");
    }
    if (taken != 0 || second_still_there == 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("entitlement capability stand: a capability minted for one occupant was spent "
                    "on the next one in the same storage");
    }
    // The second occupant is still the slot's, and the slot owes it exactly one
    // destruction.
    rt_value_cell_dispose(&target->result);
    entitlement_report("entitlement capability stand");
    if (atomic_load_explicit(&g_ent_drops, memory_order_acquire) != 2 ||
        atomic_load_explicit(&g_ent_double_drops, memory_order_acquire) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("entitlement capability stand: the two occupants were not destroyed exactly "
                    "once each");
    }
    rt_task_handle_drop(target);
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
#endif
`
