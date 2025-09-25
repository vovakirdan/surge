use surge_lexer::{lex, LexOptions};
use surge_token::SourceId;
use crate::{parse_tokens, Item};

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
        println!("Unknown attr diagnostic: {:?} - {}", diag.code, diag.message);
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
            panic!("Expected Overload attribute due to ambiguity, got: {:?}", func.sig.attrs[0]);
        }
    }
}