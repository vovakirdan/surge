//! Парсинг выражений (литералы, операторы, вызовы функций и т.д.)

use crate::ast::*;
use crate::error::{ParseCode, ParseDiag};
use crate::lexer_api::Stream;
use crate::precedence::infix_binding_power;
use std::collections::HashMap;
use surge_token::{Keyword, Span, Token, TokenKind};

const TERNARY_BP: u8 = 25;

/// Парсинг выражения в скобках
pub fn parse_paren_expr(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    context: &str,
) -> Option<Expr> {
    let open = match stream.eat(TokenKind::LParen) {
        Some(tok) => tok,
        None => {
            let tok = stream.peek();
            error(
                diags,
                ParseCode::UnexpectedToken,
                tok.span,
                format!("Expected '(' for {context}"),
            );
            return None;
        }
    };
    let expr = parse_expr(stream, diags, fn_purity, parallel_checks)?;
    if let Some(close) = stream.eat(TokenKind::RParen) {
        let span = open.span.join(close.span);
        Some(with_span(expr, span))
    } else {
        error(
            diags,
            ParseCode::UnclosedParen,
            open.span,
            "Expected ')' to close expression",
        );
        Some(expr)
    }
}

/// Основная функция парсинга выражений
pub fn parse_expr(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
) -> Option<Expr> {
    parse_expr_bp(stream, diags, fn_purity, parallel_checks, 0)
}

/// Парсинг выражения с силой связывания для обработки приоритета
fn parse_expr_bp(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    min_bp: u8,
) -> Option<Expr> {
    let mut lhs = parse_prefix(stream, diags, fn_purity, parallel_checks)?;

    loop {
        // Обрабатываем тернарный оператор
        if stream.peek().kind == TokenKind::Question {
            if TERNARY_BP < min_bp {
                break;
            }

            let cond = lhs;
            let question_span = stream.bump().span;
            let then_expr =
                match parse_expr_bp(stream, diags, fn_purity, parallel_checks, TERNARY_BP + 1) {
                    Some(expr) => expr,
                    None => return None,
                };

            if stream.eat(TokenKind::Colon).is_none() {
                let tok = stream.peek();
                error(
                    diags,
                    ParseCode::UnexpectedToken,
                    tok.span,
                    "Expected ':' in ternary expression",
                );
                return Some(cond);
            }

            let else_expr =
                match parse_expr_bp(stream, diags, fn_purity, parallel_checks, TERNARY_BP) {
                    Some(expr) => expr,
                    None => return None,
                };

            let span = expr_span(&cond).join(expr_span(&else_expr));
            lhs = Expr::Ternary {
                cond: Box::new(cond),
                then_branch: Box::new(then_expr),
                else_branch: Box::new(else_expr),
                span: question_span.join(span),
            };
            continue;
        }

        match stream.peek().kind {
            TokenKind::LParen => {
                let open = stream.bump();
                let mut args = Vec::new();
                if !stream.at(TokenKind::RParen) {
                    loop {
                        let arg = parse_expr(stream, diags, fn_purity, parallel_checks)?;
                        args.push(arg);
                        if stream.eat(TokenKind::Comma).is_some() {
                            continue;
                        }
                        break;
                    }
                }
                let end_span = if let Some(close) = stream.eat(TokenKind::RParen) {
                    close.span
                } else {
                    error(
                        diags,
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
                let open = stream.bump();
                let index = parse_expr(stream, diags, fn_purity, parallel_checks)?;
                let end_span = if let Some(close) = stream.eat(TokenKind::RBracket) {
                    close.span
                } else {
                    error(
                        diags,
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

        let tok = stream.peek();
        let Some((l_bp, r_bp)) = infix_binding_power(&tok.kind) else {
            if !is_expr_terminator(tok.kind) {
                report_unexpected_in_expr(stream, diags, tok, &lhs);
            }
            break;
        };
        if l_bp < min_bp {
            break;
        }

        let op_tok = stream.bump();
        if let Some(assign_op) = assign_op_from_token(op_tok.kind) {
            // Проверяем является ли левая часть допустимой целью для присваивания
            if !is_assignable_expr(&lhs) {
                error(
                    diags,
                    ParseCode::AssignmentWithoutLhs,
                    expr_span(&lhs),
                    "Invalid assignment target",
                );
            }

            let rhs = parse_expr_bp(stream, diags, fn_purity, parallel_checks, r_bp)?;
            let span = expr_span(&lhs).join(expr_span(&rhs));
            lhs = Expr::Assign {
                lhs: Box::new(lhs),
                rhs: Box::new(rhs),
                op: assign_op,
                span,
            };
            continue;
        }

        let rhs = parse_expr_bp(stream, diags, fn_purity, parallel_checks, r_bp)?;
        let span = expr_span(&lhs).join(expr_span(&rhs));
        let op = match op_tok.kind {
            TokenKind::Plus => BinaryOp::Add,
            TokenKind::Minus => BinaryOp::Sub,
            TokenKind::Star => BinaryOp::Mul,
            TokenKind::Slash => BinaryOp::Div,
            TokenKind::Percent => BinaryOp::Mod,
            TokenKind::Shl => BinaryOp::Shl,
            TokenKind::Shr => BinaryOp::Shr,
            TokenKind::Amp => BinaryOp::BitAnd,
            TokenKind::Caret => BinaryOp::BitXor,
            TokenKind::Pipe => BinaryOp::BitOr,
            TokenKind::DotDot => BinaryOp::Range,
            TokenKind::DotDotEq => BinaryOp::RangeInclusive,
            TokenKind::LAngle => BinaryOp::Lt,
            TokenKind::RAngle => BinaryOp::Gt,
            TokenKind::Le => BinaryOp::Le,
            TokenKind::Ge => BinaryOp::Ge,
            TokenKind::EqEq => BinaryOp::EqEq,
            TokenKind::Ne => BinaryOp::Ne,
            TokenKind::Keyword(Keyword::Is) => BinaryOp::Is,
            TokenKind::AndAnd => BinaryOp::AndAnd,
            TokenKind::OrOr => BinaryOp::OrOr,
            TokenKind::QuestionQuestion => BinaryOp::NullCoalesce,
            other => {
                error(
                    diags,
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

/// Парсинг префиксных выражений (литералы, идентификаторы, унарные операторы)
fn parse_prefix(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
) -> Option<Expr> {
    let tok = stream.bump();
    match tok.kind {
        TokenKind::IntLit => Some(Expr::LitInt(
            stream.slice(tok.span).unwrap_or("0").to_string(),
            tok.span,
        )),
        TokenKind::FloatLit => Some(Expr::LitFloat(
            stream.slice(tok.span).unwrap_or("0.0").to_string(),
            tok.span,
        )),
        TokenKind::StringLit => Some(Expr::LitString(
            stream.slice(tok.span).unwrap_or("\"\"").to_string(),
            tok.span,
        )),
        TokenKind::Ident => {
            let name = if let Some(slice) = stream.slice(tok.span) {
                slice.to_string()
            } else {
                format!("identifier_{}", tok.span.start)
            };
            Some(Expr::Ident(name, tok.span))
        }
        TokenKind::Keyword(Keyword::Compare) => {
            parse_compare_expr(stream, diags, fn_purity, parallel_checks, tok)
        }
        TokenKind::Keyword(Keyword::Parallel) => {
            parse_parallel_expr(stream, diags, fn_purity, parallel_checks, tok)
        }
        TokenKind::Keyword(Keyword::True) => Some(Expr::Ident("true".into(), tok.span)),
        TokenKind::Keyword(Keyword::False) => Some(Expr::Ident("false".into(), tok.span)),
        TokenKind::Keyword(Keyword::Nothing) => Some(Expr::Ident("nothing".into(), tok.span)),
        TokenKind::Minus => {
            let rhs = parse_expr_bp(stream, diags, fn_purity, parallel_checks, 90)?;
            let span = tok.span.join(expr_span(&rhs));
            Some(Expr::Unary {
                op: UnaryOp::Neg,
                rhs: Box::new(rhs),
                span,
            })
        }
        TokenKind::Plus => {
            let rhs = parse_expr_bp(stream, diags, fn_purity, parallel_checks, 90)?;
            let span = tok.span.join(expr_span(&rhs));
            Some(Expr::Unary {
                op: UnaryOp::Pos,
                rhs: Box::new(rhs),
                span,
            })
        }
        TokenKind::Not => {
            let rhs = parse_expr_bp(stream, diags, fn_purity, parallel_checks, 90)?;
            let span = tok.span.join(expr_span(&rhs));
            Some(Expr::Unary {
                op: UnaryOp::Not,
                rhs: Box::new(rhs),
                span,
            })
        }
        TokenKind::LParen => {
            let inner = parse_expr_bp(stream, diags, fn_purity, parallel_checks, 0)?;
            if let Some(close) = stream.eat(TokenKind::RParen) {
                let span = tok.span.join(close.span);
                Some(with_span(inner, span))
            } else {
                error(
                    diags,
                    ParseCode::UnclosedParen,
                    tok.span,
                    "Expected ')' to close expression",
                );
                Some(inner)
            }
        }
        TokenKind::LBracket => {
            parse_array_literal(stream, diags, fn_purity, parallel_checks, tok.span)
        }
        TokenKind::RAngle => {
            if let Some(prev) = stream.previous_n(1) {
                if prev.kind == TokenKind::LAngle {
                    error(
                        diags,
                        ParseCode::UnexpectedToken,
                        tok.span,
                        "Unexpected token '>' after '<' — operator '<>' is not valid",
                    );
                    return None;
                }
            }
            error(
                diags,
                ParseCode::UnexpectedToken,
                tok.span,
                "Unexpected token '>' in expression",
            );
            None
        }
        other => {
            error(
                diags,
                ParseCode::UnexpectedPrimary,
                tok.span,
                format!("Unexpected token {:?} in expression", other),
            );
            None
        }
    }
}

/// Парсинг литерала массива
fn parse_array_literal(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    start: Span,
) -> Option<Expr> {
    let mut elems = Vec::new();
    if stream.at(TokenKind::RBracket) {
        let close = stream.bump();
        let span = start.join(close.span);
        return Some(Expr::Array { elems, span });
    }
    loop {
        if stream.is_eof() {
            break;
        }
        if stream.at(TokenKind::RBracket) {
            break;
        }
        match parse_expr(stream, diags, fn_purity, parallel_checks) {
            Some(expr) => elems.push(expr),
            None => break,
        }
        if stream.eat(TokenKind::Comma).is_some() {
            continue;
        }
        break;
    }
    if let Some(close) = stream.eat(TokenKind::RBracket) {
        let span = start.join(close.span);
        Some(Expr::Array { elems, span })
    } else {
        error(
            diags,
            ParseCode::UnclosedBracket,
            start,
            "Expected ']' to close array literal",
        );
        None
    }
}

/// Парсинг выражения compare
fn parse_compare_expr(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    compare_tok: Token,
) -> Option<Expr> {
    let scrutinee = parse_expr(stream, diags, fn_purity, parallel_checks)?;
    let _brace_open = match stream.eat(TokenKind::LBrace) {
        Some(tok) => tok,
        None => {
            let tok = stream.peek();
            error(
                diags,
                ParseCode::CompareMissingBrace,
                tok.span,
                "Expected '{' to start compare arms",
            );
            return None;
        }
    };

    let mut arms = Vec::new();
    let mut saw_finally = false;

    while !stream.is_eof() && !stream.at(TokenKind::RBrace) {
        let pattern = match parse_pattern(stream, diags) {
            Some(pat) => {
                if matches!(pat.kind, PatternKind::Finally) {
                    if saw_finally {
                        error(
                            diags,
                            ParseCode::UnexpectedToken,
                            pat.span,
                            "Duplicate 'finally' arm in compare expression",
                        );
                    }
                    saw_finally = true;
                }
                pat
            }
            None => {
                recover_compare_arm(stream);
                continue;
            }
        };

        let guard = if stream.eat(TokenKind::Keyword(Keyword::If)).is_some() {
            match parse_expr(stream, diags, fn_purity, parallel_checks) {
                Some(expr) => Some(expr),
                None => {
                    recover_compare_arm(stream);
                    continue;
                }
            }
        } else {
            None
        };

        if stream.eat(TokenKind::FatArrow).is_none() {
            let tok = stream.peek();
            error(
                diags,
                ParseCode::CompareMissingArrow,
                tok.span,
                "Expected '=>' in compare arm",
            );
            recover_compare_arm(stream);
            continue;
        }

        let expr = match parse_expr(stream, diags, fn_purity, parallel_checks) {
            Some(expr) => expr,
            None => {
                let tok = stream.peek();
                error(
                    diags,
                    ParseCode::CompareMissingExpr,
                    tok.span,
                    "Expected expression after '=>' in compare arm",
                );
                recover_compare_arm(stream);
                continue;
            }
        };

        let mut arm_span = pattern.span;
        if let Some(ref guard_expr) = guard {
            arm_span = arm_span.join(expr_span(guard_expr));
        }
        arm_span = arm_span.join(expr_span(&expr));

        arms.push(CompareArm {
            pattern,
            guard,
            expr,
            span: arm_span,
        });

        if stream.eat(TokenKind::Semicolon).is_some() {
            continue;
        }
        if stream.eat(TokenKind::Comma).is_some() {
            continue;
        }
    }

    let close = match stream.eat(TokenKind::RBrace) {
        Some(tok) => tok,
        None => {
            let tok = stream.peek();
            error(
                diags,
                ParseCode::CompareMissingBrace,
                tok.span,
                "Expected '}' to close compare expression",
            );
            return None;
        }
    };

    let span = compare_tok.span.join(close.span);
    Some(Expr::Compare {
        scrutinee: Box::new(scrutinee),
        arms,
        span,
    })
}

/// Парсинг шаблона для выражений compare
fn parse_pattern(stream: &mut Stream, diags: &mut Vec<ParseDiag>) -> Option<Pattern> {
    let tok = stream.bump();
    match tok.kind {
        TokenKind::Keyword(Keyword::Finally) => Some(Pattern {
            kind: PatternKind::Finally,
            span: tok.span,
        }),
        TokenKind::Keyword(Keyword::Nothing) => Some(Pattern {
            kind: PatternKind::Nothing,
            span: tok.span,
        }),
        TokenKind::Keyword(Keyword::True) | TokenKind::Keyword(Keyword::False) => {
            let default = if matches!(tok.kind, TokenKind::Keyword(Keyword::True)) {
                "true"
            } else {
                "false"
            };
            let text = stream.slice(tok.span).unwrap_or(default).to_string();
            Some(Pattern {
                kind: PatternKind::Literal(Expr::Ident(text, tok.span)),
                span: tok.span,
            })
        }
        TokenKind::IntLit => {
            let text = stream.slice(tok.span).unwrap_or("0").to_string();
            Some(Pattern {
                kind: PatternKind::Literal(Expr::LitInt(text, tok.span)),
                span: tok.span,
            })
        }
        TokenKind::FloatLit => {
            let text = stream.slice(tok.span).unwrap_or("0.0").to_string();
            Some(Pattern {
                kind: PatternKind::Literal(Expr::LitFloat(text, tok.span)),
                span: tok.span,
            })
        }
        TokenKind::StringLit => {
            let text = stream.slice(tok.span).unwrap_or("\"\"").to_string();
            Some(Pattern {
                kind: PatternKind::Literal(Expr::LitString(text, tok.span)),
                span: tok.span,
            })
        }
        TokenKind::Ident => {
            let name = if let Some(slice) = stream.slice(tok.span) {
                slice.to_string()
            } else {
                format!("ident_{}", tok.span.start)
            };

            if stream.at(TokenKind::LParen) {
                let open = stream.bump();
                let mut args = Vec::new();
                while !stream.is_eof() && !stream.at(TokenKind::RParen) {
                    match parse_pattern(stream, diags) {
                        Some(arg) => args.push(arg),
                        None => {
                            recover_pattern_args(stream);
                            break;
                        }
                    }
                    if stream.eat(TokenKind::Comma).is_some() {
                        continue;
                    }
                    break;
                }
                let close = match stream.eat(TokenKind::RParen) {
                    Some(tok) => tok,
                    None => {
                        error(
                            diags,
                            ParseCode::UnclosedParen,
                            open.span,
                            "Expected ')' after tag pattern",
                        );
                        open
                    }
                };
                let span = tok.span.join(close.span);
                Some(Pattern {
                    kind: PatternKind::Tag { name, args },
                    span,
                })
            } else {
                Some(Pattern {
                    kind: PatternKind::Binding(name),
                    span: tok.span,
                })
            }
        }
        _ => {
            error(
                diags,
                ParseCode::UnexpectedPrimary,
                tok.span,
                "Unexpected token in compare pattern",
            );
            None
        }
    }
}

/// Парсинг выражений parallel (map/reduce)
fn parse_parallel_expr(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    parallel_tok: Token,
) -> Option<Expr> {
    let mode_tok = stream.bump();
    let mode = match mode_tok.kind {
        TokenKind::Ident => {
            let text = stream.slice(mode_tok.span).unwrap_or("").to_string();
            if text == "map" {
                Some(true)
            } else if text == "reduce" {
                Some(false)
            } else {
                None
            }
        }
        _ => None,
    };

    match mode {
        Some(true) => parse_parallel_map(stream, diags, fn_purity, parallel_checks, parallel_tok),
        Some(false) => {
            parse_parallel_reduce(stream, diags, fn_purity, parallel_checks, parallel_tok)
        }
        None => {
            error(
                diags,
                ParseCode::UnexpectedToken,
                mode_tok.span,
                "Expected 'map' or 'reduce' after 'parallel'",
            );
            None
        }
    }
}

/// Парсинг выражения parallel map
fn parse_parallel_map(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    parallel_tok: Token,
) -> Option<Expr> {
    let seq = parse_expr(stream, diags, fn_purity, parallel_checks)?;

    if stream.eat(TokenKind::Keyword(Keyword::With)).is_none() {
        let tok = stream.peek();
        error(
            diags,
            ParseCode::ParallelMissingWith,
            tok.span,
            "Expected 'with' in parallel map expression",
        );
    }

    let args = parse_parallel_arg_list(stream, diags, fn_purity, parallel_checks);

    if stream.eat(TokenKind::FatArrow).is_none() {
        let tok = stream.peek();
        error(
            diags,
            ParseCode::ParallelMissingFatArrow,
            tok.span,
            "Expected '=>' in parallel map expression",
        );
    }

    let func = match parse_expr(stream, diags, fn_purity, parallel_checks) {
        Some(expr) => expr,
        None => {
            let tok = stream.peek();
            error(
                diags,
                ParseCode::ParallelBadHeader,
                tok.span,
                "Expected mapping expression after '=>'",
            );
            return None;
        }
    };

    let span = parallel_tok.span.join(expr_span(&func));
    register_parallel_func(parallel_checks, diags, &func);
    Some(Expr::ParallelMap {
        seq: Box::new(seq),
        args,
        func: Box::new(func),
        span,
    })
}

/// Парсинг выражения parallel reduce
fn parse_parallel_reduce(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
    parallel_tok: Token,
) -> Option<Expr> {
    let seq = parse_expr(stream, diags, fn_purity, parallel_checks)?;

    if stream.eat(TokenKind::Keyword(Keyword::With)).is_none() {
        let tok = stream.peek();
        error(
            diags,
            ParseCode::ParallelMissingWith,
            tok.span,
            "Expected 'with' in parallel reduce expression",
        );
    }

    let init = match parse_expr(stream, diags, fn_purity, parallel_checks) {
        Some(expr) => expr,
        None => {
            let tok = stream.peek();
            error(
                diags,
                ParseCode::ParallelBadHeader,
                tok.span,
                "Expected initializer expression after 'with'",
            );
            return None;
        }
    };

    if stream.eat(TokenKind::Comma).is_none() {
        let tok = stream.peek();
        error(
            diags,
            ParseCode::ParallelBadHeader,
            tok.span,
            "Expected ',' between initializer and argument list",
        );
    }

    let args = parse_parallel_arg_list(stream, diags, fn_purity, parallel_checks);

    if stream.eat(TokenKind::FatArrow).is_none() {
        let tok = stream.peek();
        error(
            diags,
            ParseCode::ParallelMissingFatArrow,
            tok.span,
            "Expected '=>' in parallel reduce expression",
        );
    }

    let func = match parse_expr(stream, diags, fn_purity, parallel_checks) {
        Some(expr) => expr,
        None => {
            let tok = stream.peek();
            error(
                diags,
                ParseCode::ParallelBadHeader,
                tok.span,
                "Expected reducer expression after '=>'",
            );
            return None;
        }
    };

    let span = parallel_tok.span.join(expr_span(&func));
    register_parallel_func(parallel_checks, diags, &func);
    Some(Expr::ParallelReduce {
        seq: Box::new(seq),
        init: Box::new(init),
        args,
        func: Box::new(func),
        span,
    })
}

/// Парсинг списка аргументов для выражений parallel
fn parse_parallel_arg_list(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    fn_purity: &mut HashMap<String, bool>,
    parallel_checks: &mut Vec<(Span, String)>,
) -> Vec<Expr> {
    let mut args = Vec::new();
    let Some(open) = stream.eat(TokenKind::LParen) else {
        let tok = stream.peek();
        error(
            diags,
            ParseCode::ParallelBadHeader,
            tok.span,
            "Expected '(' to start argument list",
        );
        return args;
    };

    if stream.at(TokenKind::RParen) {
        stream.bump();
        return args;
    }

    loop {
        if stream.is_eof() {
            break;
        }

        match parse_expr(stream, diags, fn_purity, parallel_checks) {
            Some(expr) => args.push(expr),
            None => {
                recover_parallel_args(stream);
                break;
            }
        }

        if stream.eat(TokenKind::Comma).is_some() {
            continue;
        }
        break;
    }

    if stream.eat(TokenKind::RParen).is_none() {
        error(
            diags,
            ParseCode::ParallelBadHeader,
            open.span,
            "Expected ')' after argument list",
        );
    }

    args
}

/// Регистрация функции для проверки чистоты parallel
fn register_parallel_func(
    parallel_checks: &mut Vec<(Span, String)>,
    diags: &mut Vec<ParseDiag>,
    func: &Expr,
) {
    match func {
        Expr::Ident(name, span) => {
            parallel_checks.push((*span, name.clone()));
        }
        other => {
            let span = expr_span(other);
            error(
                diags,
                ParseCode::ParallelFuncNotPure,
                span,
                "Parallel map/reduce requires a function name marked @pure",
            );
        }
    }
}

/// Обработка завершающих токенов выражений
pub fn handle_trailing_expr_tokens(stream: &mut Stream, diags: &mut Vec<ParseDiag>, expr: &Expr) {
    let next = stream.peek();
    if is_expr_terminator(next.kind) || matches!(next.kind, TokenKind::Semicolon) {
        return;
    }
    if matches!(expr, Expr::Ident(_, _))
        && matches!(
            next.kind,
            TokenKind::IntLit | TokenKind::FloatLit | TokenKind::Ident
        )
    {
        error(
            diags,
            ParseCode::UnexpectedToken,
            next.span,
            "Expected '=' in assignment",
        );
        stream.bump();
        return;
    }
    if !is_expr_terminator(next.kind) && !stream.is_eof() {
        error(
            diags,
            ParseCode::UnexpectedToken,
            next.span,
            format!("Unexpected token {:?} in expression", next.kind),
        );
        stream.bump();
    }
}

/// Проверка является ли выражение допустимой целью для присваивания
fn is_assignable_expr(expr: &Expr) -> bool {
    match expr {
        Expr::Ident(_, _) => true,  // Переменные могут быть присвоены
        Expr::Index { .. } => true, // Индексирование массива/объекта может быть присвоено (arr[i] = x)
        _ => false,                 // Литералы, вызовы функций и т.д. не могут быть присвоены
    }
}

fn is_expr_terminator(kind: TokenKind) -> bool {
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
            | TokenKind::Keyword(Keyword::With)
            | TokenKind::Eof
    )
}

fn report_unexpected_in_expr(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    tok: Token,
    lhs: &Expr,
) {
    if matches!(lhs, Expr::Ident(_, _))
        && matches!(
            tok.kind,
            TokenKind::IntLit | TokenKind::FloatLit | TokenKind::Ident
        )
    {
        error(
            diags,
            ParseCode::UnexpectedToken,
            tok.span,
            "Expected '=' in assignment",
        );
        stream.bump();
        return;
    }
    if tok.kind == TokenKind::RAngle {
        if let Some(prev) = stream.previous() {
            if prev.kind == TokenKind::LAngle {
                error(
                    diags,
                    ParseCode::UnexpectedToken,
                    tok.span,
                    "Unexpected token '>' after '<' — operator '<>' is not valid",
                );
                return;
            }
        }
    }
    error(
        diags,
        ParseCode::UnexpectedToken,
        tok.span,
        format!("Unexpected token {:?} in expression", tok.kind),
    );
    if !is_expr_terminator(tok.kind) && !stream.is_eof() {
        stream.bump();
    }
}

// Функции восстановления
fn recover_pattern_args(stream: &mut Stream) {
    while !stream.is_eof() {
        match stream.peek().kind {
            TokenKind::Comma => {
                stream.bump();
                break;
            }
            TokenKind::RParen => break,
            _ => {
                stream.bump();
            }
        }
    }
}

fn recover_compare_arm(stream: &mut Stream) {
    while !stream.is_eof() {
        match stream.peek().kind {
            TokenKind::Semicolon | TokenKind::Comma => {
                stream.bump();
                break;
            }
            TokenKind::RBrace => break,
            _ => {
                stream.bump();
            }
        }
    }
}

fn recover_parallel_args(stream: &mut Stream) {
    while !stream.is_eof() {
        match stream.peek().kind {
            TokenKind::Comma => {
                stream.bump();
                break;
            }
            TokenKind::RParen => break,
            _ => {
                stream.bump();
            }
        }
    }
}

/// Получение span выражения
pub fn expr_span(expr: &Expr) -> Span {
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
        | Expr::Compare { span, .. }
        | Expr::Ternary { span, .. }
        | Expr::Let { span, .. }
        | Expr::ParallelMap { span, .. }
        | Expr::ParallelReduce { span, .. } => *span,
    }
}

/// Применение span к выражению
pub fn with_span(expr: Expr, span: Span) -> Expr {
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
        Expr::Assign { lhs, rhs, op, .. } => Expr::Assign { lhs, rhs, op, span },
        Expr::Compare {
            scrutinee, arms, ..
        } => Expr::Compare {
            scrutinee,
            arms,
            span,
        },
        Expr::Ternary {
            cond,
            then_branch,
            else_branch,
            ..
        } => Expr::Ternary {
            cond,
            then_branch,
            else_branch,
            span,
        },
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
            seq, args, func, ..
        } => Expr::ParallelMap {
            seq,
            args,
            func,
            span,
        },
        Expr::ParallelReduce {
            seq,
            init,
            args,
            func,
            ..
        } => Expr::ParallelReduce {
            seq,
            init,
            args,
            func,
            span,
        },
    }
}

fn assign_op_from_token(kind: TokenKind) -> Option<AssignOp> {
    use AssignOp::*;
    use TokenKind::*;

    Some(match kind {
        Eq => Assign,
        PlusEq => AddAssign,
        MinusEq => SubAssign,
        StarEq => MulAssign,
        SlashEq => DivAssign,
        PercentEq => ModAssign,
        AmpEq => BitAndAssign,
        PipeEq => BitOrAssign,
        CaretEq => BitXorAssign,
        ShlEq => ShlAssign,
        ShrEq => ShrAssign,
        _ => return None,
    })
}

/// Вспомогательная функция для добавления ошибки
fn error(diags: &mut Vec<ParseDiag>, code: ParseCode, span: Span, message: impl Into<String>) {
    diags.push(ParseDiag::new(code, span, message));
}
