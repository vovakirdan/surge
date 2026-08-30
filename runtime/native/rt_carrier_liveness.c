#include "rt_carrier_liveness.h"

#ifdef RT_TEST_SYNC_POINTS

#include "rt_async_internal.h"
#include "rt_sync_point.h"

bool rt_carrier_liveness_jumbo_admitted(void) {
    return rt_sync_point_reached_count(RT_SYNC_POINT_SP_CARRIER_JUMBO_ADMITTED) != 0;
}

bool rt_carrier_liveness_credit_parked(void) {
    return rt_sync_point_reached_count(RT_SYNC_POINT_SP_TRANSPORT_DATA_SLOT_TASK_PARKED) != 0;
}

void rt_carrier_liveness_release_jumbo(void) {
    rt_sync_point_open();
}

bool rt_carrier_liveness_request_shutdown(void) {
    return rt_executor_request_shutdown(ensure_exec()) == RT_RUNTIME_STATUS_OK;
}

#else

typedef int rt_carrier_liveness_translation_unit_not_empty;

#endif
