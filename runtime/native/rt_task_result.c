#include "rt_task_result.h"

#include "rt_sync_point.h"

int rt_task_result_matches(const rt_value_cell* cell, const rt_result_source* source) {
    if (cell == NULL || source == NULL || source->result_generation == 0) {
        return 0;
    }
    // The capability has resolved to a live task; what is still open is whether
    // the slot holds the OCCUPANT it was minted for. The generation is what
    // answers that: the same bytes can be rebound and refilled, and a holder
    // that arrives late must be told "gone" rather than handed whoever moved in.
    RT_SYNC_POINT(SP_RESULT_CAPABILITY_BEFORE_MATCH);
    return RT_RESULT_GENERATION_MATCHES(cell, source) && rt_value_cell_is_ready(cell);
}
