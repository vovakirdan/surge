// The one canonical result slot a task owns, exercised on its own before any
// task uses one. It links the whole runtime because the slot allocates through
// it -- the point of the wide-result row is that the allocation is REAL.
#include "rt_value_cell.h"

#include "rt_async_internal.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// What a task's canonical result slot has to guarantee before any task uses
// one: a value published once and read once, a small result that costs no
// allocation, a wide one that costs exactly one, and a value nobody took
// destroyed exactly once.

typedef struct {
    uint64_t marker;
} narrow_result;

// A local result never crosses; the slot refuses rather than inventing a plan.
static rt_carrier_status
task_result_plan_cross(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

typedef struct {
    uint64_t marker;
    uint64_t filler[7];
} wide_result;

int rt_argc = 0;
char** rt_argv_raw = NULL;

static int task_result_drops;
static uint64_t task_result_last_dropped;

static void task_result_move(void* destination, void* source) {
    memcpy(destination, source, sizeof(narrow_result));
    ((narrow_result*)source)->marker = 0;
}

static void task_result_drop(void* value) {
    task_result_last_dropped = ((narrow_result*)value)->marker;
    ((narrow_result*)value)->marker = 0;
    task_result_drops++;
}

static void wide_result_move(void* destination, void* source) {
    memcpy(destination, source, sizeof(wide_result));
    ((wide_result*)source)->marker = 0;
}

static void wide_result_drop(void* value) {
    task_result_last_dropped = ((wide_result*)value)->marker;
    ((wide_result*)value)->marker = 0;
    task_result_drops++;
}

static const rt_value_ops narrow_ops = {
    .layout = {.size = sizeof(narrow_result),
               .align = _Alignof(narrow_result),
               .stride = sizeof(narrow_result),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = task_result_move,
    .copy_init = NULL,
    .clone_init = NULL,
    .drop_in_place = task_result_drop,
    .trace = NULL,
    .plan_cross = task_result_plan_cross,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

static const rt_value_ops wide_ops = {
    .layout = {.size = sizeof(wide_result),
               .align = _Alignof(wide_result),
               .stride = sizeof(wide_result),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = wide_result_move,
    .copy_init = NULL,
    .clone_init = NULL,
    .drop_in_place = wide_result_drop,
    .trace = NULL,
    .plan_cross = task_result_plan_cross,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

static const rt_value_ops zero_sized_ops = {
    .layout = {.size = 0, .align = 1, .stride = 0, .flags = 0},
    .move_init = task_result_move,
    .copy_init = NULL,
    .clone_init = NULL,
    .drop_in_place = NULL,
    .trace = NULL,
    .plan_cross = task_result_plan_cross,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

#define REQUIRE(expr)                                                                              \
    do {                                                                                           \
        if (!(expr)) {                                                                             \
            fprintf(stderr, "task-result stand failed: %s at %s:%d\n", #expr, __FILE__, __LINE__); \
            return 1;                                                                              \
        }                                                                                          \
    } while (0)

static int task_result_cases(void) {
    task_result_drops = 0;
    task_result_last_dropped = 0;

    // A task with no result value: bound, inert, and safe to dispose.
    rt_value_cell none;
    REQUIRE(rt_value_cell_bind(&none, NULL) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_value_cell_publish_storage(&none) == NULL);
    REQUIRE(!rt_value_cell_is_ready(&none));
    rt_value_cell_dispose(&none);

    // A narrow result lives in the task's own bytes -- no allocation is what
    // makes this slot no worse than the machine word it replaces.
    rt_value_cell narrow;
    REQUIRE(rt_value_cell_bind(&narrow, &narrow_ops) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_value_cell_publish_storage(&narrow) == (void*)narrow.inline_storage);
    narrow_result produced = {.marker = 7};
    narrow_ops.move_init(rt_value_cell_publish_storage(&narrow), &produced);
    REQUIRE(produced.marker == 0);
    REQUIRE(rt_value_cell_commit(&narrow) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_value_cell_is_ready(&narrow));

    // A task completes once: a second publication is refused rather than
    // overwriting a value someone may already be reading.
    REQUIRE(rt_value_cell_publish_storage(&narrow) == NULL);
    REQUIRE(rt_value_cell_commit(&narrow) == RT_SLOT_CONTROL_INVALID_STATE);

    // Reading it out leaves the slot with no obligation, so disposing after a
    // move destroys nothing.
    narrow_result taken = {.marker = 0};
    narrow_ops.move_init(&taken, rt_value_cell_value(&narrow));
    REQUIRE(taken.marker == 7);
    REQUIRE(rt_value_cell_commit_move(&narrow) == RT_SLOT_CONTROL_OK);
    REQUIRE(!rt_value_cell_is_ready(&narrow));
    REQUIRE(rt_value_cell_commit_move(&narrow) == RT_SLOT_CONTROL_INVALID_STATE);
    rt_value_cell_dispose(&narrow);
    REQUIRE(task_result_drops == 0);

    // A result nobody took is destroyed exactly once, by the slot.
    rt_value_cell untaken;
    REQUIRE(rt_value_cell_bind(&untaken, &narrow_ops) == RT_SLOT_CONTROL_OK);
    narrow_result abandoned = {.marker = 11};
    narrow_ops.move_init(rt_value_cell_publish_storage(&untaken), &abandoned);
    REQUIRE(rt_value_cell_commit(&untaken) == RT_SLOT_CONTROL_OK);
    rt_value_cell_dispose(&untaken);
    REQUIRE(task_result_drops == 1);
    REQUIRE(task_result_last_dropped == 11);
    rt_value_cell_dispose(&untaken);
    REQUIRE(task_result_drops == 1);

    // A wide result takes one block, which is what the box it replaces cost.
    rt_value_cell wide;
    REQUIRE(rt_value_cell_bind(&wide, &wide_ops) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_value_cell_publish_storage(&wide) != (void*)wide.inline_storage);
    REQUIRE(wide.owns_block == 1);
    wide_result produced_wide;
    memset(&produced_wide, 0, sizeof(produced_wide));
    produced_wide.marker = 13;
    wide_ops.move_init(rt_value_cell_publish_storage(&wide), &produced_wide);
    REQUIRE(rt_value_cell_commit(&wide) == RT_SLOT_CONTROL_OK);
    rt_value_cell_dispose(&wide);
    REQUIRE(task_result_drops == 2);
    REQUIRE(task_result_last_dropped == 13);

    // A zero-sized result has no bytes and still has a lifecycle.
    rt_value_cell zero;
    REQUIRE(rt_value_cell_bind(&zero, &zero_sized_ops) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_value_cell_publish_storage(&zero) != NULL);
    REQUIRE(zero.owns_block == 0);
    REQUIRE(rt_value_cell_commit(&zero) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_value_cell_is_ready(&zero));
    rt_value_cell_dispose(&zero);
    REQUIRE(task_result_drops == 2);
    printf("task result slot: drops=%d\n", task_result_drops);
    return 0;
}

// The runtime this stand links expects these; no task of its own ever polls.
void __surge_poll_call(uint64_t id) {
    (void)id;
    rt_async_return(NULL, &(uint64_t){0});
}

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

void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    if (out_dst != NULL) {
        *(uint64_t*)out_dst = 0;
    }
    return;
}

int main(int argc, char** argv) {
    (void)argc;
    (void)argv;
    return task_result_cases();
}
