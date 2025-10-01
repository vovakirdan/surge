//! Recursive-descent parser that turns tokens into a Surge AST.

use crate::ast::SpanExt;
use crate::ast::*;
use crate::attributes::parse_attrs;
use crate::error::{ParseCode, ParseDiag};
use crate::lexer_api::Stream;
use crate::statements::{parse_block, parse_stmt};
use crate::sync::is_top_level_sync;
use crate::types::parse_type_node;
use std::collections::HashMap;
use surge_token::{Keyword, SourceId, Span, Token, TokenKind};

/// Parsing outcome containing AST and diagnostics.
pub struct ParseResult {
    pub ast: Ast,
    pub diags: Vec<ParseDiag>,
}

/// Parse an already tokenised input into an AST.
pub fn parse_tokens(file: SourceId, tokens: &[Token]) -> ParseResult {
    Parser::new(file, tokens, None).parse()
}

/// Lex and parse the provided source.
pub fn parse_source(file: SourceId, src: &str) -> (ParseResult, surge_lexer::LexResult) {
    let lex_res = surge_lexer::lex(src, file, &surge_lexer::LexOptions::default());
    let parse_res = Parser::new(file, &lex_res.tokens, Some(src)).parse();
    (parse_res, lex_res)
}

/// Lex with custom options and parse the provided source.
pub fn parse_source_with_options(
    file: SourceId,
    src: &str,
    lex_opts: &surge_lexer::LexOptions,
) -> (ParseResult, surge_lexer::LexResult) {
    let lex_res = surge_lexer::lex(src, file, lex_opts);
    let parse_res = Parser::new(file, &lex_res.tokens, Some(src)).parse();
    (parse_res, lex_res)
}

struct Parser<'src> {
    file: SourceId,
    stream: Stream<'src>,
    diags: Vec<ParseDiag>,
    fn_purity: HashMap<String, bool>,
    parallel_checks: Vec<(Span, String)>,
}

impl<'src> Parser<'src> {
    // ========================================
    // PARSER INITIALIZATION AND COORDINATION
    // ========================================

    fn new(file: SourceId, tokens: &'src [Token], src: Option<&'src str>) -> Self {
        Self {
            file,
            stream: Stream::new(tokens, src),
            diags: Vec::new(),
            fn_purity: HashMap::new(),
            parallel_checks: Vec::new(),
        }
    }

    fn parse(mut self) -> ParseResult {
        let module = self.parse_module();
        ParseResult {
            ast: Ast { module },
            diags: self.diags,
        }
    }

    // ========================================
    // TOP-LEVEL PARSING (MODULE AND ITEMS)
    // ========================================

    fn parse_module(&mut self) -> Module {
        let mut items = Vec::new();
        while !self.stream.is_eof() {
            if self.stream.at(TokenKind::Eof) {
                break;
            }
            match self.parse_item() {
                Some(item) => items.push(item),
                None => self.synchronize_top_level(),
            }
        }
        self.finalize_parallel_checks();
        Module { items }
    }

    fn parse_item(&mut self) -> Option<Item> {
        let attrs = parse_attrs(&mut self.stream, &mut self.diags);
        if self.stream.at_keyword(Keyword::Fn) {
            return self.parse_fn(attrs).map(Item::Fn);
        }

        if self.stream.at_keyword(Keyword::Let) {
            return match parse_stmt(
                &mut self.stream,
                &mut self.diags,
                &mut self.fn_purity,
                &mut self.parallel_checks,
                self.file,
            ) {
                Some(stmt @ Stmt::Let { .. }) => Some(Item::Let(stmt)),
                Some(_) => None,
                None => None,
            };
        }

        if let Some(keyword) = self.stream.peek_keyword() {
            match keyword {
                Keyword::Type
                | Keyword::Literal
                | Keyword::Alias
                | Keyword::Extern
                | Keyword::Import => {
                    let tok = self.stream.bump();
                    self.error(
                        ParseCode::UnexpectedToken,
                        tok.span,
                        "Item kind not implemented yet",
                    );
                    return None;
                }
                _ => {}
            }
        }

        match self.stream.peek().kind {
            TokenKind::Eof => None,
            unexpected => {
                let tok = self.stream.bump();
                self.error(
                    ParseCode::UnexpectedToken,
                    tok.span,
                    format!("Unexpected token {:?} at top level", unexpected),
                );
                None
            }
        }
    }

    // ========================================
    // FUNCTION PARSING
    // ========================================

    fn parse_fn(&mut self, attrs: Vec<Attr>) -> Option<Func> {
        let Some(fn_tok) = self.stream.eat_keyword(Keyword::Fn) else {
            let tok = self.stream.bump();
            self.error(
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected 'fn' keyword",
            );
            return None;
        };
        let (name, name_span) = self.expect_ident("function name")?;
        let params = self.parse_param_list();
        let ret = self.parse_return_type(fn_tok.span);

        let mut sig_span = fn_tok.span.join(name_span);
        if let Some(last) = params.last() {
            sig_span = sig_span.join(last.span);
        }
        if let Some(ref ty) = ret {
            sig_span = sig_span.join(ty.span);
        }

        let sig = FuncSig {
            name,
            params,
            ret,
            span: sig_span,
            attrs,
        };
        self.record_function_purity(&sig);
        let body = match parse_block(
            &mut self.stream,
            &mut self.diags,
            &mut self.fn_purity,
            &mut self.parallel_checks,
            self.file,
        ) {
            Some(block) => Some(block),
            None => None,
        };
        let mut span = sig.span;
        if let Some(ref block) = body {
            span = span.join(block.span);
        }
        Some(Func { sig, body, span })
    }

    fn parse_param_list(&mut self) -> Vec<Param> {
        let mut params = Vec::new();
        let open = match self.stream.eat(TokenKind::LParen) {
            Some(tok) => tok,
            None => {
                let tok = self.stream.peek();
                self.error(
                    ParseCode::UnexpectedToken,
                    tok.span,
                    "Expected '(' after function name",
                );
                return params;
            }
        };
        if self.stream.at(TokenKind::RParen) {
            self.stream.bump();
            return params;
        }
        loop {
            match self.parse_param() {
                Some(param) => params.push(param),
                None => self.recover_in_param_list(),
            }
            if self.stream.eat(TokenKind::Comma).is_some() {
                if self.stream.at(TokenKind::RParen) {
                    break;
                }
                continue;
            }
            break;
        }
        if let Some(close) = self.stream.eat(TokenKind::RParen) {
            if let Some(last) = params.last_mut() {
                last.span = last.span.join(close.span);
            }
        } else {
            self.error(
                ParseCode::UnclosedParen,
                open.span,
                "Expected ')' to close parameter list",
            );
        }
        params
    }

    fn parse_param(&mut self) -> Option<Param> {
        let (name, name_span) = self.expect_ident("parameter name")?;
        let mut span = name_span;
        let ty = if let Some(colon) = self.stream.eat(TokenKind::Colon) {
            let ty = parse_type_node(&mut self.stream, &mut self.diags);
            if let Some(ref ty) = ty {
                span = span.join(ty.span);
            } else {
                span = span.join(colon.span);
            }
            ty
        } else {
            self.error(
                ParseCode::MissingColonInType,
                Span::new(self.file, name_span.end, name_span.end),
                "Expected ':' before parameter type",
            );
            None
        };
        Some(Param { name, ty, span })
    }

    fn record_function_purity(&mut self, sig: &FuncSig) {
        let is_pure = sig
            .attrs
            .iter()
            .any(|attr| matches!(attr, Attr::Pure { .. }));
        let entry = self.fn_purity.entry(sig.name.clone()).or_insert(false);
        if is_pure {
            *entry = true;
        }
    }

    fn parse_return_type(&mut self, _fn_span: Span) -> Option<TypeNode> {
        if let Some(arrow_tok) = self.stream.eat(TokenKind::ThinArrow) {
            // Arrow present, type is required
            if let Some(type_node) = parse_type_node(&mut self.stream, &mut self.diags) {
                Some(type_node)
            } else {
                // Arrow present but no valid type follows
                self.error(
                    ParseCode::ExpectedTypeAfterArrow,
                    arrow_tok.span,
                    "Expected type after '->'",
                );
                None
            }
        } else {
            // No arrow means no return type (optional)
            None
        }
    }

    // ========================================
    // UTILITY FUNCTIONS (error handling, recovery, etc.)
    // ========================================

    fn expect_ident(&mut self, what: &str) -> Option<(String, Span)> {
        let tok = self.stream.peek();
        if tok.kind == TokenKind::Ident {
            let taken = self.stream.bump();
            let name = if let Some(slice) = self.stream.slice(taken.span) {
                slice.to_string()
            } else {
                // Fallback when source text is unavailable
                format!("identifier_{}", taken.span.start)
            };
            return Some((name, taken.span));
        }
        self.error(
            ParseCode::UnexpectedToken,
            tok.span,
            format!("Expected {what}"),
        );
        None
    }

    fn recover_in_param_list(&mut self) {
        while !self.stream.is_eof() {
            let tok = self.stream.peek();
            if matches!(tok.kind, TokenKind::Comma | TokenKind::RParen) {
                break;
            }
            self.stream.bump();
        }
    }

    fn synchronize_top_level(&mut self) {
        while !self.stream.is_eof() {
            let kind = self.stream.peek().kind;
            if is_top_level_sync(&kind) {
                if kind == TokenKind::Semicolon {
                    self.stream.bump();
                }
                break;
            }
            self.stream.bump();
        }
    }

    fn finalize_parallel_checks(&mut self) {
        let pending = std::mem::take(&mut self.parallel_checks);
        for (span, name) in pending {
            match self.fn_purity.get(&name).copied() {
                Some(true) => {}
                _ => {
                    self.error(
                        ParseCode::ParallelFuncNotPure,
                        span,
                        format!(
                            "Function '{}' used in parallel context must be declared @pure",
                            name
                        ),
                    );
                }
            }
        }
    }

    fn error(&mut self, code: ParseCode, span: Span, message: impl Into<String>) {
        self.diags.push(ParseDiag::new(code, span, message));
    }
}
