#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "lexer.h"
#include "token.h"
#include "config.h"

static void usage(const char *prog) {
    fprintf(stderr, "Surge v%d.%d.%d\n", SURGE_VERSION_MAJOR, SURGE_VERSION_MINOR, SURGE_VERSION_PATCH);
    fprintf(stderr, "Usage: %s <file.sg | ->\n", prog);
    fprintf(stderr, "Phase A: lex only — prints tokens\n");
}

int main(int argc, char **argv) {
    if (argc < 2) { usage(argv[0]); return 2; }

    SurgeLexer lx = {0};
    int ok = 0;

    if (strcmp(argv[1], "-") == 0) {
        // read from stdin
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

    if (!ok) {
        fprintf(stderr, "Failed to open/initialize input: %s\n", argv[1]);
        return 1;
    }

    for (;;) {
        SurgeToken t = surge_lexer_next(&lx);
        printf("%zu:%zu\t%-12s\t", t.pos.line, t.pos.col, surge_token_kind_cstr(t.kind));
        if (t.has_int) {
            printf("lexeme=\"%s\" int=%lld\n", t.lexeme ? t.lexeme : "", (long long)t.int_value);
        } else if (t.has_float) {
            printf("lexeme=\"%s\" float=%g\n", t.lexeme ? t.lexeme : "", t.float_value);
        } else {
            printf("lexeme=\"%s\"\n", t.lexeme ? t.lexeme : "");
        }
        if (t.kind == TOK_EOF) {
            surge_token_free(&t);
            break;
        }
        surge_token_free(&t);
    }

    surge_lexer_destroy(&lx);
    return 0;
}