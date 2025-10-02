use super::*;
use crate::{AliasVariant, Attr, BinaryOp, Expr, Item, PatternKind, Stmt};

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

#[test]
fn let_without_initializer_is_allowed() {
    let src = r#"
fn defaults() -> int {
    let count:int;
    let mut total:int;
    count = 1;
    total = count + 2;
    return total;
}

"#;
    let res = parse(src);
    assert_no_parse_errors(&res);

    if let Item::Fn(func) = &res.ast.module.items[0] {
        let body = func.body.as_ref().expect("function body");
        // Ensure we recorded let statements without initializers
        assert!(matches!(body.stmts[0], Stmt::Let { ref init, .. } if init.is_none()));
        assert!(matches!(
            body.stmts[1],
            Stmt::Let {
                ref init,
                mutable: true,
                ..
            } if init.is_none()
        ));
    } else {
        panic!("expected function item");
    }
}

#[test]
fn parses_struct_types_with_inheritance_and_defaults() {
    let src = r#"
@sealed
type Person<T> = {
    name: string,
    @readonly age: int = 30,
};

type Employee = Person : {
    @hidden id: uint64,
};
"#;
    let res = parse(src);
    assert_no_parse_errors(&res);

    assert_eq!(res.ast.module.items.len(), 2);

    let person = match &res.ast.module.items[0] {
        Item::Type(def) => def,
        other => panic!("expected first item to be type, got {other:?}"),
    };
    assert_eq!(person.name, "Person");
    assert_eq!(person.generics.len(), 1);
    assert_eq!(person.attrs.len(), 1);
    assert!(matches!(person.attrs[0], Attr::Sealed { .. }));
    assert!(person.base.is_none());
    assert_eq!(person.fields.len(), 2);
    assert!(person.fields[0].attrs.is_empty());
    assert_eq!(person.fields[0].name, "name");
    assert!(person.fields[0].default.is_none());
    assert_eq!(person.fields[1].name, "age");
    assert!(matches!(
        person.fields[1].attrs.get(0),
        Some(Attr::Readonly { .. })
    ));
    assert!(person.fields[1].default.is_some());

    let employee = match &res.ast.module.items[1] {
        Item::Type(def) => def,
        other => panic!("expected second item to be type, got {other:?}"),
    };
    assert!(employee.base.is_some());
    assert_eq!(employee.fields.len(), 1);
    assert_eq!(employee.fields[0].name, "id");
    assert!(matches!(
        employee.fields[0].attrs.get(0),
        Some(Attr::Hidden { .. })
    ));
}

#[test]
fn parses_newtypes_and_aliases_with_generics() {
    let src = r#"
newtype UserId<T> = uint64;
alias Maybe<T> = T | nothing;
alias Option<T> = Some(T) | nothing;
"#;

    let res = parse(src);
    assert_no_parse_errors(&res);
    assert_eq!(res.ast.module.items.len(), 3);

    let newtype = match &res.ast.module.items[0] {
        Item::Newtype(def) => def,
        other => panic!("expected newtype, got {other:?}"),
    };
    assert_eq!(newtype.name, "UserId");
    assert_eq!(newtype.generics.len(), 1);

    let alias_maybe = match &res.ast.module.items[1] {
        Item::Alias(def) => def,
        other => panic!("expected alias, got {other:?}"),
    };
    assert_eq!(alias_maybe.name, "Maybe");
    assert_eq!(alias_maybe.generics.len(), 1);
    assert_eq!(alias_maybe.variants.len(), 2);

    let alias_option = match &res.ast.module.items[2] {
        Item::Alias(def) => def,
        other => panic!("expected alias, got {other:?}"),
    };
    assert_eq!(alias_option.name, "Option");
    assert_eq!(alias_option.generics.len(), 1);
    assert_eq!(alias_option.variants.len(), 2);
    assert!(matches!(alias_option.variants[0], AliasVariant::Tag { .. }));
    assert!(matches!(
        alias_option.variants[1],
        AliasVariant::Nothing { .. }
    ));
}

#[test]
fn parses_literal_and_tag_definitions() {
    let src = r#"
literal Color = "red" | "green";
tag Some<T>(T);
tag None();
"#;

    let res = parse(src);
    assert_no_parse_errors(&res);
    assert_eq!(res.ast.module.items.len(), 3);

    let literal = match &res.ast.module.items[0] {
        Item::Literal(def) => def,
        other => panic!("expected literal def, got {other:?}"),
    };
    assert_eq!(literal.name, "Color");
    assert_eq!(literal.values.len(), 2);
    assert_eq!(literal.values[0].value, "red");
    assert_eq!(literal.values[1].value, "green");

    let tag_some = match &res.ast.module.items[1] {
        Item::Tag(def) => def,
        other => panic!("expected tag def, got {other:?}"),
    };
    assert_eq!(tag_some.name, "Some");
    assert_eq!(tag_some.generics.len(), 1);
    assert_eq!(tag_some.params.len(), 1);

    let tag_none = match &res.ast.module.items[2] {
        Item::Tag(def) => def,
        other => panic!("expected tag def, got {other:?}"),
    };
    assert_eq!(tag_none.name, "None");
    assert!(tag_none.params.is_empty());
}

#[test]
fn for_in_parses_pattern_and_optional_type() {
    let src = r#"
fn walk(seq: Option<int>) {
    for value in seq {
        continue;
    }

    for Some(inner): Option<int> in seq {
        let current:int = inner;
    }
}
"#;

    let res = parse(src);
    assert_no_parse_errors(&res);

    let func = match &res.ast.module.items[0] {
        Item::Fn(func) => func,
        other => panic!("expected function item, got {other:?}"),
    };
    let body = func.body.as_ref().expect("function body");
    assert_eq!(body.stmts.len(), 2);

    match &body.stmts[0] {
        Stmt::ForIn {
            pattern,
            ty,
            body: loop_body,
            ..
        } => {
            assert!(matches!(pattern.kind, PatternKind::Binding(ref name) if name == "value"));
            assert!(ty.is_none());
            assert!(matches!(
                loop_body.stmts.as_slice(),
                [Stmt::Continue { .. }]
            ));
        }
        other => panic!("expected for-in binding without type, got {other:?}"),
    }

    match &body.stmts[1] {
        Stmt::ForIn {
            pattern,
            ty,
            body: loop_body,
            ..
        } => {
            match &pattern.kind {
                PatternKind::Tag { name, args } => {
                    assert_eq!(name, "Some");
                    assert_eq!(args.len(), 1);
                    assert!(
                        matches!(args[0].kind, PatternKind::Binding(ref inner) if inner == "inner")
                    );
                }
                other => panic!("expected tag pattern in second loop, got {other:?}"),
            }
            assert!(ty.is_some());
            assert!(matches!(loop_body.stmts.as_slice(), [Stmt::Let { .. }]));
        }
        other => panic!("expected for-in tag pattern, got {other:?}"),
    }
}

#[test]
fn extern_block_parses_methods() {
    let src = r#"
extern<Point> {
    @override
    fn __add(self: &Point, rhs: &Point) -> Point {
        return __add_impl(self, rhs);
    }
}
"#;

    let res = parse(src);
    assert_no_parse_errors(&res);

    assert_eq!(res.ast.module.items.len(), 1);
    let extern_block = match &res.ast.module.items[0] {
        Item::Extern(block) => block,
        other => panic!("expected extern block, got {other:?}"),
    };

    assert!(extern_block.attrs.is_empty());
    assert_eq!(extern_block.target.repr.trim(), "Point");
    assert_eq!(extern_block.methods.len(), 1);

    let method = &extern_block.methods[0];
    assert!(matches!(
        method.sig.attrs.first(),
        Some(Attr::Override { .. })
    ));
    assert_eq!(method.sig.name, "__add");
    assert!(method.body.is_some());
}
