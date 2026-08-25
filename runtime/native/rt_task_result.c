#include "rt_task_result.h"

int rt_task_result_matches(const rt_value_cell* cell, const rt_result_source* source) {
    return cell != NULL && source != NULL && source->result_generation != 0 &&
           rt_value_cell_generation(cell) == source->result_generation &&
           rt_value_cell_is_ready(cell);
}
