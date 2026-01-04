#include "rt.h"
#include <stddef.h>

void __surge_start(void);

int rt_argc = 0;
char** rt_argv_raw = NULL;

int main(int argc, char** argv) {
    rt_argc = argc;
    rt_argv_raw = argv;
    __surge_start();
    return 0;
}
