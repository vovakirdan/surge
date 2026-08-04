#ifndef RT_CARRIER_LIVENESS_H
#define RT_CARRIER_LIVENESS_H

#ifdef RT_TEST_SYNC_POINTS

#include <stdbool.h>

bool rt_carrier_liveness_jumbo_admitted(void);
bool rt_carrier_liveness_credit_parked(void);
void rt_carrier_liveness_release_jumbo(void);
bool rt_carrier_liveness_request_shutdown(void);

#endif

#endif
