#include "rt.h"
#include <stddef.h>

void __surge_start(void);
#if defined(_MSC_VER)
void __surge_start_default(void);
#endif

#if defined(__GNUC__) || defined(__clang__)
__attribute__((weak)) void __surge_start(void) {
}
#elif defined(_MSC_VER)
void __surge_start_default(void) {
}
#pragma comment(linker, "/alternatename:__surge_start=__surge_start_default")
#else
#pragma weak __surge_start
void __surge_start(void) {
}
#endif

int rt_argc = 0;
char** rt_argv_raw = NULL;

int main(int argc, char** argv) {
    rt_argc = argc;
    rt_argv_raw = argv;
    __surge_start();
    return 0;
}
