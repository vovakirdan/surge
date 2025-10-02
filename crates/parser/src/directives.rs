use std::sync::Arc;

use crate::ast::{DirectiveAnchor, DirectiveBlock, DirectiveBody, DirectiveCondition, SpanExt};
use crate::error::{ParseCode, ParseDiag};
use crate::lexer_api::Stream;
use surge_token::{DirectiveKind, DirectiveSpec, SourceId, Span, Token, TokenContext, TokenKind};

/// Consume a directive block from the token stream if the next token starts one.
/// Returns `None` when the upcoming token is not a directive header.
pub fn take_directive_block(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    file: SourceId,
) -> Option<DirectiveBlock> {
    let lookahead = stream.peek();
    let spec = match &lookahead.kind {
        TokenKind::Directive(spec) => Arc::clone(spec),
        _ => return None,
    };

    let header = stream.bump();
    let mut block = DirectiveBlock {
        kind: spec.kind,
        namespace: spec.namespace().to_string(),
        sub_namespace: spec.sub_namespace().map(|s| s.to_string()),
        label: None,
        has_trailing_colon: spec.has_trailing_colon,
        body: DirectiveBody::default(),
        span: header.span,
        header_span: header.span,
        anchor: DirectiveAnchor::Detached,
        condition: None,
    };

    let mut last_span = header.span;
    let requires_label = block.kind != DirectiveKind::Target;

    if !block.has_trailing_colon {
        diags.push(ParseDiag::new(
            ParseCode::DirectiveMalformed,
            header.span,
            "Directive header must end with ':'",
        ));
    }

    if requires_label {
        match peek_directive(stream, &spec) {
            Some(next) if matches!(next.kind, TokenKind::Ident) => {
                let label_tok = stream.bump();
                let label_start = label_tok.span.start;
                let mut label_end = label_tok.span.end;
                let mut found_colon = false;

                loop {
                    match peek_directive(stream, &spec) {
                        Some(colon_tok) if matches!(colon_tok.kind, TokenKind::Colon) => {
                            let colon_tok = stream.bump();
                            last_span = colon_tok.span;
                            block.header_span = block.header_span.join(last_span);
                            found_colon = true;
                            break;
                        }
                        Some(_) => {
                            let tok = stream.bump();
                            label_end = tok.span.end;
                        }
                        None => break,
                    }
                }

                if !found_colon {
                    diags.push(ParseDiag::new(
                        ParseCode::DirectiveMalformed,
                        label_tok.span,
                        "Expected ':' after directive label",
                    ));
                }

                let label_span = Span::new(file, label_start, label_end);
                let label_text = stream
                    .slice(label_span)
                    .unwrap_or_default()
                    .trim()
                    .to_string();
                block.label = Some(label_text);
            }
            Some(unexpected) => {
                diags.push(ParseDiag::new(
                    ParseCode::DirectiveMalformed,
                    unexpected.span,
                    "Directive label must be an identifier",
                ));
            }
            None => {
                diags.push(ParseDiag::new(
                    ParseCode::DirectiveMalformed,
                    header.span,
                    "Directive header must be followed by a label",
                ));
            }
        }
    }

    let mut body_tokens = Vec::new();
    while peek_directive(stream, &spec).is_some() {
        let tok = stream.bump();
        last_span = tok.span;
        body_tokens.push(tok);
    }

    // Determine the textual extent of the directive block for raw body reconstruction.
    let next = stream.peek();
    let mut end = last_span.end.max(header.span.end);
    if !is_same_directive(&next, &spec) {
        end = end.max(next.span.start);
    }
    block.span = header.span.join(Span::new(file, end, end));

    let skip_lines = if requires_label { 2 } else { 1 };
    block.body = DirectiveBody {
        raw_lines: collect_body_lines(stream, file, header.span.start, end, skip_lines),
        tokens: body_tokens,
    };

    if block.kind == DirectiveKind::Target {
        block.condition = parse_target_condition(stream, &block.body.tokens, diags, file);
    }

    Some(block)
}

fn peek_directive(stream: &Stream, spec: &Arc<DirectiveSpec>) -> Option<Token> {
    let tok = stream.peek();
    if is_same_directive(&tok, spec) {
        Some(tok)
    } else {
        None
    }
}

fn is_same_directive(tok: &Token, spec: &Arc<DirectiveSpec>) -> bool {
    matches!(
        &tok.context,
        TokenContext::Directive(ctx) if Arc::ptr_eq(ctx, spec)
    )
}

fn collect_body_lines(
    stream: &Stream,
    file: SourceId,
    start: u32,
    end: u32,
    skip: usize,
) -> Vec<String> {
    let Some(snippet) = stream.slice(Span::new(file, start, end)) else {
        return Vec::new();
    };

    snippet
        .lines()
        .map(|line| line_after_marker(line))
        .skip(skip)
        .map(|line| line.to_string())
        .collect()
}

fn line_after_marker(line: &str) -> &str {
    match line.find("///") {
        Some(idx) => line[idx + 3..].trim_start(),
        None => line.trim_start(),
    }
}

fn parse_target_condition(
    stream: &Stream,
    tokens: &[Token],
    diags: &mut Vec<ParseDiag>,
    file: SourceId,
) -> Option<DirectiveCondition> {
    if tokens.is_empty() {
        return None;
    }
    let mut parser = TargetConditionParser {
        tokens,
        idx: 0,
        stream,
        diags,
        file,
    };
    let condition = parser.parse_condition();
    if parser.idx < tokens.len() {
        let span = tokens[parser.idx].span;
        parser.diags.push(ParseDiag::new(
            ParseCode::DirectiveMalformed,
            span,
            "Unexpected trailing tokens in target directive",
        ));
    }
    condition
}

struct TargetConditionParser<'a> {
    tokens: &'a [Token],
    idx: usize,
    stream: &'a Stream<'a>,
    diags: &'a mut Vec<ParseDiag>,
    file: SourceId,
}

impl<'a> TargetConditionParser<'a> {
    fn parse_condition(&mut self) -> Option<DirectiveCondition> {
        let cond = self.parse_prim()?;
        Some(cond)
    }

    fn parse_prim(&mut self) -> Option<DirectiveCondition> {
        let token = self.next_token()?;
        match &token.kind {
            TokenKind::Ident => self.parse_ident_start(token),
            _ => {
                self.diags.push(ParseDiag::new(
                    ParseCode::DirectiveMalformed,
                    token.span,
                    "Expected identifier in target directive",
                ));
                None
            }
        }
    }

    fn parse_ident_start(&mut self, token: Token) -> Option<DirectiveCondition> {
        let ident = self.token_text(&token);
        if self.peek_kind(TokenKind::LParen) {
            self.bump();
            match ident.as_str() {
                "all" => self.parse_list(token.span, ListKind::All),
                "any" => self.parse_list(token.span, ListKind::Any),
                "not" => self.parse_not(token.span),
                _ => {
                    self.diags.push(ParseDiag::new(
                        ParseCode::DirectiveMalformed,
                        token.span,
                        "Unknown function in target directive",
                    ));
                    None
                }
            }
        } else if self.peek_kind(TokenKind::Eq) {
            self.bump();
            let value_tok = match self.next_token() {
                Some(tok) => tok,
                None => return None,
            };
            let value = match value_tok.kind {
                TokenKind::StringLit | TokenKind::Ident => self.token_text(&value_tok),
                _ => {
                    self.diags.push(ParseDiag::new(
                        ParseCode::DirectiveMalformed,
                        value_tok.span,
                        "Expected string or identifier after '='",
                    ));
                    return None;
                }
            };
            let span = token.span.join(value_tok.span);
            Some(DirectiveCondition::KeyValue {
                key: ident,
                value,
                span,
            })
        } else {
            Some(DirectiveCondition::Flag {
                name: ident,
                span: token.span,
            })
        }
    }

    fn parse_list(&mut self, start_span: Span, kind: ListKind) -> Option<DirectiveCondition> {
        let mut items = Vec::new();
        loop {
            if self.peek_kind(TokenKind::RParen) {
                let end = self.bump().span;
                return Some(kind.into_node(items, start_span.join(end)));
            }
            let item = self.parse_condition()?;
            items.push(item);
            if self.peek_kind(TokenKind::Comma) {
                self.bump();
            } else if self.peek_kind(TokenKind::RParen) {
                let end = self.bump().span;
                return Some(kind.into_node(items, start_span.join(end)));
            } else {
                let span = self.current_span();
                self.diags.push(ParseDiag::new(
                    ParseCode::DirectiveMalformed,
                    span,
                    "Expected ',' or ')' in target directive",
                ));
                return None;
            }
        }
    }

    fn parse_not(&mut self, start_span: Span) -> Option<DirectiveCondition> {
        let cond = self.parse_condition()?;
        if !self.peek_kind(TokenKind::RParen) {
            let span = self.current_span();
            self.diags.push(ParseDiag::new(
                ParseCode::DirectiveMalformed,
                span,
                "Expected ')' after not(...)",
            ));
            return None;
        }
        let end = self.bump().span;
        Some(DirectiveCondition::Not {
            span: start_span.join(end),
            condition: Box::new(cond),
        })
    }

    fn next_token(&mut self) -> Option<Token> {
        let tok = self.tokens.get(self.idx)?.clone();
        self.idx += 1;
        Some(tok)
    }

    fn peek_kind(&self, kind: TokenKind) -> bool {
        self.tokens
            .get(self.idx)
            .map_or(false, |tok| tok.kind == kind)
    }

    fn bump(&mut self) -> Token {
        let tok = self.tokens[self.idx].clone();
        self.idx += 1;
        tok
    }

    fn current_span(&self) -> Span {
        self.tokens
            .get(self.idx)
            .map(|tok| tok.span)
            .unwrap_or_else(|| Span::new(self.file, 0, 0))
    }

    fn token_text(&self, token: &Token) -> String {
        self.stream
            .slice(token.span)
            .unwrap_or("")
            .trim_matches('"')
            .to_string()
    }
}

enum ListKind {
    All,
    Any,
}

impl ListKind {
    fn into_node(self, conditions: Vec<DirectiveCondition>, span: Span) -> DirectiveCondition {
        match self {
            ListKind::All => DirectiveCondition::All { conditions, span },
            ListKind::Any => DirectiveCondition::Any { conditions, span },
        }
    }
}
