//! Парсинг операторов (let, if, while, for и т.д.)

use crate::ast::*;
use crate::directives::take_directive_block;
use crate::error::{ParseCode, ParseDiag};
use crate::expressions::{
    expr_span, forbid_fat_arrow, handle_trailing_expr_tokens, parse_expr, parse_paren_expr,
};
use crate::lexer_api::Stream;
use crate::patterns::parse_pattern;
use crate::sync::is_stmt_sync;
use crate::types::parse_type_node;
use std::collections::HashMap;
use surge_token::{Keyword, SourceId, Span, Token, TokenKind};

/// Парсинг блока операторов
pub fn parse_block(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    file: SourceId,
    directives: &mut Vec<DirectiveBlock>,
) -> Option<Block> {
    let open = match stream.eat(TokenKind::LBrace) {
        Some(tok) => tok,
        None => {
            let tok = stream.peek();
            error(
                diags,
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected '{' to start block",
            );
            return None;
        }
    };

    let mut stmts = Vec::new();
    let mut pending_directives = Vec::new();
    while !stream.at(TokenKind::RBrace) && !stream.is_eof() {
        let mut consumed = false;
        while let Some(mut block) = take_directive_block(stream, diags, file) {
            block.anchor = DirectiveAnchor::Detached;
            directives.push(block);
            pending_directives.push(directives.len() - 1);
            consumed = true;
        }
        if consumed {
            continue;
        }

        match parse_stmt(stream, diags, fn_purity, parallel_checks, file, directives) {
            Some(stmt) => {
                let span = stmt_span(&stmt);
                for idx in pending_directives.drain(..) {
                    directives[idx].anchor = DirectiveAnchor::Statement { span };
                }
                stmts.push(stmt);
            }
            None => {
                pending_directives.clear();
                synchronize_stmt(stream);
            }
        }
    }

    pending_directives.clear();

    let span = if let Some(close) = stream.eat(TokenKind::RBrace) {
        open.span.join(close.span)
    } else {
        error(
            diags,
            ParseCode::UnclosedBrace,
            open.span,
            "Expected '}' to close block",
        );
        open.span
    };

    Some(Block { stmts, span })
}

/// Парсинг оператора
pub fn parse_stmt(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    file: SourceId,
    directives: &mut Vec<DirectiveBlock>,
) -> Option<Stmt> {
    let tok = stream.peek();
    match tok.kind {
        TokenKind::Keyword(Keyword::Let) => {
            parse_let_stmt(stream, diags, fn_purity, parallel_checks, file)
        }
        TokenKind::Keyword(Keyword::Return) => {
            parse_return_stmt(stream, diags, fn_purity, parallel_checks, file)
        }
        TokenKind::Keyword(Keyword::If) => {
            parse_if_stmt(stream, diags, fn_purity, parallel_checks, file, directives)
        }
        TokenKind::Keyword(Keyword::While) => {
            parse_while_stmt(stream, diags, fn_purity, parallel_checks, file, directives)
        }
        TokenKind::Keyword(Keyword::For) => {
            parse_for_stmt(stream, diags, fn_purity, parallel_checks, file, directives)
        }
        TokenKind::Keyword(Keyword::Signal) => {
            parse_signal_stmt(stream, diags, fn_purity, parallel_checks, file, directives)
        }
        TokenKind::Keyword(Keyword::Break) => {
            let token = stream.bump();
            let semi = expect_semicolon(stream, diags, token.span);
            Some(Stmt::Break {
                span: token.span,
                semi,
            })
        }
        TokenKind::Keyword(Keyword::Continue) => {
            let token = stream.bump();
            let semi = expect_semicolon(stream, diags, token.span);
            Some(Stmt::Continue {
                span: token.span,
                semi,
            })
        }
        _ => parse_expr_stmt(stream, diags, fn_purity, parallel_checks, file),
    }
}

/// Парсинг оператора let
fn parse_let_stmt(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    file: SourceId,
) -> Option<Stmt> {
    let let_tok = stream.bump();
    let mutable = stream.eat(TokenKind::Keyword(Keyword::Mut)).is_some();
    let (name, name_span) = expect_ident(stream, diags, "binding name")?;
    let mut span = let_tok.span.join(name_span);

    let ty = parse_optional_type_annotation(stream, diags, name_span, &mut span, file);

    let mut init = None;
    if stream.eat(TokenKind::Eq).is_some() {
        init = parse_expr(stream, diags, fn_purity, parallel_checks);
        if let Some(ref expr) = init {
            forbid_fat_arrow(stream, diags);
            span = span.join(expr_span(expr));
        }
    } else if ty.is_none() {
        // Ни аннотация типа, ни инициализатор не предоставлены – выдаем диагностику
        let diag_span = Span::new(file, name_span.end, name_span.end);
        error(
            diags,
            ParseCode::LetMissingEquals,
            diag_span,
            "Expected type annotation or initializer in let declaration",
        );
    }

    let semi = expect_semicolon(stream, diags, span);
    Some(Stmt::Let {
        name,
        ty,
        init,
        mutable,
        span,
        semi,
    })
}

/// Парсинг let выражения в for-init
pub fn parse_for_init_let(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    let_tok: Token,
    file: SourceId,
) -> Option<Expr> {
    let mutable = stream.eat(TokenKind::Keyword(Keyword::Mut)).is_some();
    let (name, name_span) = expect_ident(stream, diags, "binding name")?;
    let mut span = let_tok.span.join(name_span);

    let ty = parse_optional_type_annotation_no_error(stream, diags, name_span, &mut span, file);

    let mut init = None;
    if stream.eat(TokenKind::Eq).is_some() {
        if let Some(expr) = parse_expr(stream, diags, fn_purity, parallel_checks) {
            forbid_fat_arrow(stream, diags);
            span = span.join(expr_span(&expr));
            init = Some(Box::new(expr));
        }
    } else if ty.is_none() {
        let diag_span = Span::new(file, name_span.end, name_span.end);
        error(
            diags,
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

/// Парсинг оператора return
fn parse_return_stmt(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    _file: SourceId,
) -> Option<Stmt> {
    let ret_tok = stream.bump();
    let expr = if stream.at(TokenKind::Semicolon) {
        None
    } else {
        let expr = parse_expr(stream, diags, fn_purity, parallel_checks);
        if expr.is_some() {
            forbid_fat_arrow(stream, diags);
        }
        expr
    };
    let mut span = ret_tok.span;
    if let Some(ref expr) = expr {
        span = span.join(expr_span(expr));
    }
    let semi = expect_semicolon(stream, diags, span);
    Some(Stmt::Return { expr, span, semi })
}

/// Парсинг оператора if
fn parse_if_stmt(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    file: SourceId,
    directives: &mut Vec<DirectiveBlock>,
) -> Option<Stmt> {
    let if_tok = stream.bump();
    let cond = parse_paren_expr(stream, diags, fn_purity, parallel_checks, "if condition")?;
    let then_block = parse_block(stream, diags, fn_purity, parallel_checks, file, directives)?;
    let mut span = if_tok.span.join(then_block.span);
    let else_b = if stream.eat(TokenKind::Keyword(Keyword::Else)).is_some() {
        if stream.at(TokenKind::Keyword(Keyword::If)) {
            let stmt = parse_if_stmt(stream, diags, fn_purity, parallel_checks, file, directives)?;
            span = span.join(stmt_span(&stmt));
            Some(Box::new(StmtOrBlock::Stmt(stmt)))
        } else if let Some(block) =
            parse_block(stream, diags, fn_purity, parallel_checks, file, directives)
        {
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

/// Парсинг оператора while
fn parse_while_stmt(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    file: SourceId,
    directives: &mut Vec<DirectiveBlock>,
) -> Option<Stmt> {
    let while_tok = stream.bump();
    let cond = parse_paren_expr(stream, diags, fn_purity, parallel_checks, "while condition")?;
    let body = parse_block(stream, diags, fn_purity, parallel_checks, file, directives)?;
    let span = while_tok.span.join(body.span);
    Some(Stmt::While { cond, body, span })
}

/// Парсинг оператора for
fn parse_for_stmt(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    file: SourceId,
    directives: &mut Vec<DirectiveBlock>,
) -> Option<Stmt> {
    let for_tok = stream.bump();
    if stream.at(TokenKind::LParen) {
        parse_for_c(
            stream,
            diags,
            fn_purity,
            parallel_checks,
            for_tok,
            file,
            directives,
        )
    } else {
        parse_for_in(
            stream,
            diags,
            fn_purity,
            parallel_checks,
            for_tok,
            file,
            directives,
        )
    }
}

/// Парсинг for-цикла в стиле C
fn parse_for_c(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    for_tok: Token,
    file: SourceId,
    directives: &mut Vec<DirectiveBlock>,
) -> Option<Stmt> {
    let header_open = stream.bump(); // consume '('

    let init = if stream.at(TokenKind::Semicolon) {
        stream.bump();
        None
    } else if stream.peek().kind == TokenKind::Keyword(Keyword::Let) {
        let let_tok = stream.bump();
        let expr = parse_for_init_let(stream, diags, fn_purity, parallel_checks, let_tok, file);
        if stream.eat(TokenKind::Semicolon).is_none() {
            let tok = stream.peek();
            error(
                diags,
                ParseCode::MissingSemicolon,
                tok.span,
                "Expected ';' after for-init",
            );
            skip_until(stream, TokenKind::Semicolon);
            stream.eat(TokenKind::Semicolon);
        }
        expr
    } else {
        let expr = parse_expr(stream, diags, fn_purity, parallel_checks);
        if expr.is_some() {
            forbid_fat_arrow(stream, diags);
        }
        if stream.eat(TokenKind::Semicolon).is_none() {
            let tok = stream.peek();
            error(
                diags,
                ParseCode::MissingSemicolon,
                tok.span,
                "Expected ';' after for-init",
            );
            skip_until(stream, TokenKind::Semicolon);
            stream.eat(TokenKind::Semicolon);
        }
        expr
    };

    let cond = if stream.at(TokenKind::Semicolon) {
        stream.bump();
        None
    } else {
        let expr = parse_expr(stream, diags, fn_purity, parallel_checks);
        if expr.is_some() {
            forbid_fat_arrow(stream, diags);
        }
        if stream.eat(TokenKind::Semicolon).is_none() {
            let tok = stream.peek();
            error(
                diags,
                ParseCode::MissingSemicolon,
                tok.span,
                "Expected ';' after for-condition",
            );
            skip_until(stream, TokenKind::Semicolon);
            stream.eat(TokenKind::Semicolon);
        }
        expr
    };

    let step = if stream.at(TokenKind::RParen) {
        None
    } else {
        let expr = parse_expr(stream, diags, fn_purity, parallel_checks);
        if expr.is_some() {
            forbid_fat_arrow(stream, diags);
        }
        expr
    };

    if stream.eat(TokenKind::RParen).is_none() {
        error(
            diags,
            ParseCode::UnclosedParen,
            header_open.span,
            "Expected ')' to close for loop header",
        );
    }

    let body = parse_block(stream, diags, fn_purity, parallel_checks, file, directives)?;
    let span = for_tok.span.join(body.span);
    Some(Stmt::ForC {
        init,
        cond,
        step,
        body,
        span,
    })
}

/// Парсинг for-in цикла
fn parse_for_in(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    for_tok: Token,
    file: SourceId,
    directives: &mut Vec<DirectiveBlock>,
) -> Option<Stmt> {
    let pattern = match parse_pattern(stream, diags) {
        Some(pat) => pat,
        None => return None,
    };
    let mut span = for_tok.span.join(pattern.span);

    let ty = if let Some(colon) = stream.eat(TokenKind::Colon) {
        let ty = parse_type_node(stream, diags);
        if let Some(ref ty_node) = ty {
            span = span.join(ty_node.span);
        } else {
            span = span.join(colon.span);
            error(
                diags,
                ParseCode::ForInMissingType,
                colon.span,
                "Expected type after ':' in for-in loop",
            );
        }
        ty
    } else {
        None
    };

    if stream.eat(TokenKind::Keyword(Keyword::In)).is_none() {
        let tok = stream.peek();
        error(
            diags,
            ParseCode::ForInMissingIn,
            tok.span,
            "Expected 'in' in for-in loop",
        );
        return None;
    }

    let iter = match parse_expr(stream, diags, fn_purity, parallel_checks) {
        Some(expr) => {
            forbid_fat_arrow(stream, diags);
            span = span.join(expr_span(&expr));
            expr
        }
        None => {
            let tok = stream.peek();
            error(
                diags,
                ParseCode::ForInMissingExpr,
                tok.span,
                "Expected iterator expression in for-in loop",
            );
            return None;
        }
    };

    let body = parse_block(stream, diags, fn_purity, parallel_checks, file, directives)?;
    span = span.join(body.span);

    Some(Stmt::ForIn {
        pattern,
        ty,
        iter,
        body,
        span,
    })
}

/// Парсинг оператора signal
fn parse_signal_stmt(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    _file: SourceId,
    _directives: &mut Vec<DirectiveBlock>,
) -> Option<Stmt> {
    let sig_tok = stream.bump();
    let (name, _span) = expect_ident(stream, diags, "signal name")?;
    if stream.eat(TokenKind::ColonEq).is_none() {
        let tok = stream.peek();
        error(
            diags,
            ParseCode::SignalMissingAssign,
            tok.span,
            "Expected ':=' after signal name",
        );
    }
    let expr = parse_expr(stream, diags, fn_purity, parallel_checks)?;
    forbid_fat_arrow(stream, diags);
    let span = sig_tok.span.join(expr_span(&expr));
    let semi = expect_semicolon(stream, diags, span);
    Some(Stmt::Signal {
        name,
        expr,
        span,
        semi,
    })
}

/// Парсинг оператора-выражения
fn parse_expr_stmt(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    _file: SourceId,
) -> Option<Stmt> {
    let expr = parse_expr(stream, diags, fn_purity, parallel_checks)?;
    handle_trailing_expr_tokens(stream, diags, &expr);
    let span = expr_span(&expr);
    let semi = match stream.eat(TokenKind::Semicolon) {
        Some(tok) => Some(tok.span),
        None => {
            if !is_stmt_sync(&stream.peek().kind) {
                error(
                    diags,
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

/// Парсинг опциональной аннотации типа для операторов let
fn parse_optional_type_annotation(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    name_span: Span,
    span: &mut Span,
    file: SourceId,
) -> Option<TypeNode> {
    if let Some(colon) = stream.eat(TokenKind::Colon) {
        let ty = parse_type_node(stream, diags);
        if let Some(ref ty) = ty {
            *span = span.join(ty.span);
        } else {
            *span = span.join(colon.span);
        }
        ty
    } else if looks_like_type_start(stream.peek().kind) {
        let next = stream.peek();
        let diag_span = Span::new(file, name_span.end, next.span.start);
        error(
            diags,
            ParseCode::MissingColonInType,
            diag_span,
            "Expected ':' before type annotation",
        );
        None
    } else {
        None
    }
}

/// Парсинг опциональной аннотации типа без выдачи ошибки
fn parse_optional_type_annotation_no_error(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    name_span: Span,
    span: &mut Span,
    file: SourceId,
) -> Option<TypeNode> {
    if let Some(colon) = stream.eat(TokenKind::Colon) {
        let ty = parse_type_node(stream, diags);
        if let Some(ref ty) = ty {
            *span = span.join(ty.span);
        } else {
            *span = span.join(colon.span);
        }
        ty
    } else if looks_like_type_start(stream.peek().kind) {
        let next = stream.peek();
        let diag_span = Span::new(file, name_span.end, next.span.start);
        error(
            diags,
            ParseCode::MissingColonInType,
            diag_span,
            "Expected ':' before type annotation",
        );
        None
    } else {
        None
    }
}

fn looks_like_type_start(kind: TokenKind) -> bool {
    matches!(
        kind,
        TokenKind::Ident
            | TokenKind::Keyword(Keyword::Own)
            | TokenKind::Keyword(Keyword::Nothing)
            | TokenKind::Amp
            | TokenKind::Star
    )
}

/// Вспомогательная функция для ожидания идентификатора
fn expect_ident(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    what: &str,
) -> Option<(String, Span)> {
    let tok = stream.peek();
    if tok.kind == TokenKind::Ident {
        let taken = stream.bump();
        let name = if let Some(slice) = stream.slice(taken.span) {
            slice.to_string()
        } else {
            // Fallback когда исходный текст недоступен
            format!("identifier_{}", taken.span.start)
        };
        return Some((name, taken.span));
    }
    error(
        diags,
        ParseCode::UnexpectedToken,
        tok.span,
        format!("Expected {what}"),
    );
    None
}

/// Вспомогательная функция для ожидания точки с запятой
fn expect_semicolon(stream: &mut Stream, diags: &mut Vec<ParseDiag>, span: Span) -> Option<Span> {
    if let Some(tok) = stream.eat(TokenKind::Semicolon) {
        Some(tok.span)
    } else {
        error(
            diags,
            ParseCode::MissingSemicolon,
            span,
            "Expected ';' after statement",
        );
        None
    }
}

fn skip_until(stream: &mut Stream, kind: TokenKind) {
    while !stream.is_eof() && stream.peek().kind != kind {
        stream.bump();
    }
}

fn synchronize_stmt(stream: &mut Stream) {
    while !stream.is_eof() {
        let kind = stream.peek().kind;
        if is_stmt_sync(&kind) {
            if kind == TokenKind::Semicolon {
                stream.bump();
            }
            break;
        }
        stream.bump();
    }
}

/// Получение span оператора
pub fn stmt_span(stmt: &Stmt) -> Span {
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

/// Вспомогательная функция для добавления ошибки
fn error(diags: &mut Vec<ParseDiag>, code: ParseCode, span: Span, message: impl Into<String>) {
    diags.push(ParseDiag::new(code, span, message));
}
