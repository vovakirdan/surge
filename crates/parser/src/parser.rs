//! Recursive-descent parser that turns tokens into a Surge AST.

use crate::ast::SpanExt;
use crate::ast::*;
use crate::error::{ParseCode, ParseDiag};
use crate::lexer_api::Stream;
use crate::precedence::infix_binding_power;
use crate::sync::{is_stmt_sync, is_top_level_sync};
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
}

impl<'src> Parser<'src> {
    fn new(file: SourceId, tokens: &'src [Token], src: Option<&'src str>) -> Self {
        Self {
            file,
            stream: Stream::new(tokens, src),
            diags: Vec::new(),
        }
    }

    fn parse(mut self) -> ParseResult {
        let module = self.parse_module();
        ParseResult {
            ast: Ast { module },
            diags: self.diags,
        }
    }

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
        Module { items }
    }

    fn parse_item(&mut self) -> Option<Item> {
        let attrs = self.parse_attrs();
        match self.stream.peek().kind {
            TokenKind::Keyword(Keyword::Fn) => self.parse_fn(attrs).map(Item::Fn),
            TokenKind::Keyword(Keyword::Let) => self.parse_let_stmt().map(Item::Let),
            TokenKind::Keyword(Keyword::Type)
            | TokenKind::Keyword(Keyword::Literal)
            | TokenKind::Keyword(Keyword::Alias)
            | TokenKind::Keyword(Keyword::Extern)
            | TokenKind::Keyword(Keyword::Import) => {
                let tok = self.stream.bump();
                self.error(
                    ParseCode::UnexpectedToken,
                    tok.span,
                    "Item kind not implemented yet",
                );
                None
            }
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

    fn parse_attrs(&mut self) -> Vec<Attr> {
        let mut attrs = Vec::new();
        while self.stream.peek().kind == TokenKind::At {
            if let Some(attr) = self.parse_single_attr() {
                attrs.push(attr);
            }
        }
        attrs
    }

    fn parse_single_attr(&mut self) -> Option<Attr> {
        let at_tok = self.stream.bump(); // consume '@'

        // Get attribute name using expect_ident which handles both source and tokens-only modes
        let (attr_name, ident_span) = match self.expect_ident("attribute name") {
            Some(result) => result,
            None => return None, // Error already emitted by expect_ident
        };

        let mut span = at_tok.span.join(ident_span);

        // Determine attribute type - prefer exact name match when available
        let attr_type = if attr_name.starts_with("identifier_") {
            // In parse_tokens mode, we got a fallback name - use heuristics
            let ident_len = (ident_span.end - ident_span.start) as usize;
            let has_paren_after = self.stream.peek().kind == TokenKind::LParen;

            match (ident_len, has_paren_after) {
                (4, false) => Some("pure"),   // "pure" is 4 chars, no parens
                (7, true) => Some("backend"), // "backend" is 7 chars, has parens
                (8, false) => {
                    // Both "overload" and "override" are 8 chars without parens
                    // We can't distinguish them without source text, so we'll default to "overload"
                    // but this is an inherent limitation of the token-only approach
                    Some("overload")
                }
                _ => None, // Unknown attribute
            }
        } else {
            // We have actual source text - use exact match
            Some(attr_name.as_str())
        };

        let attr_display = format!("@{}", attr_name);

        match attr_type {
            Some("pure") => Some(Attr::Pure { span }),
            Some("overload") => Some(Attr::Overload { span }),
            Some("override") => Some(Attr::Override { span }),
            Some("backend") => {
                // Parse @backend("string") format
                if self.stream.peek().kind == TokenKind::LParen {
                    let open_tok = self.stream.bump();
                    span = span.join(open_tok.span);

                    let (value, value_span) = if self.stream.peek().kind == TokenKind::StringLit {
                        let str_tok = self.stream.bump();
                        let text = self
                            .stream
                            .slice(str_tok.span)
                            .unwrap_or("\"\"")
                            .to_string();
                        let has_source_text = !text.is_empty();

                        // Handle case where string text is not available
                        let value = if has_source_text {
                            // Remove quotes from string literal
                            if text.len() >= 2 {
                                text[1..text.len() - 1].to_string()
                            } else {
                                text
                            }
                        } else {
                            // Fallback: guess based on string literal span length
                            let str_len = (str_tok.span.end - str_tok.span.start) as usize;
                            match str_len {
                                5 => "cpu".to_string(), // Both "cpu" and "gpu" + quotes = 5 chars - ambiguous, default to cpu
                                _ => format!("<string@{}>", str_tok.span.start),
                            }
                        };

                        // Validate backend string only when we have real source text
                        if !value.starts_with('<')
                            && has_source_text
                            && value != "cpu"
                            && value != "gpu"
                        {
                            self.error(
                                ParseCode::UnknownAttribute,
                                str_tok.span,
                                &format!("Invalid backend '{}', expected 'cpu' or 'gpu'", value),
                            );
                        }

                        (value, str_tok.span)
                    } else {
                        self.error(
                            ParseCode::UnexpectedToken,
                            self.stream.peek().span,
                            "Expected string literal in @backend attribute",
                        );
                        (String::new(), self.stream.peek().span)
                    };

                    if let Some(close_tok) = self.stream.eat(TokenKind::RParen) {
                        span = span.join(close_tok.span);
                    } else {
                        self.error(
                            ParseCode::UnclosedParen,
                            open_tok.span,
                            "Expected ')' to close @backend attribute",
                        );
                    }

                    Some(Attr::Backend {
                        span,
                        value,
                        value_span,
                    })
                } else {
                    self.error(
                        ParseCode::UnexpectedToken,
                        self.stream.peek().span,
                        "Expected '(' after @backend",
                    );
                    None
                }
            }
            _ => {
                self.error(
                    ParseCode::UnknownAttribute,
                    ident_span,
                    &format!("Unknown attribute {}", attr_display),
                );

                // Recovery: consume possible parentheses after unknown attribute
                if self.stream.peek().kind == TokenKind::LParen {
                    let mut paren_depth = 0;
                    while !self.stream.is_eof() {
                        let tok = self.stream.peek();
                        match tok.kind {
                            TokenKind::LParen => {
                                paren_depth += 1;
                                self.stream.bump();
                            }
                            TokenKind::RParen => {
                                self.stream.bump();
                                paren_depth -= 1;
                                if paren_depth == 0 {
                                    break;
                                }
                            }
                            _ => {
                                self.stream.bump();
                            }
                        }
                    }
                }

                None
            }
        }
    }

    fn parse_fn(&mut self, attrs: Vec<Attr>) -> Option<Func> {
        let fn_tok = self.stream.bump();
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
        let body = match self.parse_block() {
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
            let ty = self.parse_type_node();
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

    fn parse_return_type(&mut self, _fn_span: Span) -> Option<TypeNode> {
        if let Some(arrow_tok) = self.stream.eat(TokenKind::ThinArrow) {
            // Arrow present, type is required
            if let Some(type_node) = self.parse_type_node() {
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

    fn parse_block(&mut self) -> Option<Block> {
        let open = match self.stream.eat(TokenKind::LBrace) {
            Some(tok) => tok,
            None => {
                let tok = self.stream.peek();
                self.error(
                    ParseCode::UnexpectedToken,
                    tok.span,
                    "Expected '{' to start block",
                );
                return None;
            }
        };

        let mut stmts = Vec::new();
        while !self.stream.at(TokenKind::RBrace) && !self.stream.is_eof() {
            match self.parse_stmt() {
                Some(stmt) => stmts.push(stmt),
                None => self.synchronize_stmt(),
            }
        }

        let span = if let Some(close) = self.stream.eat(TokenKind::RBrace) {
            open.span.join(close.span)
        } else {
            self.error(
                ParseCode::UnclosedBrace,
                open.span,
                "Expected '}' to close block",
            );
            open.span
        };

        Some(Block { stmts, span })
    }

    fn parse_stmt(&mut self) -> Option<Stmt> {
        let tok = self.stream.peek();
        match tok.kind {
            TokenKind::Keyword(Keyword::Let) => self.parse_let_stmt(),
            TokenKind::Keyword(Keyword::Return) => self.parse_return_stmt(),
            TokenKind::Keyword(Keyword::If) => self.parse_if_stmt(),
            TokenKind::Keyword(Keyword::While) => self.parse_while_stmt(),
            TokenKind::Keyword(Keyword::For) => self.parse_for_stmt(),
            TokenKind::Keyword(Keyword::Signal) => self.parse_signal_stmt(),
            TokenKind::Keyword(Keyword::Break) => {
                let token = self.stream.bump();
                let semi = self.expect_semicolon(token.span);
                Some(Stmt::Break {
                    span: token.span,
                    semi,
                })
            }
            TokenKind::Keyword(Keyword::Continue) => {
                let token = self.stream.bump();
                let semi = self.expect_semicolon(token.span);
                Some(Stmt::Continue {
                    span: token.span,
                    semi,
                })
            }
            _ => self.parse_expr_stmt(),
        }
    }

    fn parse_let_stmt(&mut self) -> Option<Stmt> {
        let let_tok = self.stream.bump();
        let mutable = self.stream.eat(TokenKind::Keyword(Keyword::Mut)).is_some();
        let (name, name_span) = self.expect_ident("binding name")?;
        let mut span = let_tok.span.join(name_span);

        let ty = if let Some(colon) = self.stream.eat(TokenKind::Colon) {
            let ty = self.parse_type_node();
            if let Some(ref ty) = ty {
                span = span.join(ty.span);
            } else {
                span = span.join(colon.span);
            }
            ty
        } else if self.looks_like_type_start(self.stream.peek().kind) {
            let next = self.stream.peek();
            let diag_span = Span::new(self.file, name_span.end, next.span.start);
            self.error(
                ParseCode::MissingColonInType,
                diag_span,
                "Expected ':' before type annotation",
            );
            None
        } else {
            None
        };

        let mut init = None;
        if self.stream.eat(TokenKind::Eq).is_some() {
            init = self.parse_expr();
            if let Some(ref expr) = init {
                span = span.join(expr_span(expr));
            }
        } else if ty.is_none() {
            // Neither a type annotation nor an initializer provided – issue a diagnostic
            let diag_span = Span::new(self.file, name_span.end, name_span.end);
            self.error(
                ParseCode::LetMissingEquals,
                diag_span,
                "Expected type annotation or initializer in let declaration",
            );
        }

        let semi = self.expect_semicolon(span);
        Some(Stmt::Let {
            name,
            ty,
            init,
            mutable,
            span,
            semi,
        })
    }

    fn parse_for_init_let(&mut self, let_tok: Token) -> Option<Expr> {
        let mutable = self.stream.eat(TokenKind::Keyword(Keyword::Mut)).is_some();
        let (name, name_span) = self.expect_ident("binding name")?;
        let mut span = let_tok.span.join(name_span);

        let ty = if let Some(colon) = self.stream.eat(TokenKind::Colon) {
            let ty = self.parse_type_node();
            if let Some(ref ty) = ty {
                span = span.join(ty.span);
            } else {
                span = span.join(colon.span);
            }
            ty
        } else if self.looks_like_type_start(self.stream.peek().kind) {
            let next = self.stream.peek();
            let diag_span = Span::new(self.file, name_span.end, next.span.start);
            self.error(
                ParseCode::MissingColonInType,
                diag_span,
                "Expected ':' before type annotation",
            );
            None
        } else {
            None
        };

        let mut init = None;
        if self.stream.eat(TokenKind::Eq).is_some() {
            if let Some(expr) = self.parse_expr() {
                span = span.join(expr_span(&expr));
                init = Some(Box::new(expr));
            }
        } else if ty.is_none() {
            let diag_span = Span::new(self.file, name_span.end, name_span.end);
            self.error(
                ParseCode::LetMissingEquals,
                diag_span,
                "Expected type annotation or initializer in let declaration",
            );
        }

        Some(Expr::Let {
            name,
            ty,
            init,
            mutable,
            span,
        })
    }

    fn parse_return_stmt(&mut self) -> Option<Stmt> {
        let ret_tok = self.stream.bump();
        let expr = if self.stream.at(TokenKind::Semicolon) {
            None
        } else {
            self.parse_expr()
        };
        let mut span = ret_tok.span;
        if let Some(ref expr) = expr {
            span = span.join(expr_span(expr));
        }
        let semi = self.expect_semicolon(span);
        Some(Stmt::Return { expr, span, semi })
    }

    fn parse_if_stmt(&mut self) -> Option<Stmt> {
        let if_tok = self.stream.bump();
        let cond = self.parse_paren_expr("if condition")?;
        let then_block = self.parse_block()?;
        let mut span = if_tok.span.join(then_block.span);
        let else_b = if self.stream.eat(TokenKind::Keyword(Keyword::Else)).is_some() {
            if self.stream.at(TokenKind::Keyword(Keyword::If)) {
                let stmt = self.parse_if_stmt()?;
                span = span.join(stmt_span(&stmt));
                Some(Box::new(StmtOrBlock::Stmt(stmt)))
            } else if let Some(block) = self.parse_block() {
                span = span.join(block.span);
                Some(Box::new(StmtOrBlock::Block(block)))
            } else {
                None
            }
        } else {
            None
        };
        Some(Stmt::If {
            cond,
            then_b: then_block,
            else_b,
            span,
        })
    }

    fn parse_while_stmt(&mut self) -> Option<Stmt> {
        let while_tok = self.stream.bump();
        let cond = self.parse_paren_expr("while condition")?;
        let body = self.parse_block()?;
        let span = while_tok.span.join(body.span);
        Some(Stmt::While { cond, body, span })
    }

    fn parse_for_stmt(&mut self) -> Option<Stmt> {
        let for_tok = self.stream.bump();
        let header_open = match self.stream.eat(TokenKind::LParen) {
            Some(tok) => tok,
            None => {
                let tok = self.stream.peek();
                self.error(
                    ParseCode::UnexpectedToken,
                    tok.span,
                    "Expected '(' after 'for'",
                );
                return None;
            }
        };

        let init = if self.stream.at(TokenKind::Semicolon) {
            self.stream.bump();
            None
        } else if self.stream.peek().kind == TokenKind::Keyword(Keyword::Let) {
            let let_tok = self.stream.bump();
            let expr = self.parse_for_init_let(let_tok);
            if self.stream.eat(TokenKind::Semicolon).is_none() {
                let tok = self.stream.peek();
                self.error(
                    ParseCode::MissingSemicolon,
                    tok.span,
                    "Expected ';' after for-init",
                );
                self.skip_until(TokenKind::Semicolon);
                self.stream.eat(TokenKind::Semicolon);
            }
            expr
        } else {
            let expr = self.parse_expr();
            if self.stream.eat(TokenKind::Semicolon).is_none() {
                let tok = self.stream.peek();
                self.error(
                    ParseCode::MissingSemicolon,
                    tok.span,
                    "Expected ';' after for-init",
                );
                self.skip_until(TokenKind::Semicolon);
                self.stream.eat(TokenKind::Semicolon);
            }
            expr
        };

        let cond = if self.stream.at(TokenKind::Semicolon) {
            self.stream.bump();
            None
        } else {
            let expr = self.parse_expr();
            if self.stream.eat(TokenKind::Semicolon).is_none() {
                let tok = self.stream.peek();
                self.error(
                    ParseCode::MissingSemicolon,
                    tok.span,
                    "Expected ';' after for-condition",
                );
                self.skip_until(TokenKind::Semicolon);
                self.stream.eat(TokenKind::Semicolon);
            }
            expr
        };

        let step = if self.stream.at(TokenKind::RParen) {
            None
        } else {
            self.parse_expr()
        };

        if self.stream.eat(TokenKind::RParen).is_none() {
            self.error(
                ParseCode::UnclosedParen,
                header_open.span,
                "Expected ')' to close for loop header",
            );
        }

        let body = self.parse_block()?;
        let span = for_tok.span.join(body.span);
        Some(Stmt::ForC {
            init,
            cond,
            step,
            body,
            span,
        })
    }

    fn parse_signal_stmt(&mut self) -> Option<Stmt> {
        let sig_tok = self.stream.bump();
        let (name, _span) = self.expect_ident("signal name")?;
        if self.stream.eat(TokenKind::ColonEq).is_none() {
            let tok = self.stream.peek();
            self.error(
                ParseCode::SignalMissingAssign,
                tok.span,
                "Expected ':=' after signal name",
            );
        }
        let expr = self.parse_expr()?;
        let span = sig_tok.span.join(expr_span(&expr));
        let semi = self.expect_semicolon(span);
        Some(Stmt::Signal {
            name,
            expr,
            span,
            semi,
        })
    }

    fn parse_expr_stmt(&mut self) -> Option<Stmt> {
        let expr = self.parse_expr()?;
        self.handle_trailing_expr_tokens(&expr);
        let span = expr_span(&expr);
        let semi = match self.stream.eat(TokenKind::Semicolon) {
            Some(tok) => Some(tok.span),
            None => {
                if !is_stmt_sync(&self.stream.peek().kind) {
                    self.error(
                        ParseCode::MissingSemicolon,
                        span,
                        "Expected ';' after expression",
                    );
                }
                None
            }
        };
        Some(Stmt::ExprStmt { expr, span, semi })
    }

    fn parse_paren_expr(&mut self, context: &str) -> Option<Expr> {
        let open = match self.stream.eat(TokenKind::LParen) {
            Some(tok) => tok,
            None => {
                let tok = self.stream.peek();
                self.error(
                    ParseCode::UnexpectedToken,
                    tok.span,
                    format!("Expected '(' for {context}"),
                );
                return None;
            }
        };
        let expr = self.parse_expr()?;
        if let Some(close) = self.stream.eat(TokenKind::RParen) {
            let span = open.span.join(close.span);
            Some(with_span(expr, span))
        } else {
            self.error(
                ParseCode::UnclosedParen,
                open.span,
                "Expected ')' to close expression",
            );
            Some(expr)
        }
    }

    fn parse_expr(&mut self) -> Option<Expr> {
        self.parse_expr_bp(0)
    }

    fn parse_expr_bp(&mut self, min_bp: u8) -> Option<Expr> {
        let mut lhs = self.parse_prefix()?;

        loop {
            match self.stream.peek().kind {
                TokenKind::LParen => {
                    let open = self.stream.bump();
                    let mut args = Vec::new();
                    if !self.stream.at(TokenKind::RParen) {
                        loop {
                            let arg = self.parse_expr()?;
                            args.push(arg);
                            if self.stream.eat(TokenKind::Comma).is_some() {
                                continue;
                            }
                            break;
                        }
                    }
                    let end_span = if let Some(close) = self.stream.eat(TokenKind::RParen) {
                        close.span
                    } else {
                        self.error(
                            ParseCode::UnclosedParen,
                            open.span,
                            "Expected ')' to close call arguments",
                        );
                        open.span
                    };
                    let callee = lhs;
                    let span = expr_span(&callee).join(end_span);
                    lhs = Expr::Call {
                        callee: Box::new(callee),
                        args,
                        span,
                    };
                    continue;
                }
                TokenKind::LBracket => {
                    let open = self.stream.bump();
                    let index = self.parse_expr()?;
                    let end_span = if let Some(close) = self.stream.eat(TokenKind::RBracket) {
                        close.span
                    } else {
                        self.error(
                            ParseCode::UnclosedBracket,
                            open.span,
                            "Expected ']' after index expression",
                        );
                        open.span
                    };
                    let base = lhs;
                    let span = expr_span(&base).join(end_span);
                    lhs = Expr::Index {
                        base: Box::new(base),
                        index: Box::new(index),
                        span,
                    };
                    continue;
                }
                _ => {}
            }

            let tok = self.stream.peek();
            let Some((l_bp, r_bp)) = infix_binding_power(&tok.kind) else {
                if !self.is_expr_terminator(tok.kind) {
                    self.report_unexpected_in_expr(tok, &lhs);
                }
                break;
            };
            if l_bp < min_bp {
                break;
            }

            let op_tok = self.stream.bump();
            if op_tok.kind == TokenKind::Eq {
                // Check if LHS is a valid assignment target
                if !self.is_assignable_expr(&lhs) {
                    self.error(
                        ParseCode::AssignmentWithoutLhs,
                        expr_span(&lhs),
                        "Invalid assignment target",
                    );
                }

                let rhs = self.parse_expr_bp(r_bp)?;
                let span = expr_span(&lhs).join(expr_span(&rhs));
                lhs = Expr::Assign {
                    lhs: Box::new(lhs),
                    rhs: Box::new(rhs),
                    span,
                };
                continue;
            }

            let rhs = self.parse_expr_bp(r_bp)?;
            let span = expr_span(&lhs).join(expr_span(&rhs));
            let op = match op_tok.kind {
                TokenKind::Plus => BinaryOp::Add,
                TokenKind::Minus => BinaryOp::Sub,
                TokenKind::Star => BinaryOp::Mul,
                TokenKind::Slash => BinaryOp::Div,
                TokenKind::Percent => BinaryOp::Mod,
                TokenKind::LAngle => BinaryOp::Lt,
                TokenKind::RAngle => BinaryOp::Gt,
                TokenKind::Le => BinaryOp::Le,
                TokenKind::Ge => BinaryOp::Ge,
                TokenKind::EqEq => BinaryOp::EqEq,
                TokenKind::Ne => BinaryOp::Ne,
                TokenKind::AndAnd => BinaryOp::AndAnd,
                TokenKind::OrOr => BinaryOp::OrOr,
                other => {
                    self.error(
                        ParseCode::UnexpectedToken,
                        op_tok.span,
                        format!("Unsupported operator {:?}", other),
                    );
                    return Some(lhs);
                }
            };
            lhs = Expr::Binary {
                lhs: Box::new(lhs),
                op,
                rhs: Box::new(rhs),
                span,
            };
        }

        Some(lhs)
    }

    fn parse_prefix(&mut self) -> Option<Expr> {
        let tok = self.stream.bump();
        match tok.kind {
            TokenKind::IntLit => Some(Expr::LitInt(
                self.stream.slice(tok.span).unwrap_or("0").to_string(),
                tok.span,
            )),
            TokenKind::FloatLit => Some(Expr::LitFloat(
                self.stream.slice(tok.span).unwrap_or("0.0").to_string(),
                tok.span,
            )),
            TokenKind::StringLit => Some(Expr::LitString(
                self.stream.slice(tok.span).unwrap_or("\"\"").to_string(),
                tok.span,
            )),
            TokenKind::Ident => {
                let name = if let Some(slice) = self.stream.slice(tok.span) {
                    slice.to_string()
                } else {
                    format!("identifier_{}", tok.span.start)
                };
                Some(Expr::Ident(name, tok.span))
            }
            TokenKind::Keyword(Keyword::True) => Some(Expr::Ident("true".into(), tok.span)),
            TokenKind::Keyword(Keyword::False) => Some(Expr::Ident("false".into(), tok.span)),
            TokenKind::Keyword(Keyword::Nothing) => Some(Expr::Ident("nothing".into(), tok.span)),
            TokenKind::Minus => {
                let rhs = self.parse_expr_bp(90)?;
                let span = tok.span.join(expr_span(&rhs));
                Some(Expr::Unary {
                    op: UnaryOp::Neg,
                    rhs: Box::new(rhs),
                    span,
                })
            }
            TokenKind::Plus => {
                let rhs = self.parse_expr_bp(90)?;
                let span = tok.span.join(expr_span(&rhs));
                Some(Expr::Unary {
                    op: UnaryOp::Pos,
                    rhs: Box::new(rhs),
                    span,
                })
            }
            TokenKind::Not => {
                let rhs = self.parse_expr_bp(90)?;
                let span = tok.span.join(expr_span(&rhs));
                Some(Expr::Unary {
                    op: UnaryOp::Not,
                    rhs: Box::new(rhs),
                    span,
                })
            }
            TokenKind::LParen => {
                let inner = self.parse_expr_bp(0)?;
                if let Some(close) = self.stream.eat(TokenKind::RParen) {
                    let span = tok.span.join(close.span);
                    Some(with_span(inner, span))
                } else {
                    self.error(
                        ParseCode::UnclosedParen,
                        tok.span,
                        "Expected ')' to close expression",
                    );
                    Some(inner)
                }
            }
            TokenKind::LBracket => self.parse_array_literal(tok.span),
            TokenKind::RAngle => {
                if let Some(prev) = self.stream.previous_n(1) {
                    if prev.kind == TokenKind::LAngle {
                        self.error(
                            ParseCode::UnexpectedToken,
                            tok.span,
                            "Unexpected token '>' after '<' — operator '<>' is not valid",
                        );
                        return None;
                    }
                }
                self.error(
                    ParseCode::UnexpectedToken,
                    tok.span,
                    "Unexpected token '>' in expression",
                );
                None
            }
            other => {
                self.error(
                    ParseCode::UnexpectedPrimary,
                    tok.span,
                    format!("Unexpected token {:?} in expression", other),
                );
                None
            }
        }
    }

    fn parse_array_literal(&mut self, start: Span) -> Option<Expr> {
        let mut elems = Vec::new();
        if self.stream.at(TokenKind::RBracket) {
            let close = self.stream.bump();
            let span = start.join(close.span);
            return Some(Expr::Array { elems, span });
        }
        loop {
            if self.stream.is_eof() {
                break;
            }
            if self.stream.at(TokenKind::RBracket) {
                break;
            }
            match self.parse_expr() {
                Some(expr) => elems.push(expr),
                None => break,
            }
            if self.stream.eat(TokenKind::Comma).is_some() {
                continue;
            }
            break;
        }
        if let Some(close) = self.stream.eat(TokenKind::RBracket) {
            let span = start.join(close.span);
            Some(Expr::Array { elems, span })
        } else {
            self.error(
                ParseCode::UnclosedBracket,
                start,
                "Expected ']' to close array literal",
            );
            None
        }
    }

    fn parse_type_node(&mut self) -> Option<TypeNode> {
        let mut depth_angle = 0i32;
        let mut depth_bracket = 0i32;
        let mut consumed_any = false;
        let mut start_span = None;
        let mut end_span = None;
        let mut consumed_tokens = Vec::new();

        loop {
            let tok = self.stream.peek();
            if !consumed_any {
                if !self.looks_like_type_start(tok.kind) {
                    self.error(ParseCode::UnexpectedToken, tok.span, "Expected type");
                    return None;
                }
            } else if depth_angle == 0 && depth_bracket == 0 && self.is_type_terminator(tok.kind) {
                break;
            }

            if self.stream.is_eof() {
                break;
            }

            let tok = self.stream.bump();
            consumed_any = true;
            start_span.get_or_insert(tok.span);
            end_span = Some(tok.span);

            // Store token for reconstruction
            consumed_tokens.push(tok);

            match tok.kind {
                TokenKind::LAngle => depth_angle += 1,
                TokenKind::RAngle => depth_angle -= 1,
                TokenKind::LBracket => depth_bracket += 1,
                TokenKind::RBracket => depth_bracket -= 1,
                _ => {}
            }

            if depth_angle < 0 {
                break;
            }
            if depth_bracket < 0 {
                break;
            }

            if depth_angle == 0
                && depth_bracket == 0
                && self.is_type_terminator(self.stream.peek().kind)
            {
                break;
            }
        }

        let Some(start) = start_span else {
            return None;
        };
        let end = end_span.unwrap_or(start);
        let span = start.join(end);

        // First try to get text from source if available, otherwise reconstruct from tokens
        let repr = if let Some(slice) = self.stream.slice(span) {
            slice.to_string()
        } else {
            // Reconstruct type text from consumed tokens
            self.reconstruct_type_from_tokens(&consumed_tokens)
        };

        Some(TypeNode { repr, span })
    }

    /// Check if an expression is a valid assignment target
    fn is_assignable_expr(&self, expr: &Expr) -> bool {
        match expr {
            Expr::Ident(_, _) => true,  // Variables can be assigned
            Expr::Index { .. } => true, // Array/object indexing can be assigned (arr[i] = x)
            _ => false,                 // Literals, function calls, etc. cannot be assigned
        }
    }

    /// Reconstruct type representation from tokens when source text is unavailable
    fn reconstruct_type_from_tokens(&self, tokens: &[surge_token::Token]) -> String {
        let mut result = String::new();

        for tok in tokens {
            let token_repr = match &tok.kind {
                TokenKind::Ident => {
                    if let Some(slice) = self.stream.slice(tok.span) {
                        slice.to_string()
                    } else {
                        // Common type names based on token length heuristics
                        let len = (tok.span.end - tok.span.start) as usize;
                        match len {
                            3 => "int".to_string(),
                            4 => {
                                if result.is_empty() {
                                    "bool".to_string()
                                } else {
                                    "uint".to_string()
                                }
                            }
                            5 => "float".to_string(),
                            6 => "string".to_string(),
                            _ => format!("T{}", len), // Generic fallback
                        }
                    }
                }
                TokenKind::Keyword(kw) => format!("{:?}", kw).to_lowercase(),
                TokenKind::LAngle => "<".to_string(),
                TokenKind::RAngle => ">".to_string(),
                TokenKind::LBracket => "[".to_string(),
                TokenKind::RBracket => "]".to_string(),
                TokenKind::Comma => ", ".to_string(),
                TokenKind::Colon => ":".to_string(),
                TokenKind::Amp => "&".to_string(),
                TokenKind::Star => "*".to_string(),
                TokenKind::IntLit => {
                    if let Some(slice) = self.stream.slice(tok.span) {
                        slice.to_string()
                    } else {
                        "0".to_string()
                    }
                }
                _ => {
                    if let Some(slice) = self.stream.slice(tok.span) {
                        slice.to_string()
                    } else {
                        "".to_string()
                    }
                }
            };

            result.push_str(&token_repr);
        }

        if result.is_empty() {
            "T".to_string() // Ultimate fallback
        } else {
            result
        }
    }

    fn is_type_terminator(&self, kind: TokenKind) -> bool {
        matches!(
            kind,
            TokenKind::Comma
                | TokenKind::RParen
                | TokenKind::Semicolon
                | TokenKind::Eq
                | TokenKind::RBrace
                | TokenKind::LBrace
                | TokenKind::ColonEq
                | TokenKind::Keyword(Keyword::In)
                | TokenKind::Eof
        )
    }

    fn looks_like_type_start(&self, kind: TokenKind) -> bool {
        matches!(
            kind,
            TokenKind::Ident | TokenKind::Keyword(Keyword::Own) | TokenKind::Amp | TokenKind::Star
        )
    }

    fn handle_trailing_expr_tokens(&mut self, expr: &Expr) {
        let next = self.stream.peek();
        if self.is_expr_terminator(next.kind) || matches!(next.kind, TokenKind::Semicolon) {
            return;
        }
        if matches!(expr, Expr::Ident(_, _))
            && matches!(
                next.kind,
                TokenKind::IntLit | TokenKind::FloatLit | TokenKind::Ident
            )
        {
            self.error(
                ParseCode::UnexpectedToken,
                next.span,
                "Expected '=' in assignment",
            );
            self.stream.bump();
            return;
        }
        if !self.is_expr_terminator(next.kind) && !self.stream.is_eof() {
            self.error(
                ParseCode::UnexpectedToken,
                next.span,
                format!("Unexpected token {:?} in expression", next.kind),
            );
            self.stream.bump();
        }
    }

    fn is_expr_terminator(&self, kind: TokenKind) -> bool {
        matches!(
            kind,
            TokenKind::Semicolon
                | TokenKind::Comma
                | TokenKind::RParen
                | TokenKind::RBracket
                | TokenKind::RBrace
                | TokenKind::LBrace
                | TokenKind::Colon
                | TokenKind::FatArrow
                | TokenKind::ThinArrow
                | TokenKind::Keyword(Keyword::Else)
                | TokenKind::Keyword(Keyword::Let)
                | TokenKind::Keyword(Keyword::Return)
                | TokenKind::Keyword(Keyword::If)
                | TokenKind::Keyword(Keyword::While)
                | TokenKind::Keyword(Keyword::For)
                | TokenKind::Keyword(Keyword::Break)
                | TokenKind::Keyword(Keyword::Continue)
                | TokenKind::Keyword(Keyword::Signal)
                | TokenKind::Eof
        )
    }

    fn report_unexpected_in_expr(&mut self, tok: Token, lhs: &Expr) {
        if matches!(lhs, Expr::Ident(_, _))
            && matches!(
                tok.kind,
                TokenKind::IntLit | TokenKind::FloatLit | TokenKind::Ident
            )
        {
            self.error(
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected '=' in assignment",
            );
            self.stream.bump();
            return;
        }
        if tok.kind == TokenKind::RAngle {
            if let Some(prev) = self.stream.previous() {
                if prev.kind == TokenKind::LAngle {
                    self.error(
                        ParseCode::UnexpectedToken,
                        tok.span,
                        "Unexpected token '>' after '<' — operator '<>' is not valid",
                    );
                    return;
                }
            }
        }
        self.error(
            ParseCode::UnexpectedToken,
            tok.span,
            format!("Unexpected token {:?} in expression", tok.kind),
        );
        if !self.is_expr_terminator(tok.kind) && !self.stream.is_eof() {
            self.stream.bump();
        }
    }

    fn expect_ident(&mut self, what: &str) -> Option<(String, Span)> {
        let tok = self.stream.peek();
        if tok.kind == TokenKind::Ident {
            let taken = self.stream.bump();
            let name = if let Some(slice) = self.stream.slice(taken.span) {
                slice.to_string()
            } else {
                // Fallback when source text is not available
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

    fn expect_semicolon(&mut self, span: Span) -> Option<Span> {
        if let Some(tok) = self.stream.eat(TokenKind::Semicolon) {
            Some(tok.span)
        } else {
            self.error(
                ParseCode::MissingSemicolon,
                span,
                "Expected ';' after statement",
            );
            None
        }
    }

    fn skip_until(&mut self, kind: TokenKind) {
        while !self.stream.is_eof() && self.stream.peek().kind != kind {
            self.stream.bump();
        }
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

    fn synchronize_stmt(&mut self) {
        while !self.stream.is_eof() {
            let kind = self.stream.peek().kind;
            if is_stmt_sync(&kind) {
                if kind == TokenKind::Semicolon {
                    self.stream.bump();
                }
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

    fn error(&mut self, code: ParseCode, span: Span, message: impl Into<String>) {
        self.diags.push(ParseDiag::new(code, span, message));
    }
}

fn expr_span(expr: &Expr) -> Span {
    match expr {
        Expr::LitInt(_, span)
        | Expr::LitFloat(_, span)
        | Expr::LitString(_, span)
        | Expr::Ident(_, span)
        | Expr::Call { span, .. }
        | Expr::Index { span, .. }
        | Expr::Array { span, .. }
        | Expr::Unary { span, .. }
        | Expr::Binary { span, .. }
        | Expr::Assign { span, .. }
        | Expr::Let { span, .. }
        | Expr::ParallelMap { span, .. }
        | Expr::ParallelReduce { span, .. } => *span,
    }
}

fn with_span(expr: Expr, span: Span) -> Expr {
    match expr {
        Expr::LitInt(text, _) => Expr::LitInt(text, span),
        Expr::LitFloat(text, _) => Expr::LitFloat(text, span),
        Expr::LitString(text, _) => Expr::LitString(text, span),
        Expr::Ident(text, _) => Expr::Ident(text, span),
        Expr::Call { callee, args, .. } => Expr::Call { callee, args, span },
        Expr::Index { base, index, .. } => Expr::Index { base, index, span },
        Expr::Array { elems, .. } => Expr::Array { elems, span },
        Expr::Unary { op, rhs, .. } => Expr::Unary { op, rhs, span },
        Expr::Binary { lhs, op, rhs, .. } => Expr::Binary { lhs, op, rhs, span },
        Expr::Assign { lhs, rhs, .. } => Expr::Assign { lhs, rhs, span },
        Expr::Let {
            name,
            ty,
            init,
            mutable,
            ..
        } => Expr::Let {
            name,
            ty,
            init,
            mutable,
            span,
        },
        Expr::ParallelMap {
            seq, params, func, ..
        } => Expr::ParallelMap {
            seq,
            params,
            func,
            span,
        },
        Expr::ParallelReduce {
            seq,
            init,
            params,
            func,
            ..
        } => Expr::ParallelReduce {
            seq,
            init,
            params,
            func,
            span,
        },
    }
}

fn stmt_span(stmt: &Stmt) -> Span {
    match stmt {
        Stmt::Let { span, .. }
        | Stmt::While { span, .. }
        | Stmt::ForC { span, .. }
        | Stmt::ForIn { span, .. }
        | Stmt::If { span, .. }
        | Stmt::Return { span, .. }
        | Stmt::ExprStmt { span, .. }
        | Stmt::Signal { span, .. }
        | Stmt::Break { span, .. }
        | Stmt::Continue { span, .. } => *span,
    }
}
