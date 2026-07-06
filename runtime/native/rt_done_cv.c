#include "rt_async_internal.h"

void rt_done_cv_broadcast_after_done(rt_executor* ex) {
    if (ex == NULL || rt_done_waiters_load_after_done(ex) == 0) {
        return;
    }
#ifndef RV2_DEBT_022_NEGATIVE_CONTROL
    int locked_here = 0;
    if (!rt_lane_holds_control()) {
        rt_control_lock(ex);
        rt_trace_control_lock_site(RT_CTRL_SITE_AWAIT_COMPAT);
        locked_here = 1;
    }
#endif
    pthread_cond_broadcast(&ex->done_cv);
#ifndef RV2_DEBT_022_NEGATIVE_CONTROL
    if (locked_here) {
        rt_control_unlock(ex);
    }
#endif
}
