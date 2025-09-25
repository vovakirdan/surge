use super::*;
use crate::{BinaryOp, Expr, Item, Stmt};

#[test]
fn parses_function_with_return_and_body() {
    let src = r#"
fn add(a:int, b:int) -> int {
    let result:int = a + b * 3;
    return result;
}
"#;
    let res = parse(src);
    assert_no_parse_errors(&res);

    assert_eq!(res.ast.module.items.len(), 1);
    let item = &res.ast.module.items[0];
    match item {
        Item::Fn(func) => {
            assert_eq!(func.sig.name, "add");
            assert_eq!(func.sig.params.len(), 2);
            assert!(func.sig.ret.is_some());
            let body = func.body.as_ref().expect("function body");
            assert_eq!(body.stmts.len(), 2);
            match &body.stmts[0] {
                Stmt::Let { name, init, .. } => {
                    assert_eq!(name, "result");
                    let expr = init.as_ref().expect("init expression");
                    match expr {
                        Expr::Binary { op, rhs, .. } => {
                            assert_eq!(*op, BinaryOp::Add);
                            match rhs.as_ref() {
                                Expr::Binary { op: mul_op, .. } => {
                                    assert_eq!(*mul_op, BinaryOp::Mul);
                                }
                                other => panic!("expected multiplication in rhs, got {other:?}"),
                            }
                        }
                        other => panic!("expected binary expression, got {other:?}"),
                    }
                }
                other => panic!("unexpected first statement: {other:?}"),
            }
        }
        other => panic!("expected function item, got {other:?}"),
    }
}

#[test]
fn expression_precedence_respected() {
    let src = r#"
fn calc() -> int {
    return 1 + 2 * 3;
}
"#;
    let res = parse(src);
    assert_no_parse_errors(&res);
    let func = match &res.ast.module.items[0] {
        Item::Fn(func) => func,
        other => panic!("expected fn, got {other:?}"),
    };
    let body = func.body.as_ref().unwrap();
    let ret = match &body.stmts[0] {
        Stmt::Return {
            expr: Some(expr), ..
        } => expr,
        other => panic!("expected return stmt, got {other:?}"),
    };
    match ret {
        Expr::Binary { op, lhs, rhs, .. } => {
            assert_eq!(*op, BinaryOp::Add);
            assert!(matches!(lhs.as_ref(), Expr::LitInt(value, _) if value == "1"));
            match rhs.as_ref() {
                Expr::Binary {
                    op: mul_op,
                    lhs: mul_lhs,
                    rhs: mul_rhs,
                    ..
                } => {
                    assert_eq!(*mul_op, BinaryOp::Mul);
                    assert!(matches!(mul_lhs.as_ref(), Expr::LitInt(value, _) if value == "2"));
                    assert!(matches!(mul_rhs.as_ref(), Expr::LitInt(value, _) if value == "3"));
                }
                other => panic!("expected multiplication in rhs, got {other:?}"),
            }
        }
        other => panic!("expected binary expression, got {other:?}"),
    }
}

#[test]
fn arrays_and_indexing_parse() {
    let src = r#"
fn arrays() -> int {
    let values:int[] = [1, 2, 3];
    let idx:int = values[1];
    return values[2];
}
"#;
    let res = parse(src);
    assert_no_parse_errors(&res);
    let func = match &res.ast.module.items[0] {
        Item::Fn(func) => func,
        _ => panic!("expected fn"),
    };
    let body = func.body.as_ref().unwrap();
    assert_eq!(body.stmts.len(), 3);
    match &body.stmts[0] {
        Stmt::Let {
            init: Some(expr), ..
        } => match expr {
            Expr::Array { elems, .. } => {
                assert_eq!(elems.len(), 3);
            }
            other => panic!("expected array literal, got {other:?}"),
        },
        other => panic!("unexpected stmt: {other:?}"),
    }
    match &body.stmts[1] {
        Stmt::Let {
            init: Some(expr), ..
        } => match expr {
            Expr::Index { .. } => {}
            other => panic!("expected index expression, got {other:?}"),
        },
        other => panic!("unexpected stmt: {other:?}"),
    }
}

#[test]
fn control_flow_constructs() {
    let src = r#"
fn control() -> int {
    let mut acc:int = 0;
    if (true) {
        acc = acc + 1;
    } else {
        acc = acc - 1;
    }
    while (acc < 10) {
        acc = acc + 1;
    }
    for (let i:int = 0; i < 3; i = i + 1) {
        acc = acc + i;
    }
    return acc;
}
"#;
    let res = parse(src);
    assert_no_parse_errors(&res);
    let func = match &res.ast.module.items[0] {
        Item::Fn(func) => func,
        _ => panic!("expected fn"),
    };
    let body = func.body.as_ref().unwrap();
    assert!(
        body.stmts
            .iter()
            .any(|stmt| matches!(stmt, Stmt::If { .. }))
    );
    assert!(
        body.stmts
            .iter()
            .any(|stmt| matches!(stmt, Stmt::While { .. }))
    );
    assert!(
        body.stmts
            .iter()
            .any(|stmt| matches!(stmt, Stmt::ForC { .. }))
    );
}
