// A container's element slot that a task handle was moved out of holds NULL,
// and the container's drop glue still visits it. rt_task_handle_drop is what
// that visit calls, so it must treat an empty slot as nothing to give back --
// it used to hand NULL to task_from_handle, which panics "invalid task
// handle" and ends the process at teardown of every array of task handles.
#include "rt.h"

#include <stdint.h>
#include <stdio.h>

// The program-side symbols the runtime links against; this stand runs no
// program, so each is the empty answer.
int rt_argc = 0;
char** rt_argv_raw = NULL;

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst);

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id) {
    (void)id;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    if (out_dst != NULL) {
        *(uint64_t*)out_dst = 0;
    }
}

int main(void) {
    rt_task_handle_drop(NULL);
    puts("null-handle-drop-ok");
    return 0;
}
