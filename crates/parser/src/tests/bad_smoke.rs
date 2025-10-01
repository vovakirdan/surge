use super::*;
use crate::ParseCode;

#[test]
fn detects_unclosed_parenthesis() {
    let src = r#"
fn main() -> int {
    if (true {
        return 1;
    } else {
        return 0;
    }
}
"#;
    let res = parse(src);
    assert!(!res.diags.is_empty(), "expected diagnostics");
    assert_eq!(res.diags[0].code, ParseCode::UnclosedParen);
    assert_eq!(res.diags[0].message, "Expected ')' to close expression");
}

#[test]
fn reports_missing_semicolon() {
    let src = r#"
fn test() -> int {
    let x:int = 42
    let y:int = 10;
    return x + y;
}
"#;
    let res = parse(src);
    assert_eq!(res.diags[0].code, ParseCode::MissingSemicolon);
    assert_eq!(res.diags[0].message, "Expected ';' after statement");
}

#[test]
fn reports_missing_colon_in_type() {
    let src = r#"
fn test() -> int {
    let value int = 42;
    return value;
}
"#;
    let res = parse(src);
    assert_eq!(res.diags[0].code, ParseCode::MissingColonInType);
    assert_eq!(res.diags[0].message, "Expected ':' before type annotation");
}

#[test]
fn accepts_function_without_return_type() {
    let src = r#"
fn test() {
    return 0;
}
"#;
    let res = parse(src);
    // Return types are now optional, so this should parse without error
    assert!(
        res.diags.is_empty(),
        "Expected no diagnostics, but got: {:?}",
        res.diags
    );

    // Verify the function parsed correctly
    assert_eq!(res.ast.module.items.len(), 1);
    if let crate::Item::Fn(func) = &res.ast.module.items[0] {
        assert_eq!(func.sig.name, "test");
        assert!(func.sig.ret.is_none()); // No return type
        assert!(func.sig.params.is_empty());
    } else {
        panic!("Expected function item");
    }
}

#[test]
fn reports_invalid_operator() {
    let src = r#"
fn test_func() -> bool {
    let a:int = 1;
    let b:int = 2;
    return a <> b;
}
"#;
    let res = parse(src);
    assert_eq!(res.diags[0].code, ParseCode::UnexpectedToken);
    assert_eq!(
        res.diags[0].message,
        "Unexpected token '>' after '<' — operator '<>' is not valid"
    );
}

#[test]
fn reports_missing_assignment_equals() {
    let src = r#"
fn assign() -> int {
    let x:int = 1;
    let y:int;
    y 10;
    return x + y;
}
"#;
    let res = parse(src);
    assert_eq!(res.diags[0].code, ParseCode::UnexpectedToken);
    assert_eq!(res.diags[0].message, "Expected '=' in assignment");
}

#[test]
fn reports_let_missing_type_and_initializer() {
    let src = r#"
fn missing() -> int {
    let value;
    return 0;
}
"#;
    let res = parse(src);
    assert_eq!(res.diags[0].code, ParseCode::LetMissingEquals);
    assert_eq!(
        res.diags[0].message,
        "Expected type annotation or initializer in let declaration"
    );
}

#[test]
fn reports_unclosed_block() {
    let src = r#"
fn test_incomplete() -> int {
    let x:int = 42;
"#;
    let res = parse(src);
    assert_eq!(res.diags[0].code, ParseCode::UnclosedBrace);
    assert_eq!(res.diags[0].message, "Expected '}' to close block");
}

#[test]
fn reports_unclosed_array_literal() {
    let src = r#"
fn test_array() -> int {
    let values:int[] = [10, 20, 30;
    return values[0];
}
"#;
    let res = parse(src);
    assert_eq!(res.diags[0].code, ParseCode::UnclosedBracket);
    assert_eq!(res.diags[0].message, "Expected ']' to close array literal");
}

#[test]
fn reports_fat_arrow_outside_parallel() {
    let src = r#"
fn invalid_arrow() -> int {
    let value:int = compute => 42;
    return value;
}
"#;
    let res = parse(src);
    assert_eq!(res.diags[0].code, ParseCode::FatArrowOutsideParallel);
    assert_eq!(
        res.diags[0].message,
        "'=>` is only allowed in compare arms and parallel map/reduce expressions"
    );
}

#[test]
fn reports_parallel_map_missing_with() {
    let src = r#"
fn demo(xs: int[]) -> int[] {
    return parallel map xs => process_item;
}
"#;
    let res = parse(src);
    assert_eq!(res.diags[0].code, ParseCode::ParallelMissingWith);
}

#[test]
fn reports_parallel_map_missing_arrow() {
    let src = r#"
fn demo(xs: int[]) -> int[] {
    return parallel map xs with () process_item;
}
"#;
    let res = parse(src);
    assert_eq!(res.diags[0].code, ParseCode::ParallelMissingFatArrow);
}

#[test]
fn reports_parallel_map_missing_parens() {
    let src = r#"
fn demo(xs: int[]) -> int[] {
    return parallel map xs with item => process_item;
}
"#;
    let res = parse(src);
    assert_eq!(res.diags[0].code, ParseCode::ParallelBadHeader);
}

#[test]
fn reports_parallel_reduce_missing_comma() {
    let src = r#"
fn demo(xs: int[]) -> int {
    return parallel reduce xs with 0 () => combine;
}
"#;
    let res = parse(src);
    assert_eq!(res.diags[0].code, ParseCode::ParallelBadHeader);
}
