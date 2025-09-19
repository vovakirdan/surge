#ifndef SURGE_PARSER_H
#define SURGE_PARSER_H

#include <stdbool.h>
#include "ast.h"
#include "lexer.h"

typedef struct SurgeParser {
    SurgeLexer *lx;
    SurgeToken cur;
    bool had_error;
} SurgeParser;

// initialize parser (takes ownership of currrent token via next())
bool parser_init(SurgeParser *ps, SurgeLexer *lx);

// Parse whole unit (program)
SurgeAstUnit *parser_parse_unit(SurgeParser *ps);

// Destroy parser (releases current token)
void parser_destroy(SurgeParser *ps);

#endif // SURGE_PARSER_H
