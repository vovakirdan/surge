#include "token.h"
#include <stdlib.h>

const char *surge_token_kind_cstr(SurgeTokenKind k) {
    switch (k) {
        case TOK_EOF: return "EOF";
        case TOK_ERROR: return "ERROR";
        case TOK_IDENTIFIER: return "IDENT";
        case TOK_KW_LET: return "KW_LET";
        case TOK_KW_SIGNAL: return "KW_SIGNAL";
        case TOK_KW_FN: return "KW_FN";
        case TOK_KW_IF: return "KW_IF";
        case TOK_KW_ELSE: return "KW_ELSE";
        case TOK_KW_WHILE: return "KW_WHILE";
        case TOK_KW_RETURN: return "KW_RETURN";
        case TOK_KW_IMPORT: return "KW_IMPORT";
        case TOK_KW_TRUE: return "KW_TRUE";
        case TOK_KW_FALSE: return "KW_FALSE";
        case TOK_KW_PARALLEL: return "KW_PARALLEL";
        case TOK_KW_MAP: return "KW_MAP";
        case TOK_KW_REDUCE: return "KW_REDUCE";
        case TOK_KW_OWN: return "KW_OWN";
        case TOK_KW_TYPE: return "KW_TYPE";
        case TOK_INT: return "INT";
        case TOK_FLOAT: return "FLOAT";
        case TOK_BOOL: return "BOOL";
        case TOK_STRING: return "STRING";
        case TOK_LPAREN: return "LPAREN";
        case TOK_RPAREN: return "RPAREN";
        case TOK_LBRACE: return "LBRACE";
        case TOK_RBRACE: return "RBRACE";
        case TOK_LBRACKET: return "LBRACKET";
        case TOK_RBRACKET: return "RBRACKET";
        case TOK_COMMA: return "COMMA";
        case TOK_SEMICOLON: return "SEMICOLON";
        case TOK_DOT: return "DOT";
        case TOK_COLON: return "COLON";
        case TOK_PLUS: return "PLUS";
        case TOK_MINUS: return "MINUS";
        case TOK_STAR: return "STAR";
        case TOK_SLASH: return "SLASH";
        case TOK_PERCENT: return "PERCENT";
        case TOK_BANG: return "BANG";
        case TOK_AND_AND: return "AND_AND";
        case TOK_OR_OR: return "OR_OR";
        case TOK_ASSIGN: return "ASSIGN";
        case TOK_BIND: return "BIND";
        case TOK_EQ: return "EQ";
        case TOK_NE: return "NE";
        case TOK_LT: return "LT";
        case TOK_LE: return "LE";
        case TOK_GT: return "GT";
        case TOK_GE: return "GE";
        case TOK_ARROW: return "ARROW";
        case TOK_REDUCE_EXP: return "REDUCE_EXP";
        case TOK_AMP: return "AMP";
        default: return "UNKNOWN";
    }
}

void surge_token_free(SurgeToken *t) {
    if (!t) return;
    if (t->lexeme) {
        free(t->lexeme);
        t->lexeme = NULL;
    }
    t->length = 0;
    t->has_int = false;
    t->has_float = false;
    t->int_value = 0;
    t->float_value = 0.0;
}