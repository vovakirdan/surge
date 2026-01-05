#include "rt.h"
#include <stddef.h>

#if defined(__GNUC__) || defined(__clang__)
__attribute__((weak)) void __surge_start(void) {}
#else
void __surge_start(void) {}
#endif

int rt_argc = 0;
char** rt_argv_raw = NULL;

int main(int argc, char** argv) {
    rt_argc = argc;
    rt_argv_raw = argv;
    __surge_start();
    return 0;
}
