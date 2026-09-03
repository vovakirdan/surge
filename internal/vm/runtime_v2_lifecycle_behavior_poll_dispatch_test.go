//go:build runtime_v2_pending

package vm_test

// lifecycleHarnessPollDispatch holds the harness's drop-dispatch stubs and the
// __surge_poll_call switch -- the one place that maps a poll-fn id to the C
// function a stand wrote for it. It lives apart from lifecycleHarnessMain
// because it is the only part of the harness EVERY stand adds a line to: a new
// stand adds its poll function to its own file and one case here, so the
// dispatcher grows with the stand count while the drivers and main do not.
// Keeping them in one file walked that file into the 500-line cap
// (runtime-v2-file-size-check, CROSSED_500 at effective 503).
//
// Concatenated by buildRuntimeV2LifecycleHarnessWithFlags
// (runtime_v2_lifecycle_behavior_harness_test.go) AFTER every mode constant --
// each case here names a function one of them defines -- and immediately before
// lifecycleHarnessMain.
const lifecycleHarnessPollDispatch = `
// Drop-dispatch stub: no harness state struct carries a drop obligation
// (drop-fn id 0 never dispatches), so reaching this is a test bug.
void __surge_drop_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

void __surge_drop_result_call(uint64_t id, void* value) {
    (void)id;
    (void)value;
}

void __surge_drop_abandoned_state_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

void __surge_poll_call(uint64_t id) {
    switch (id) {
        case POLL_OWNER_PROBE:
            poll_owner_probe();
            break;
        case POLL_JOIN_TARGET_SPIN:
            poll_join_target_spin();
            break;
        case POLL_JOIN_TARGET_QUICK:
            poll_join_target_quick();
            break;
        case POLL_JOINER:
            poll_joiner();
            break;
        case POLL_CLONE_TARGET:
            poll_clone_target();
            break;
        case POLL_CLONE_RACER:
            poll_clone_racer();
            break;
        case POLL_PIN_TARGET:
            poll_pin_target();
            break;
        case POLL_PIN_JOINER:
            poll_pin_joiner();
            break;
        case POLL_SCOPE_CHILD_QUICK:
            poll_scope_child_quick();
            break;
        case POLL_SCOPE_CHILD_SPIN:
            poll_scope_child_spin();
            break;
        case POLL_SCOPE_OWNER:
            poll_scope_owner();
            break;
        case POLL_SCOPE_CANCEL_OWNER:
            poll_scope_cancel_owner();
            break;
        case POLL_SPIN_FOREVER:
            poll_spin_forever();
            break;
        case POLL_TIMER_PARK:
            poll_timer_park();
            break;
        case POLL_CHANNEL_PARK:
            poll_channel_park();
            break;
        case POLL_BLOCKING_PARK:
            poll_blocking_park();
            break;
        case POLL_EXTERNAL_AWAIT_TARGET:
            poll_external_await_target();
            break;
        case POLL_MAKE_CHAN:
            poll_make_chan();
            break;
        case POLL_SCOPE_OWNER_FOREVER:
            poll_scope_owner_forever();
            break;
        case POLL_JOIN_TARGET_GATED:
            poll_join_target_gated();
            break;
        case POLL_PARK_FOREVER:
            poll_park_forever();
            break;
        case POLL_CARRIER_OWNER:
            poll_carrier_owner();
            break;
        case POLL_CARRIER_CHILD:
            poll_carrier_child();
            break;
        case POLL_CARRIER_SPINNER:
            poll_carrier_spinner();
            break;
        case POLL_CARRIER_SHUTDOWN_OWNER:
            poll_carrier_shutdown_owner();
            break;
        case POLL_MAKE_PARK_FOREVER_CHAN:
            poll_make_park_forever_chan();
            break;
        case POLL_SCOPE_OWNER_FAILFAST:
            poll_scope_owner_failfast();
            break;
        case POLL_ADOPT_TARGET:
            poll_adopt_target();
            break;
        case POLL_ADOPT_JOINER:
            poll_adopt_joiner();
            break;
        case POLL_XOWNER_GRANDCHILD:
            poll_xowner_grandchild();
            break;
        case POLL_XOWNER_SCOPE_CHILD:
            poll_xowner_scope_child();
            break;
        case POLL_XOWNER_OWNER:
            poll_xowner_owner();
            break;
        case POLL_STAND_TRAP_OWNER:
            poll_stand_trap_owner();
            break;
	#ifdef RT_TEST_SYNC_POINTS
        case POLL_DEBT020_ADOPT_JOINER:
            poll_debt020_adopt_joiner();
            break;
        case POLL_DEBT261_SCOPE_OWNER:
            poll_debt261_scope_owner();
            break;
        case POLL_DEBT280_SCOPE_OWNER:
            poll_debt280_scope_owner();
            break;
        case POLL_DEBT280_SCOPE_CHILD:
            poll_debt280_scope_child();
            break;
        case POLL_DEBT280_GRANDCHILD:
            poll_debt280_grandchild();
            break;
        case POLL_DEBT263_CANCELLED_CHILD:
            poll_debt263_cancelled_child();
            break;
        case POLL_DEBT263_SCOPE_OWNER:
            poll_debt263_scope_owner();
            break;
        case POLL_DEBT263_SEALED_TASK:
            poll_debt263_sealed_task();
            break;
        case POLL_DEBT020_GAP_JOINER:
            poll_debt020_gap_joiner();
            break;
        case POLL_DEBT022_GATED_TARGET:
            poll_debt022_gated_target();
            break;
        case POLL_CANCEL_PARK_PROOF:
            poll_cancel_park_proof();
            break;
        case POLL_DEBT046_JOINER:
            poll_debt046_joiner();
            break;
        case POLL_DEBT248_TARGET:
            poll_debt248_target();
            break;
        case POLL_DEBT248_JOINER:
            poll_debt248_joiner();
            break;
        case POLL_READY_REQUEUE_PROBE:
            poll_ready_requeue_probe();
            break;
        case POLL_INLINE_CLAIM_OWNER:
            poll_inline_claim_owner();
            break;
        case POLL_INLINE_CLAIM_CHILD:
            poll_inline_claim_child();
            break;
        case POLL_SCOPE_MEMBERSHIP_OWNER:
            poll_scope_membership_owner();
            break;
        case POLL_SCOPE_MEMBERSHIP_CHILD:
            poll_scope_membership_child();
        case POLL_ENTITLEMENT_OWNING_RESULT:
            poll_entitlement_owning_result();
            break;
        case POLL_DEBT291_PIN_PROBE:
            poll_debt291_pin_probe();
            break;
#endif
        default:
            break;
    }
    rt_async_return(NULL, &(uint64_t){0});
}
`
