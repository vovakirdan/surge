#include "rt.h"

void __surge_start(void);

int main(int argc, char** argv) {
    (void)argc;
    (void)argv;
    __surge_start();
    return 0;
}
