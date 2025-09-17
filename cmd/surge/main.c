#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "config.h"
#include "lexer.h"
#include "parser.h"
#include "ast.h"

static void usage(const char *prog) {
    fprintf(stderr, "Surge v%d.%d.%d\n", SURGE_VERSION_MAJOR, SURGE_VERSION_MINOR, SURGE_VERSION_PATCH);
    fprintf(stderr, "Usage: %s <file.sg | ->\n", prog);
    fprintf(stderr, "Phase A pt.2: parse program and print AST snapshot\n");
}

int main(int argc, char **argv) {
    if (argc < 2) { usage(argv[0]); return 2; }

    SurgeLexer lx = {0};
    int ok = 0;

    if (strcmp(argv[1], "-") == 0) {
        size_t cap = 4096, len = 0;
        char *buf = (char*)malloc(cap);
        if (!buf) { fprintf(stderr, "OOM\n"); return 1; }
        int ch;
        while ((ch = fgetc(stdin)) != EOF) {
            if (len + 1 >= cap) {
                cap *= 2;
                char *nb = (char*)realloc(buf, cap);
                if (!nb) { free(buf); fprintf(stderr, "OOM\n"); return 1; }
                buf = nb;
            }
            buf[len++] = (char)ch;
        }
        buf[len] = '\0';
        ok = surge_lexer_init_from_string(&lx, buf, "<stdin>");
        free(buf);
    } else {
        ok = surge_lexer_init_from_file(&lx, argv[1]);
    }

    if (!ok) { fprintf(stderr, "Failed to initialize lexer for %s\n", argv[1]); return 1; }

    SurgeParser ps;
    parser_init(&ps, &lx);
    SurgeAstUnit *unit = parser_parse_unit(&ps);

    ast_print_unit(unit, stdout);

    ast_free_unit(unit);
    parser_destroy(&ps);
    surge_lexer_destroy(&lx);
    return 0;
}
