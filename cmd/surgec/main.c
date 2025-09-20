#include <stdio.h>
#include <string.h>
#include <stdbool.h>
#include "lexer.h"
#include "parser.h"
#include "diagnostics.h"
#include "codegen.h"

static void usage(const char *prog){
    fprintf(stderr, "Usage: %s [--trace] <input.sg> -o <out.sbc>\n", prog);
}

int main(int argc, char **argv){
    if (argc < 4){ usage(argv[0]); return 2; }
    const char *in = NULL, *out = NULL;
    bool trace = false;
    for (int i = 1; i < argc; ++i) {
        const char *arg = argv[i];
        if (strcmp(arg, "--trace") == 0) {
            trace = true;
            continue;
        }
        if (strcmp(arg, "-o") == 0) {
            if (i + 1 >= argc) {
                usage(argv[0]);
                return 2;
            }
            out = argv[++i];
            continue;
        }
        if (!in) {
            in = arg;
            continue;
        }
        usage(argv[0]);
        return 2;
    }
    if (!in || !out){ usage(argv[0]); return 2; }

    SurgeLexer lx = {0};
    if (!surge_lexer_init_from_file(&lx, in)){ fprintf(stderr,"lexer: failed\n"); return 1; }
    surge_diag_set_source(lx.file? lx.file : in, lx.buf, lx.len);

    SurgeParser ps; parser_init(&ps, &lx);
    SurgeAstUnit *unit = parser_parse_unit(&ps);
    if (ps.had_error){ parser_destroy(&ps); surge_lexer_destroy(&lx); return 1; }

    surge_codegen_set_trace(trace);
    CgResult r = surge_codegen_unit(unit, out);
    ast_free_unit(unit);
    parser_destroy(&ps);
    surge_lexer_destroy(&lx);

    if (r != CG_OK){ fprintf(stderr, "codegen: failed\n"); return 1; }
    printf("OK: %s -> %s\n", in, out);
    return 0;
}
