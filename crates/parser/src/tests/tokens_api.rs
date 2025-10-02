use crate::{
    Attr, DirectiveCondition, Item, ParseCode, parse_source, parse_source_with_options,
    parse_tokens,
};
use surge_lexer::{LexOptions, lex};
use surge_token::SourceId;

#[test]
fn test_parse_tokens_with_attributes() {
    let src = "@pure\nfn test() -> int { return 42; }";
    let source_id = SourceId(0);

    // Токенизируем
    let lex_res = lex(src, source_id, &LexOptions::default());

    // Парсим только из токенов (без исходного текста)
    let parse_res = parse_tokens(source_id, &lex_res.tokens);

    // Должна быть одна функция
    assert_eq!(parse_res.ast.module.items.len(), 1);

    if let Item::Fn(func) = &parse_res.ast.module.items[0] {
        // Проверим, что атрибут парсится при отсутствии исходного текста
        println!("Attributes: {:?}", func.sig.attrs);
        println!("Diagnostics count: {}", parse_res.diags.len());

        for diag in &parse_res.diags {
            println!("Diagnostic: {:?} - {}", diag.code, diag.message);
        }

        // Проверяем, что атрибут парсится правильно
        assert_eq!(func.sig.attrs.len(), 1);
        if let crate::Attr::Pure { .. } = &func.sig.attrs[0] {
            // Ожидаемое поведение
        } else {
            panic!("Expected Pure attribute, got: {:?}", func.sig.attrs[0]);
        }
    }
}

#[test]
fn test_parse_tokens_unknown_attribute() {
    let src = "@unknown\nfn test() -> int { return 42; }";
    let source_id = SourceId(0);

    let lex_res = lex(src, source_id, &LexOptions::default());
    let parse_res = parse_tokens(source_id, &lex_res.tokens);

    // Должна быть диагностика о неизвестном атрибуте
    assert!(!parse_res.diags.is_empty());

    for diag in &parse_res.diags {
        println!(
            "Unknown attr diagnostic: {:?} - {}",
            diag.code, diag.message
        );
    }
}

#[test]
fn test_parse_tokens_backend_attribute() {
    let src = r#"@backend("cpu")
fn test() -> int { return 42; }"#;
    let source_id = SourceId(0);

    let lex_res = lex(src, source_id, &LexOptions::default());
    let parse_res = parse_tokens(source_id, &lex_res.tokens);

    if let Item::Fn(func) = &parse_res.ast.module.items[0] {
        println!("Backend attributes: {:?}", func.sig.attrs);
    }

    for diag in &parse_res.diags {
        println!("Backend diagnostic: {:?} - {}", diag.code, diag.message);
    }
}

#[test]
fn test_parse_source_intrinsic_attribute() {
    let src = "@intrinsic\nfn intrinsic_fn() { }";
    let source_id = SourceId(1);

    let (parse_res, _lex_res) = parse_source(source_id, src);
    assert!(parse_res.diags.is_empty());

    let func = match &parse_res.ast.module.items[0] {
        Item::Fn(func) => func,
        other => panic!("Expected function item, got {:?}", other),
    };

    assert!(matches!(
        func.sig.attrs.first(),
        Some(Attr::Intrinsic { .. })
    ));
}

#[test]
fn test_parse_source_deprecated_attribute() {
    let src = "@deprecated(\"use_new\")\nfn legacy() { }";
    let source_id = SourceId(2);

    let (parse_res, _lex_res) = parse_source(source_id, src);
    assert!(parse_res.diags.is_empty());

    let func = match &parse_res.ast.module.items[0] {
        Item::Fn(func) => func,
        other => panic!("Expected function item, got {:?}", other),
    };

    match func.sig.attrs.first() {
        Some(Attr::Deprecated { message, .. }) => assert_eq!(message, "use_new"),
        unexpected => panic!("Expected Deprecated attr, got {:?}", unexpected),
    }
}

#[test]
fn test_parse_source_requires_lock_attribute() {
    let src = "@requires_lock(\"mutex\")\nfn critical() { }";
    let source_id = SourceId(3);

    let (parse_res, _lex_res) = parse_source(source_id, src);
    assert!(parse_res.diags.is_empty());

    let func = match &parse_res.ast.module.items[0] {
        Item::Fn(func) => func,
        other => panic!("Expected function item, got {:?}", other),
    };

    match func.sig.attrs.first() {
        Some(Attr::RequiresLock { lock, .. }) => assert_eq!(lock, "mutex"),
        unexpected => panic!("Expected RequiresLock attr, got {:?}", unexpected),
    }
}

#[test]
fn test_parse_tokens_overload_override_ambiguity() {
    // Демонстрируем ограничение эвристического подхода:
    // "overload" и "override" оба имеют 8 символов, поэтому без исходного текста
    // мы не можем их различить и по умолчанию выберем "overload"
    let src = "@override\nfn test() -> int { return 42; }";
    let source_id = SourceId(0);

    let lex_res = lex(src, source_id, &LexOptions::default());
    let parse_res = parse_tokens(source_id, &lex_res.tokens);

    if let Item::Fn(func) = &parse_res.ast.module.items[0] {
        println!("Ambiguous attribute parsed as: {:?}", func.sig.attrs);
        // Без исходного текста "@override" будет неправильно распознан как "overload"
        assert_eq!(func.sig.attrs.len(), 1);
        if let crate::Attr::Overload { .. } = &func.sig.attrs[0] {
            // Это ожидаемое поведение при отсутствии исходного текста
            println!("As expected, @override was parsed as @overload due to token-only limitation");
        } else {
            panic!(
                "Expected Overload attribute due to ambiguity, got: {:?}",
                func.sig.attrs[0]
            );
        }
    }
}

#[test]
fn test_doc_comment_not_directive() {
    let src = "/// Just documentation\nfn foo() {}";
    let source_id = SourceId(10);
    let opts = LexOptions {
        keep_trivia: false,
        enable_directives: true,
    };

    let (parse_res, _) = parse_source_with_options(source_id, &src, &opts);
    assert!(parse_res.diags.is_empty());
    assert!(parse_res.ast.module.directives.is_empty());
}

#[test]
fn test_multi_word_directive_label() {
    let src = r#"/// benchmark:
/// Performance test:
///   benchmark.measure(main);
fn main() {}"#;
    let source_id = SourceId(11);
    let opts = LexOptions {
        keep_trivia: false,
        enable_directives: true,
    };

    let (parse_res, _) = parse_source_with_options(source_id, &src, &opts);
    assert!(parse_res.diags.is_empty());
    assert_eq!(parse_res.ast.module.directives.len(), 1);
    let directive = &parse_res.ast.module.directives[0];
    assert_eq!(directive.label.as_deref(), Some("Performance test"));
}

#[test]
fn test_custom_directive_namespace_in_directive_module() {
    let src = r#"pragma directive
literal DirectiveName = "lint";

/// lint:
/// Checks:
///   lint.run();
fn lint_run() {}
"#;
    let source_id = SourceId(12);
    let opts = LexOptions {
        keep_trivia: false,
        enable_directives: true,
    };

    let (parse_res, _) = parse_source_with_options(source_id, &src, &opts);
    assert!(parse_res.diags.is_empty());
    assert_eq!(parse_res.ast.module.directives.len(), 1);
    assert_eq!(parse_res.ast.module.directives[0].namespace, "lint");
}

#[test]
fn test_target_directive_condition_parses() {
    let src = "/// target: all(os = \"linux\", feature = \"performance\")\r\n".to_owned()
        + "fn linux_performance_function() -> int {\r\n"
        + "    return 1000;\r\n"
        + "}\r\n";
    let source_id = SourceId(13);
    let opts = LexOptions {
        keep_trivia: false,
        enable_directives: true,
    };

    let (parse_res, _) = parse_source_with_options(source_id, &src, &opts);
    assert!(parse_res.diags.is_empty(), "diags: {:?}", parse_res.diags);
    assert_eq!(parse_res.ast.module.directives.len(), 1);
    match &parse_res.ast.module.directives[0].condition {
        Some(DirectiveCondition::All { conditions, .. }) => {
            assert_eq!(conditions.len(), 2);
        }
        other => panic!("Expected all(...) condition, got {:?}", other),
    }
}

#[test]
fn test_parse_tokens_function_without_return_type() {
    // Тест функции без типа возврата (теперь это валидно)
    let src = "fn no_return() { let x = 42; }";
    let source_id = SourceId(0);

    let lex_res = lex(src, source_id, &LexOptions::default());
    let parse_res = parse_tokens(source_id, &lex_res.tokens);

    assert_eq!(parse_res.ast.module.items.len(), 1);

    if let Item::Fn(func) = &parse_res.ast.module.items[0] {
        // Функция должна парситься успешно
        // В режиме parse_tokens имя будет fallback, так как нет исходного текста
        assert!(func.sig.name.starts_with("identifier_"));
        assert!(func.sig.ret.is_none()); // Нет типа возврата
        assert!(func.sig.params.is_empty());

        println!("Function without return type parsed successfully");
        println!("Diagnostics count: {}", parse_res.diags.len());

        for diag in &parse_res.diags {
            println!("Diagnostic: {:?} - {}", diag.code, diag.message);
        }

        // Не должно быть ошибок
        assert!(parse_res.diags.is_empty());
    } else {
        panic!("Expected function item");
    }
}

#[test]
fn test_examples_12_directives_diagnostics() {
    let src = include_str!("../../../../examples/syntax/12_directives.sg");
    let source_id = SourceId(99);
    let opts = LexOptions {
        keep_trivia: false,
        enable_directives: true,
    };

    let (parse_res, lex_res) = parse_source_with_options(source_id, src, &opts);
    let has_directive_malformed = parse_res
        .diags
        .iter()
        .any(|d| matches!(d.code, ParseCode::DirectiveMalformed));
    assert!(
        !has_directive_malformed,
        "unexpected directive diagnostics: {:?}",
        parse_res.diags
    );

    let token_parse = parse_tokens(source_id, &lex_res.tokens);
    let token_has_malformed = token_parse
        .diags
        .iter()
        .any(|d| matches!(d.code, ParseCode::DirectiveMalformed));
    assert!(
        !token_has_malformed,
        "unexpected directive diagnostics when parsing tokens only: {:?}",
        token_parse.diags
    );
}

#[test]
fn test_parse_tokens_function_with_arrow_but_no_type() {
    // Тест функции с -> но без типа (должна быть ошибка)
    let src = "fn bad_arrow() -> { return 42; }";
    let source_id = SourceId(0);

    let lex_res = lex(src, source_id, &LexOptions::default());
    let parse_res = parse_tokens(source_id, &lex_res.tokens);

    // Функция должна парситься, но с диагностикой
    assert_eq!(parse_res.ast.module.items.len(), 1);

    if let Item::Fn(func) = &parse_res.ast.module.items[0] {
        // В режиме parse_tokens имя будет fallback, так как нет исходного текста
        assert!(func.sig.name.starts_with("identifier_"));
        assert!(func.sig.ret.is_none()); // Нет валидного типа возврата

        println!("Function with -> but no type parsed");
        println!("Diagnostics count: {}", parse_res.diags.len());

        for diag in &parse_res.diags {
            println!("Diagnostic: {:?} - {}", diag.code, diag.message);
        }

        // Должна быть диагностика о том, что после -> ожидается тип
        assert!(!parse_res.diags.is_empty());
        let has_expected_type_error = parse_res
            .diags
            .iter()
            .any(|d| matches!(d.code, crate::ParseCode::ExpectedTypeAfterArrow));
        assert!(
            has_expected_type_error,
            "Expected PARSE_EXPECTED_TYPE_AFTER_ARROW diagnostic"
        );
    } else {
        panic!("Expected function item");
    }
}

#[test]
fn test_parse_tokens_function_with_nothing_return() {
    // Тест функции с возвратом nothing (валидная конструкция)
    let src = "fn test() { return nothing; }";
    let source_id = SourceId(0);

    let lex_res = lex(src, source_id, &LexOptions::default());
    let parse_res = parse_tokens(source_id, &lex_res.tokens);

    assert_eq!(parse_res.ast.module.items.len(), 1);

    if let Item::Fn(func) = &parse_res.ast.module.items[0] {
        // Функция должна парситься успешно
        assert!(func.sig.name.starts_with("identifier_"));
        assert!(func.sig.ret.is_none()); // Нет типа возврата
        assert!(func.sig.params.is_empty());

        println!("Function with nothing return parsed successfully");
        println!("Diagnostics count: {}", parse_res.diags.len());

        for diag in &parse_res.diags {
            println!("Diagnostic: {:?} - {}", diag.code, diag.message);
        }

        // Не должно быть ошибок
        assert!(parse_res.diags.is_empty());
    } else {
        panic!("Expected function item");
    }
}

#[test]
fn test_parse_tokens_function_with_return_type_and_nothing() {
    // Тест функции с типом возврата, но возвращающей nothing (должна быть ошибка на семантическом уровне, но парсится)
    let src = "fn test() -> int { return nothing; }";
    let source_id = SourceId(0);

    let lex_res = lex(src, source_id, &LexOptions::default());
    let parse_res = parse_tokens(source_id, &lex_res.tokens);

    assert_eq!(parse_res.ast.module.items.len(), 1);

    if let Item::Fn(func) = &parse_res.ast.module.items[0] {
        assert!(func.sig.name.starts_with("identifier_"));
        assert!(func.sig.ret.is_some()); // Есть тип возврата
        assert_eq!(func.sig.ret.as_ref().unwrap().repr, "int"); // Теперь корректно реконструирует тип из токенов

        // На уровне парсера это должно проходить без ошибок
        // Семантическая ошибка будет на следующих этапах
        assert!(parse_res.diags.is_empty());
    } else {
        panic!("Expected function item");
    }
}
