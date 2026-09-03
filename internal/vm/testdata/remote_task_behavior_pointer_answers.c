#include "remote_task_behavior.h"

#include <stdatomic.h>
#include <string.h>

// RV2-DEBT-309 (Wave F, F2): an entry point that hands generated code a
// block it stores untested answers a valid block or ends the process with
// the RT_OOM report -- never NULL. The rows below force the refusal on the
// exact allocation through the stand-only seam rt_test_alloc_refusals and
// let the process die of the report; a process that instead comes back
// with NULL is the defect the row exists for, and is what the Rule 13
// mutant RV2_DEBT_309_NEGATIVE_CONTROL restores.

// The plain-block path: rt_argv's array header goes through
// rt_alloc_or_report.
int rtb_mode_pointer_answer_alloc(void) {
    (void)ensure_exec();
    atomic_store_explicit(&rt_test_alloc_refusals, 1, memory_order_release);
    void* answer = rt_argv();
    atomic_store_explicit(&rt_test_alloc_refusals, 0, memory_order_release);
    if (answer == NULL) {
        return rtb_fail("rt_argv answered NULL where it must report the refusal");
    }
    return rtb_fail("rt_argv was refused a block and neither reported nor answered NULL");
}

// The tagged-block path: a filesystem error result goes through
// rt_tag_alloc_or_report first (the block), and only then through the
// string reporter (its message), so one refusal lands on the FsResult.
int rtb_mode_pointer_answer_tag(void) {
    (void)ensure_exec();
    static const char missing[] = "/surge-rv2-debt-309-no-such-path";
    void* path = rt_string_from_bytes((const uint8_t*)missing, sizeof(missing) - 1);
    if (path == NULL) {
        return rtb_fail("path string could not be made");
    }
    atomic_store_explicit(&rt_test_alloc_refusals, 1, memory_order_release);
    void* answer = rt_fs_metadata(&path);
    atomic_store_explicit(&rt_test_alloc_refusals, 0, memory_order_release);
    if (answer == NULL) {
        return rtb_fail("rt_fs_metadata answered NULL where it must report the refusal");
    }
    return rtb_fail("rt_fs_metadata was refused a block and neither reported nor answered NULL");
}
