#include <stdio.h>
#include "config.h"

int main(int argc, char **argv) {
    (void)argc; (void)argv;
    printf("surgetest v%d.%d.%d — doctest runner stub (Phase J)\n",
           SURGE_VERSION_MAJOR, SURGE_VERSION_MINOR, SURGE_VERSION_PATCH);
    printf("Usage to be implemented. For now, see `make test` scaffolding.\n");
    return 0;
}
