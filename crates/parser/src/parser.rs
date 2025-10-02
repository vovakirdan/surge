//! Recursive-descent parser that turns tokens into a Surge AST.

use crate::ast::SpanExt;
use crate::ast::*;
use crate::attributes::parse_attrs;
use crate::error::{ParseCode, ParseDiag};
use crate::expressions::{expr_span, parse_expr};
use crate::lexer_api::Stream;
use crate::statements::{parse_block, parse_stmt};
use crate::sync::is_top_level_sync;
use crate::types::parse_type_node;
use std::collections::{HashMap, hash_map::Entry};
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

        if self.stream.at_keyword(Keyword::Extern) {
            return self.parse_extern(attrs).map(Item::Extern);
        }

        if self.stream.at_keyword(Keyword::Newtype) {
            return self.parse_newtype(attrs).map(Item::Newtype);
        }

        if self.stream.at_keyword(Keyword::Type) {
            return self.parse_type_def(attrs).map(Item::Type);
        }

        if self.stream.at_keyword(Keyword::Alias) {
            return self.parse_alias(attrs).map(Item::Alias);
        }

        if self.stream.at_keyword(Keyword::Literal) {
            return self.parse_literal(attrs).map(Item::Literal);
        }

        if self.stream.at_keyword(Keyword::Tag) {
            return self.parse_tag(attrs).map(Item::Tag);
        }

        if let Some(keyword) = self.stream.peek_keyword() {
            match keyword {
                Keyword::Import => {
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

    fn parse_extern(&mut self, attrs: Vec<Attr>) -> Option<ExternBlock> {
        let Some(extern_tok) = self.stream.eat_keyword(Keyword::Extern) else {
            let tok = self.stream.bump();
            self.error(
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected 'extern' keyword",
            );
            return None;
        };

        let Some(lt_tok) = self.stream.eat(TokenKind::LAngle) else {
            let tok = self.stream.peek();
            self.error(
                ParseCode::ExternGenericBrackets,
                tok.span,
                "Expected '<' after 'extern'",
            );
            return None;
        };

        let target = match parse_type_node(&mut self.stream, &mut self.diags) {
            Some(ty) => ty,
            None => {
                self.error(
                    ParseCode::ExternMissingType,
                    lt_tok.span,
                    "Expected type in extern block",
                );
                self.recover_after_extern_type();
                return None;
            }
        };

        if self.stream.eat(TokenKind::RAngle).is_none() {
            let tok = self.stream.peek();
            self.error(
                ParseCode::ExternGenericBrackets,
                tok.span,
                "Expected '>' to close extern target",
            );
            self.recover_after_extern_type();
            self.stream.eat(TokenKind::RAngle);
        }

        let open_brace = match self.stream.eat(TokenKind::LBrace) {
            Some(tok) => tok,
            None => {
                let tok = self.stream.peek();
                self.error(
                    ParseCode::UnexpectedToken,
                    tok.span,
                    "Expected '{' to start extern block",
                );
                return None;
            }
        };

        let mut methods = Vec::new();
        while !self.stream.is_eof() && !self.stream.at(TokenKind::RBrace) {
            let method_attrs = parse_attrs(&mut self.stream, &mut self.diags);
            if self.stream.at_keyword(Keyword::Fn) {
                match self.parse_fn(method_attrs) {
                    Some(func) => methods.push(func),
                    None => self.synchronize_extern_item(),
                }
                continue;
            }

            if self.stream.at(TokenKind::RBrace) {
                break;
            }

            let tok = self.stream.peek();
            self.error(
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected function declaration in extern block",
            );
            self.stream.bump();
            self.synchronize_extern_item();
        }

        let mut span = extern_tok.span.join(target.span);
        if let Some(close) = self.stream.eat(TokenKind::RBrace) {
            span = span.join(close.span);
        } else {
            self.error(
                ParseCode::ExternUnclosedBlock,
                open_brace.span,
                "Expected '}' to close extern block",
            );
            self.synchronize_extern_block();
        }

        Some(ExternBlock {
            attrs,
            target,
            methods,
            span,
        })
    }

    // ========================================
    // USER-DEFINED TYPE PARSING
    // ========================================

    /// Parse a `newtype Name = ExistingType;` declaration.
    fn parse_newtype(&mut self, attrs: Vec<Attr>) -> Option<NewtypeDef> {
        let Some(newtype_tok) = self.stream.eat_keyword(Keyword::Newtype) else {
            let tok = self.stream.bump();
            self.error(
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected 'newtype' keyword",
            );
            return None;
        };

        let (name, name_span) = self.expect_ident("newtype name")?;
        let generics = self.parse_generic_params();

        let Some(eq_tok) = self.stream.eat(TokenKind::Eq) else {
            let tok = self.stream.peek();
            self.error(
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected '=' after newtype name",
            );
            return None;
        };

        let ty = match parse_type_node(&mut self.stream, &mut self.diags) {
            Some(ty) => ty,
            None => {
                self.error(
                    ParseCode::UnexpectedToken,
                    eq_tok.span,
                    "Expected type on the right-hand side of newtype",
                );
                return None;
            }
        };

        let mut span = newtype_tok.span.join(name_span);
        if let Some(last_generic) = generics.last() {
            span = span.join(last_generic.span);
        }
        span = span.join(ty.span);
        if let Some(semi) = self.stream.eat(TokenKind::Semicolon) {
            span = span.join(semi.span);
        } else {
            let tok = self.stream.peek();
            self.error(
                ParseCode::MissingSemicolon,
                tok.span,
                "Expected ';' after newtype definition",
            );
        }

        Some(NewtypeDef {
            name,
            generics,
            ty,
            attrs,
            span,
        })
    }

    /// Parse a `type Name = Base : { ... }` or `type Name = { ... }` declaration.
    fn parse_type_def(&mut self, attrs: Vec<Attr>) -> Option<TypeDef> {
        let Some(type_tok) = self.stream.eat_keyword(Keyword::Type) else {
            let tok = self.stream.bump();
            self.error(
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected 'type' keyword",
            );
            return None;
        };

        let (name, name_span) = self.expect_ident("type name")?;
        let generics = self.parse_generic_params();

        let Some(eq_tok) = self.stream.eat(TokenKind::Eq) else {
            let tok = self.stream.peek();
            self.error(
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected '=' after type name",
            );
            return None;
        };

        let mut base = None;
        let (fields, body_span) = if self.stream.at(TokenKind::LBrace) {
            match self.parse_struct_body() {
                Some(res) => res,
                None => (Vec::new(), eq_tok.span),
            }
        } else {
            let parsed_base = match parse_type_node(&mut self.stream, &mut self.diags) {
                Some(ty) => {
                    let span = ty.span;
                    base = Some(ty);
                    span
                }
                None => {
                    self.error(
                        ParseCode::UnexpectedToken,
                        eq_tok.span,
                        "Expected base type or '{' after '='",
                    );
                    return None;
                }
            };

            if self.stream.eat(TokenKind::Colon).is_none() {
                self.error(
                    ParseCode::UnexpectedToken,
                    self.stream.peek().span,
                    "Expected ':' before struct extension body",
                );
                return None;
            }

            match self.parse_struct_body() {
                Some((fields, span)) => (fields, parsed_base.join(span)),
                None => (Vec::new(), parsed_base),
            }
        };

        let mut span = type_tok.span.join(name_span);
        if let Some(last_generic) = generics.last() {
            span = span.join(last_generic.span);
        }
        if let Some(base_ty) = &base {
            span = span.join(base_ty.span);
        }
        span = span.join(body_span);

        if let Some(semi) = self.stream.eat(TokenKind::Semicolon) {
            span = span.join(semi.span);
        }

        Some(TypeDef {
            name,
            generics,
            base,
            fields,
            attrs,
            span,
        })
    }

    /// Parse an `alias Name = Variant | Variant;` declaration.
    fn parse_alias(&mut self, attrs: Vec<Attr>) -> Option<AliasDef> {
        let Some(alias_tok) = self.stream.eat_keyword(Keyword::Alias) else {
            let tok = self.stream.bump();
            self.error(
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected 'alias' keyword",
            );
            return None;
        };

        let (name, name_span) = self.expect_ident("alias name")?;
        let generics = self.parse_generic_params();

        let Some(eq_tok) = self.stream.eat(TokenKind::Eq) else {
            let tok = self.stream.peek();
            self.error(
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected '=' after alias name",
            );
            return None;
        };

        let mut variants = Vec::new();
        let mut last_span = eq_tok.span;

        while !self.stream.at(TokenKind::Semicolon) && !self.stream.is_eof() {
            if let Some(variant) = self.parse_alias_variant() {
                last_span = match &variant {
                    AliasVariant::Type(ty) => ty.span,
                    AliasVariant::Nothing { span } => *span,
                    AliasVariant::Tag { span, .. } => *span,
                };
                variants.push(variant);
            } else {
                self.recover_in_alias_variants();
            }

            if self.stream.eat(TokenKind::Pipe).is_some() {
                continue;
            }
            break;
        }

        if variants.is_empty() {
            self.error(
                ParseCode::UnexpectedToken,
                self.stream.peek().span,
                "Expected at least one alternative in alias definition",
            );
        }

        let mut span = alias_tok.span.join(name_span);
        if let Some(last_generic) = generics.last() {
            span = span.join(last_generic.span);
        }
        span = span.join(last_span);
        if let Some(semi) = self.stream.eat(TokenKind::Semicolon) {
            span = span.join(semi.span);
        } else {
            let tok = self.stream.peek();
            self.error(
                ParseCode::MissingSemicolon,
                tok.span,
                "Expected ';' after alias definition",
            );
        }

        Some(AliasDef {
            name,
            generics,
            variants,
            attrs,
            span,
        })
    }

    /// Parse a `literal Name = "foo" | "bar";` declaration.
    fn parse_literal(&mut self, attrs: Vec<Attr>) -> Option<LiteralDef> {
        let Some(literal_tok) = self.stream.eat_keyword(Keyword::Literal) else {
            let tok = self.stream.bump();
            self.error(
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected 'literal' keyword",
            );
            return None;
        };

        let (name, name_span) = self.expect_ident("literal name")?;

        let Some(eq_tok) = self.stream.eat(TokenKind::Eq) else {
            let tok = self.stream.peek();
            self.error(
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected '=' after literal name",
            );
            return None;
        };

        // Track literal alternatives to flag duplicates early (E_FIELD_CONFLICT equivalent for literals).
        let mut seen = HashMap::<String, Span>::new();
        let mut values = Vec::new();
        let mut last_span = eq_tok.span;

        while !self.stream.is_eof() {
            match self.expect_string_literal("literal alternative") {
                Some((value, span)) => {
                    last_span = span;
                    match seen.entry(value.clone()) {
                        Entry::Vacant(slot) => {
                            slot.insert(span);
                        }
                        Entry::Occupied(entry) => {
                            let prev = *entry.get();
                            let display_value = self
                                .stream
                                .slice(span)
                                .map(|s| s.to_string())
                                .unwrap_or_else(|| format!("\"{}\"", value));
                            let diag = ParseDiag::new(
                                ParseCode::DuplicateLiteral,
                                span,
                                format!(
                                    "Literal value {} is declared multiple times",
                                    display_value
                                ),
                            )
                            .with_related(prev, "Previous declaration here");
                            self.diags.push(diag);
                        }
                    }
                    values.push(LiteralVariant { value, span });
                }
                None => {
                    self.recover_in_literal_values();
                }
            }

            if self.stream.eat(TokenKind::Pipe).is_some() {
                continue;
            }
            break;
        }

        if values.is_empty() {
            self.error(
                ParseCode::UnexpectedToken,
                self.stream.peek().span,
                "Expected at least one literal alternative",
            );
        }

        let mut span = literal_tok.span.join(name_span);
        span = span.join(last_span);
        if let Some(semi) = self.stream.eat(TokenKind::Semicolon) {
            span = span.join(semi.span);
        } else {
            let tok = self.stream.peek();
            self.error(
                ParseCode::MissingSemicolon,
                tok.span,
                "Expected ';' after literal definition",
            );
        }

        Some(LiteralDef {
            name,
            values,
            attrs,
            span,
        })
    }

    /// Parse a `tag Name<T>(args);` declaration.
    fn parse_tag(&mut self, attrs: Vec<Attr>) -> Option<TagDef> {
        let Some(tag_tok) = self.stream.eat_keyword(Keyword::Tag) else {
            let tok = self.stream.bump();
            self.error(
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected 'tag' keyword",
            );
            return None;
        };

        let (name, name_span) = self.expect_ident("tag name")?;
        let generics = self.parse_generic_params();

        let Some(open) = self.stream.eat(TokenKind::LParen) else {
            let tok = self.stream.peek();
            self.error(
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected '(' after tag name",
            );
            return None;
        };

        let mut params = Vec::new();
        let mut end_span = open.span;

        if self.stream.at(TokenKind::RParen) {
            end_span = self.stream.bump().span;
        } else {
            while !self.stream.is_eof() {
                match parse_type_node(&mut self.stream, &mut self.diags) {
                    Some(ty) => {
                        end_span = ty.span;
                        params.push(ty);
                    }
                    None => {
                        self.recover_in_tag_params();
                        break;
                    }
                }

                if self.stream.eat(TokenKind::Comma).is_some() {
                    continue;
                }
                break;
            }

            if let Some(close) = self.stream.eat(TokenKind::RParen) {
                end_span = close.span;
            } else {
                let tok = self.stream.peek();
                self.error(
                    ParseCode::UnexpectedToken,
                    tok.span,
                    "Expected ')' to close tag parameters",
                );
            }
        }

        let mut span = tag_tok.span.join(name_span);
        if let Some(last_generic) = generics.last() {
            span = span.join(last_generic.span);
        }
        span = span.join(end_span);

        if let Some(semi) = self.stream.eat(TokenKind::Semicolon) {
            span = span.join(semi.span);
        } else {
            let tok = self.stream.peek();
            self.error(
                ParseCode::MissingSemicolon,
                tok.span,
                "Expected ';' after tag declaration",
            );
        }

        Some(TagDef {
            name,
            generics,
            params,
            attrs,
            span,
        })
    }

    /// Parse `<T, U>` generic parameter lists shared between newtype, alias and type definitions.
    fn parse_generic_params(&mut self) -> Vec<GenericParam> {
        if !self.stream.at(TokenKind::LAngle) {
            return Vec::new();
        }

        let open = self.stream.bump();
        let mut params = Vec::new();

        while !self.stream.is_eof() {
            if self.stream.at(TokenKind::RAngle) {
                let close = self.stream.bump();
                if params.is_empty() {
                    self.error(
                        ParseCode::UnexpectedToken,
                        close.span,
                        "Generic parameter list cannot be empty",
                    );
                }
                break;
            }

            let (name, span) = match self.expect_ident("generic parameter") {
                Some(res) => res,
                None => {
                    self.recover_in_generics();
                    break;
                }
            };
            params.push(GenericParam { name, span });

            if self.stream.eat(TokenKind::Comma).is_some() {
                continue;
            }

            if self.stream.eat(TokenKind::RAngle).is_some() {
                break;
            }

            let tok = self.stream.peek();
            self.error(
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected ',' or '>' in generic parameter list",
            );
            self.recover_in_generics();
            break;
        }

        if !params.is_empty() {
            params
        } else {
            self.error(
                ParseCode::UnexpectedToken,
                open.span,
                "Expected at least one generic parameter",
            );
            Vec::new()
        }
    }

    /// Parse `{ field: Type, ... }` bodies used by struct definitions.
    fn parse_struct_body(&mut self) -> Option<(Vec<StructField>, Span)> {
        let Some(open) = self.stream.eat(TokenKind::LBrace) else {
            let tok = self.stream.peek();
            self.error(
                ParseCode::UnexpectedToken,
                tok.span,
                "Expected '{' to start type body",
            );
            return None;
        };

        let mut fields = Vec::new();
        let mut last_span = open.span;
        let mut seen_fields = HashMap::<String, Span>::new();

        while !self.stream.at(TokenKind::RBrace) && !self.stream.is_eof() {
            let attrs = parse_attrs(&mut self.stream, &mut self.diags);

            if self.stream.at(TokenKind::RBrace) {
                if !attrs.is_empty() {
                    self.error(
                        ParseCode::UnexpectedToken,
                        self.stream.peek().span,
                        "Expected field declaration after attributes",
                    );
                }
                break;
            }

            let (name, name_span) = match self.expect_ident("field name") {
                Some(res) => res,
                None => {
                    self.recover_in_struct_fields();
                    self.stream.eat(TokenKind::Comma);
                    continue;
                }
            };

            let mut span = name_span;

            if self.stream.eat(TokenKind::Colon).is_none() {
                self.error(
                    ParseCode::MissingColonInType,
                    name_span,
                    "Expected ':' before field type",
                );
                self.recover_in_struct_fields();
                self.stream.eat(TokenKind::Comma);
                continue;
            }

            let ty = match parse_type_node(&mut self.stream, &mut self.diags) {
                Some(ty) => {
                    span = span.join(ty.span);
                    ty
                }
                None => {
                    self.recover_in_struct_fields();
                    self.stream.eat(TokenKind::Comma);
                    continue;
                }
            };

            let default = if let Some(eq_tok) = self.stream.eat(TokenKind::Eq) {
                match parse_expr(
                    &mut self.stream,
                    &mut self.diags,
                    &mut self.fn_purity,
                    &mut self.parallel_checks,
                ) {
                    Some(expr) => {
                        span = span.join(expr_span(&expr));
                        Some(expr)
                    }
                    None => {
                        span = span.join(eq_tok.span);
                        None
                    }
                }
            } else {
                None
            };

            let field = StructField {
                attrs,
                name,
                ty,
                default,
                span,
            };
            last_span = field.span;
            match seen_fields.entry(field.name.clone()) {
                Entry::Vacant(slot) => {
                    slot.insert(field.span);
                }
                Entry::Occupied(entry) => {
                    let prev = *entry.get();
                    // Emit E_FIELD_CONFLICT eagerly so extensions surface duplicates during parsing.
                    let diag = ParseDiag::new(
                        ParseCode::FieldConflict,
                        field.span,
                        format!("Field '{}' is declared multiple times", field.name),
                    )
                    .with_related(prev, "Previous declaration here");
                    self.diags.push(diag);
                }
            }
            fields.push(field);

            if let Some(comma) = self.stream.eat(TokenKind::Comma) {
                last_span = comma.span;
            } else if !self.stream.at(TokenKind::RBrace) {
                let tok = self.stream.peek();
                self.error(
                    ParseCode::UnexpectedToken,
                    tok.span,
                    "Expected ',' or '}' after struct field",
                );
                self.recover_in_struct_fields();
                self.stream.eat(TokenKind::Comma);
            }
        }

        let close_span = if let Some(close) = self.stream.eat(TokenKind::RBrace) {
            close.span
        } else {
            let tok = self.stream.peek();
            self.error(
                ParseCode::UnclosedBrace,
                tok.span,
                "Expected '}' to close type body",
            );
            last_span
        };

        let body_span = open.span.join(close_span);
        Some((fields, body_span))
    }

    /// Parse a single union alternative inside an `alias` declaration.
    fn parse_alias_variant(&mut self) -> Option<AliasVariant> {
        if self.stream.at_keyword(Keyword::Nothing) {
            let tok = self.stream.bump();
            return Some(AliasVariant::Nothing { span: tok.span });
        }

        if self.stream.peek().kind == TokenKind::Ident
            && self.stream.nth(1).kind == TokenKind::LParen
        {
            let (name, name_span) = self.expect_ident("tag name")?;
            let open = self.stream.bump(); // consume '('
            let mut args = Vec::new();
            let mut end_span = open.span;

            if self.stream.at(TokenKind::RParen) {
                end_span = self.stream.bump().span;
            } else {
                loop {
                    match parse_type_node(&mut self.stream, &mut self.diags) {
                        Some(arg) => {
                            end_span = arg.span;
                            args.push(arg);
                        }
                        None => {
                            self.recover_in_alias_variants();
                            break;
                        }
                    }

                    if self.stream.eat(TokenKind::Comma).is_some() {
                        continue;
                    }
                    break;
                }

                if let Some(close) = self.stream.eat(TokenKind::RParen) {
                    end_span = close.span;
                } else {
                    let tok = self.stream.peek();
                    self.error(
                        ParseCode::UnexpectedToken,
                        tok.span,
                        "Expected ')' to close tag variant",
                    );
                }
            }

            let span = name_span.join(end_span);
            return Some(AliasVariant::Tag { name, args, span });
        }

        parse_type_node(&mut self.stream, &mut self.diags).map(AliasVariant::Type)
    }

    /// Recover inside a struct field list by skipping to the next delimiter.
    fn recover_in_struct_fields(&mut self) {
        while !self.stream.is_eof() {
            match self.stream.peek().kind {
                TokenKind::Comma | TokenKind::RBrace => break,
                _ => {
                    self.stream.bump();
                }
            }
        }
    }

    /// Recover in a generic parameter list until we hit a closing bracket.
    fn recover_in_generics(&mut self) {
        while !self.stream.is_eof() {
            match self.stream.peek().kind {
                TokenKind::RAngle => {
                    self.stream.bump();
                    break;
                }
                TokenKind::Comma => break,
                _ => {
                    self.stream.bump();
                }
            }
        }
    }

    /// Recover inside alias variants until the next `|` or `;`.
    fn recover_in_alias_variants(&mut self) {
        while !self.stream.is_eof() {
            match self.stream.peek().kind {
                TokenKind::Pipe | TokenKind::Semicolon => break,
                _ => {
                    self.stream.bump();
                }
            }
        }
    }

    /// Skip tokens until the next literal delimiter to keep error recovery predictable.
    fn recover_in_literal_values(&mut self) {
        while !self.stream.is_eof() {
            match self.stream.peek().kind {
                TokenKind::Pipe | TokenKind::Semicolon => break,
                _ => {
                    self.stream.bump();
                }
            }
        }
    }

    /// Skip to the next comma or closing parenthesis when a tag parameter fails to parse.
    fn recover_in_tag_params(&mut self) {
        while !self.stream.is_eof() {
            match self.stream.peek().kind {
                TokenKind::Comma | TokenKind::RParen => break,
                _ => {
                    self.stream.bump();
                }
            }
        }
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

    fn expect_string_literal(&mut self, what: &str) -> Option<(String, Span)> {
        let tok = self.stream.peek();
        if tok.kind == TokenKind::StringLit {
            let taken = self.stream.bump();
            if let Some(slice) = self.stream.slice(taken.span) {
                let text = slice.to_string();
                if text.len() >= 2 && text.starts_with('"') && text.ends_with('"') {
                    let inner = text[1..text.len() - 1].to_string();
                    return Some((inner, taken.span));
                }
                return Some((text, taken.span));
            }
            return Some((String::new(), taken.span));
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

    fn recover_after_extern_type(&mut self) {
        while !self.stream.is_eof() {
            match self.stream.peek().kind {
                TokenKind::RAngle | TokenKind::LBrace | TokenKind::RBrace => break,
                _ => {
                    self.stream.bump();
                }
            }
        }
    }

    fn synchronize_extern_item(&mut self) {
        while !self.stream.is_eof() {
            match self.stream.peek().kind {
                TokenKind::Keyword(Keyword::Fn) | TokenKind::RBrace => break,
                _ => {
                    self.stream.bump();
                }
            }
        }
    }

    fn synchronize_extern_block(&mut self) {
        while !self.stream.is_eof() && !self.stream.at(TokenKind::RBrace) {
            self.stream.bump();
        }
        self.stream.eat(TokenKind::RBrace);
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
