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
fn reports_missing_return_type() {
    let src = r#"
fn test() {
    return 0;
}
"#;
    let res = parse(src);
    assert_eq!(res.diags[0].code, ParseCode::MissingReturnType);
    assert_eq!(
        res.diags[0].message,
        "Expected '-> Type' in function signature"
    );
}

#[test]
fn reports_invalid_operator() {
    let src = r#"
fn compare() -> bool {
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
