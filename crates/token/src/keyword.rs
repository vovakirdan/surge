#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Keyword {
    Fn, Let, Mut, If, Else, While, For, In, Break, Continue,
    Import, Using, Type, Literal, Alias, Extern, Return, Signal,
    // attributes
    AtPure, AtOverload, AtOverride, AtBackend,
    True, False,
}

pub fn lookup_keyword(ident: &str) -> Option<Keyword> {
    use Keyword::*;
    Some(match ident {
        "fn" => Fn, "let" => Let, "mut" => Mut,
        "if" => If, "else" => Else, "while" => While, "for" => For, "in" => In,
        "break" => Break, "continue" => Continue,
        "import" => Import, "using" => Using,
        "type" => Type, "literal" => Literal, "alias" => Alias, "extern" => Extern,
        "return" => Return, "signal" => Signal,
        "@pure" => AtPure, "@overload" => AtOverload, "@override" => AtOverride, "@backend" => AtBackend,
        "true" => True, "false" => False,
        _ => return None,
    })
}