#include <stdio.h>
#include "config.h"

int main(void) {
    printf("surgec v%d.%d.%d — compiler stub (Phase A)\n",
           SURGE_VERSION_MAJOR, SURGE_VERSION_MINOR, SURGE_VERSION_PATCH);
    printf("TODO: parse/compile .sg → .sbc in Phase E.\n");
    return 0;
}
