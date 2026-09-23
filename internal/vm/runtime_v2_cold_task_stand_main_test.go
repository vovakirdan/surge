package vm_test

// The driver half of the cold task stand's translation unit (RV2-DEBT-370): one mode per
// row of runtime_v2_cold_task_test.go, appended after coldTaskStand.
const coldTaskStandMain = `
static uint8_t await_task(void* handle, uint64_t* out) {
    uint8_t kind = 0;
    rt_task_await(handle, &kind, out);
    return kind;
}

static uint32_t run_owner(uint64_t poll_id) {
    void* owner = __task_create(poll_id, NULL, rt_channel_opaque_word_ops());
    uint64_t out = 0;
    uint32_t verdict = await_task(owner, &out) != 1 ? 99u : (uint32_t)out;
    atomic_store(&g_owner_verdict, verdict);
    return verdict;
}

#ifdef RT_TEST_SYNC_POINTS
// A cancel by id, as the scope and parent walks make it: under the control
// lane, with no handle of its own.
static void* cancel_by_id(void* arg) {
    rt_executor* ex = ensure_exec();
    rt_control_lock(ex);
    cancel_task(ex, (uint64_t)(uintptr_t)arg);
    rt_control_unlock(ex);
    return NULL;
}
#endif

#ifdef RT_TEST_SYNC_POINTS
// The thread that drops the escaped member's last handle once the join's walk
// is held after get_task. Its drop discards the task and then waits, inside
// the free, for the control lane the walk holds.
static void* walk_dropper(void* arg) {
    unsigned reached = (unsigned)(uintptr_t)arg;
    if (!rt_sync_point_wait_until_after(RT_SYNC_POINT_SP_COLD_JOIN_WALK_AFTER_GET_TASK, reached)) {
        return NULL;
    }
    void* child = atomic_load(&g_escaped);
    atomic_store(&g_walk_child_id, ((rt_task*)child)->id);
    atomic_store(&g_walk_child, child);
    rt_task_handle_drop(child);
    return NULL;
}

// Opens the walk once the discard has retired the member (status DONE is
// written after the retirement's scope event is sent), then waits 50 ms more
// and asks the task table whether the member is still there: the free takes the
// control lane the walk holds, so it must be. A free that did not wait for the
// walk would have cleared the slot by now -- the row's plain build sees that,
// and its AddressSanitizer build sees the walk read freed memory (packet
// section 000, review D1; CF8 is the negative control).
static void* walk_opener(void* arg) {
    (void)arg;
    for (int i = 0; i < 10000; i++) {
        const rt_task* child = (const rt_task*)atomic_load(&g_walk_child);
        if (child != NULL && rt_task_publication_load(child) == RT_TASK_DISCARDED &&
            task_status_load(child) == TASK_DONE) {
            struct timespec settle = {0, 50000000};
            nanosleep(&settle, NULL);
            if (get_task(ensure_exec(), atomic_load(&g_walk_child_id)) != child) {
                atomic_store(&g_walk_freed_early, 1);
            }
            rt_sync_point_open();
            return NULL;
        }
        struct timespec pause = {0, 1000000};
        nanosleep(&pause, NULL);
    }
    atomic_store(&g_walk_timeout, 1);
    rt_sync_point_open();
    return NULL;
}
#endif

static void drain_ready(void) {
    rt_executor* ex = ensure_exec();
    while (run_ready_one(ex)) {
    }
}

// A frame released by mark_done is deferred until that lane holds no lock, and
// the lane may be another worker's: wait for want releases, let a late extra
// one land, and answer whether there were exactly want.
static int frame_drops_settle(uint32_t want) {
    for (int i = 0; i < 5000 && atomic_load(&g_frame_drops) < want; i++) {
        struct timespec pause = {0, 1000000};
        nanosleep(&pause, NULL);
    }
    struct timespec settle = {0, 10000000};
    nanosleep(&settle, NULL);
    return atomic_load(&g_frame_drops) == want;
}

static void* drop_one(void* handle) {
    rt_task_handle_drop(handle);
    return NULL;
}

int main(int argc, char** argv) {
    if (argc != 3) {
        return 2;
    }
    const char* mode = argv[1];
    long rounds = strtol(argv[2], NULL, 10);
    for (long round = 0; round < rounds; round++) {
        atomic_store(&g_frame_drops, 0);
        atomic_store(&g_child_polls, 0);
        atomic_store(&g_owner_phase, 0);
        atomic_store(&g_escaped, NULL);
        atomic_store(&g_owner_verdict, 0);
        uint64_t out = 0;
        if (strcmp(mode, "drop") == 0) {
            void* child = create_child(0);
            if (!is_cold(child)) {
                return fail(mode, "a created task is published");
            }
            rt_task_handle_drop(child);
            if (atomic_load(&g_frame_drops) != 1 || atomic_load(&g_child_polls) != 0) {
                return fail(mode, "the dropped task ran or kept its frame");
            }
        } else if (strcmp(mode, "spawn") == 0) {
            void* child = create_child(0);
            rt_task_wake(child);
            const rt_task* task = (const rt_task*)child;
            if (rt_task_publication_load(task) != RT_TASK_PUBLISHED ||
                task_enqueued_load(task) == 0) {
                return fail(mode, "spawn did not publish the task");
            }
            if (await_task(child, &out) != 1 || out != 7 || atomic_load(&g_child_polls) != 1 ||
                atomic_load(&g_frame_drops) != 1) {
                return fail(mode, "the spawned task did not run once");
            }
        } else if (strcmp(mode, "await") == 0) {
            void* child = create_child(0);
            if (await_task(child, &out) != 1 || out != 7 || atomic_load(&g_child_polls) != 1 ||
                atomic_load(&g_frame_drops) != 1) {
                return fail(mode, "the awaited task did not run once");
            }
        } else if (strcmp(mode, "clone") == 0) {
            void* child = create_child(0);
            void* sibling = rt_task_clone(child, NULL);
            rt_task_handle_drop(child);
            if (!is_cold(sibling) || atomic_load(&g_frame_drops) != 0) {
                return fail(mode, "a handle that was not the last ended the task");
            }
            rt_task_handle_drop(sibling);
            if (atomic_load(&g_frame_drops) != 1 || atomic_load(&g_child_polls) != 0) {
                return fail(mode, "the last handle did not end the cold task");
            }
        } else if (strcmp(mode, "cancel") == 0) {
            void* child = create_child(0);
            rt_task_cancel(child);
            // Published by the cancel's own wake with the cancel recorded, or
            // already answered by a worker, which writes PUBLISHED back (release)
            // after it stored RUNNING.
            uint8_t published_word = rt_task_publication_load((const rt_task*)child);
            if (published_word != RT_TASK_CANCELLED_COLD &&
                (published_word != RT_TASK_PUBLISHED || task_status_load((const rt_task*)child) == TASK_READY)) {
                return fail(mode, "cancel did not publish the task as cancelled while cold");
            }
            if (await_task(child, &out) != 2 || atomic_load(&g_child_polls) != 0) {
                return fail(mode, "the task cancelled while cold was not answered Cancelled() unrun");
            }
            if (!frame_drops_settle(1)) {
                return fail(mode, "its start frame was not given back exactly once");
            }
        } else if (strcmp(mode, "scope-drop") == 0) {
            if (run_owner(POLL_OWNER_DROP) != 1 || atomic_load(&g_child_polls) != 0 ||
                atomic_load(&g_frame_drops) != 1) {
                return fail(mode, "the dropped member held the scope or ran");
            }
        } else if (strcmp(mode, "scope-escape") == 0) {
            if (run_owner(POLL_OWNER_ESCAPE) != 1) {
                return fail(mode, "the join did not publish and join the escaped member");
            }
            void* child = atomic_load(&g_escaped);
            if (await_task(child, &out) != 1 || out != 7 || atomic_load(&g_frame_drops) != 1) {
                return fail(mode, "the escaped member's answer was lost");
            }
        } else if (strcmp(mode, "affine-inline") == 0) {
            if (run_owner(POLL_OWNER_AFFINE_INLINE) != 1 ||
                atomic_load(&g_child_worker) != atomic_load(&g_carrier)) {
                return fail(mode, "the affine child did not run inline on its carrier");
            }
        } else if (strcmp(mode, "affine-handoff") == 0) {
            if (run_owner(POLL_OWNER_AFFINE_HANDOFF) != 1) {
                return fail(mode, "the creator did not pin the child before publication");
            }
            void* child = atomic_load(&g_escaped);
            rt_task_wake(child);
            if (await_task(child, &out) != 1 || out != 7 ||
                atomic_load(&g_child_worker) != atomic_load(&g_carrier)) {
                return fail(mode, "the affine child ran off its carrier");
            }
        } else if (strcmp(mode, "scope-foreign-publish") == 0 ||
                   strcmp(mode, "scope-foreign-drop") == 0) {
            int publish = strcmp(mode, "scope-foreign-publish") == 0;
            uint64_t before = scope_events();
            void* owner = __task_create(POLL_OWNER_FOREIGN, NULL, rt_channel_opaque_word_ops());
            if (!run_ready_one(ensure_exec())) {
                return fail(mode, "the owner did not take its first turn");
            }
            void* child = atomic_load(&g_escaped);
            if (publish) {
                rt_task_wake(child);
            } else {
                rt_task_handle_drop(child);
            }
            if (await_task(owner, &out) != 1 || out != 1) {
                return fail(mode, "the owner's second turn found the hint or the count wrong");
            }
            if (scope_events() == before) {
                return fail(mode, "a lane that is not the scope's owner lane wrote the scope");
            }
            if (atomic_load(&g_child_polls) != (publish ? 1u : 0u) ||
                atomic_load(&g_frame_drops) != 1) {
                return fail(mode, "the member ran when it should not, or not when it should");
            }
            if (publish) {
                rt_task_handle_drop(child);
            }
        } else if (strcmp(mode, "drop-then-cancel") == 0) {
            void* child = create_child(0);
            uint64_t id = ((rt_task*)child)->id;
            rt_task_handle_drop(child);
            rt_executor* ex = ensure_exec();
            rt_control_lock(ex);
            cancel_task(ex, id);
            rt_control_unlock(ex);
            drain_ready();
            if (atomic_load(&g_frame_drops) != 1 || atomic_load(&g_child_polls) != 0) {
                return fail(mode, "a cancel after the discard revived the task");
            }
#ifdef RT_TEST_SYNC_POINTS
        } else if (strcmp(mode, "cancel-race") == 0) {
            // The cancel holds after its gate CAS, before its wake; the last
            // drop lands in that window, finds the gate taken, and publishes.
            void* child = create_child(0);
            uint64_t id = ((rt_task*)child)->id;
            unsigned reached = rt_sync_point_reached_count(RT_SYNC_POINT_SP_CANCEL_BEFORE_WAKE);
            rt_sync_point_arm_block(RT_SYNC_POINT_SP_CANCEL_BEFORE_WAKE);
            pthread_t other;
            if (pthread_create(&other, NULL, cancel_by_id, (void*)(uintptr_t)id) != 0) {
                return fail(mode, "thread");
            }
            if (!rt_sync_point_wait_until_after(RT_SYNC_POINT_SP_CANCEL_BEFORE_WAKE, reached)) {
                return fail(mode, "the cancel never reached its wake");
            }
            rt_task_handle_drop(child);
            // Still inside the window: the cancel holds the control lane, and a
            // task is freed only on that lane, so the word can be read. The drop
            // must have published the task, not discarded it: CANCELLED_COLD, or
            // PUBLISHED once a worker has already answered it.
            uint8_t published_word = rt_task_publication_load((const rt_task*)child);
            rt_sync_point_open();
            pthread_join(other, NULL);
            rt_sync_point_disarm(RT_SYNC_POINT_SP_CANCEL_BEFORE_WAKE);
            drain_ready();
            if (published_word != RT_TASK_CANCELLED_COLD && published_word != RT_TASK_PUBLISHED) {
                return fail(mode, "the last drop discarded a task whose gate a cancel had taken");
            }
            if (atomic_load(&g_child_polls) != 0 || !frame_drops_settle(1)) {
                return fail(mode, "a cancelled cold task was not published by its last drop and answered unrun");
            }
#endif
#ifdef RT_TEST_SYNC_POINTS
        } else if (strcmp(mode, "join-walk-drop") == 0) {
            unsigned reached =
                rt_sync_point_reached_count(RT_SYNC_POINT_SP_COLD_JOIN_WALK_AFTER_GET_TASK);
            rt_sync_point_arm_block(RT_SYNC_POINT_SP_COLD_JOIN_WALK_AFTER_GET_TASK);
            pthread_t dropper;
            pthread_t opener;
            if (pthread_create(&dropper, NULL, walk_dropper, (void*)(uintptr_t)reached) != 0 ||
                pthread_create(&opener, NULL, walk_opener, NULL) != 0) {
                return fail(mode, "thread");
            }
            uint32_t verdict = run_owner(POLL_OWNER_WALK_DROP);
            pthread_join(dropper, NULL);
            pthread_join(opener, NULL);
            rt_sync_point_disarm(RT_SYNC_POINT_SP_COLD_JOIN_WALK_AFTER_GET_TASK);
            if (atomic_load(&g_walk_timeout) != 0) {
                return fail(mode, "the last drop did not discard inside the walk's window");
            }
            if (atomic_load(&g_walk_freed_early) != 0) {
                return fail(mode, "the member was freed while the walk held the control lane");
            }
            if (verdict != 1 || atomic_load(&g_child_polls) != 0 ||
                atomic_load(&g_frame_drops) != 1) {
                return fail(mode,
                            "the walk published a discarded member, or the join did not drain");
            }
#endif
        } else if (strcmp(mode, "failfast-cold-member") == 0) {
            atomic_store(&g_member_polls, 0);
            atomic_store(&g_member_after_release, 0);
            atomic_store(&g_gate_seen, 0);
            atomic_store(&g_member, NULL);
            atomic_store(&g_sibling, NULL);
            uint32_t verdict = run_owner(POLL_OWNER_FAILFAST_COLD);
            if (verdict == 82) {
                return fail(mode, "the fail-fast cancel-all never reached the cold member");
            }
            if (atomic_load(&g_gate_seen) != 1) {
                return fail(mode, "the member's gate did not show the cancel before its drop");
            }
            int frames_once = frame_drops_settle(2);
            if (atomic_load(&g_member_after_release) != 0) {
                return fail(mode, "a member cancelled while cold ran after its owner released the local");
            }
            if (atomic_load(&g_member_polls) != 0 || atomic_load(&g_child_polls) != 0) {
                return fail(mode, "a task cancelled while cold entered its body");
            }
            if (verdict != 1) {
                return fail(mode, "the join did not drain, or drained without fail-fast");
            }
            if (!frames_once) {
                return fail(mode, "a start frame was not given back exactly once");
            }
        } else if (strcmp(mode, "inline-scope") == 0) {
            if (run_owner(POLL_OWNER_INLINE_SCOPE) != 1 || atomic_load(&g_child_polls) != 2 ||
                atomic_load(&g_frame_drops) != 2) {
                return fail(mode, "the inline claim took a child that was not the latest creation");
            }
        } else if (strcmp(mode, "race-drop") == 0) {
            void* child = create_child(0);
            void* sibling = rt_task_clone(child, NULL);
            pthread_t other;
            if (pthread_create(&other, NULL, drop_one, sibling) != 0) {
                return fail(mode, "thread");
            }
            rt_task_handle_drop(child);
            pthread_join(other, NULL);
            if (atomic_load(&g_frame_drops) != 1 || atomic_load(&g_child_polls) != 0) {
                return fail(mode, "two last drops did not end the task exactly once");
            }
        } else {
            return fail(mode, "unknown mode");
        }
    }
    if (strcmp(mode, "failfast-cold-member") == 0) {
        // Recorded, not required: which road published the member is the
        // scheduler's.
        printf("COLD_STAND_OK %s roads: drop=%u wake=%u answered=%u\n",
               mode,
               atomic_load(&g_road_drop),
               atomic_load(&g_road_wake),
               atomic_load(&g_road_answered));
        return 0;
    }
    printf("COLD_STAND_OK %s\n", mode);
    return 0;
}
`
