package token

type Kind uint8

const (
	// специальные
	Invalid Kind = iota
	EOF

	// идентификаторы
	Ident
	KwFn       // fn
	KwLet      // let
	KwConst    // const
	KwMut      // mut
	KwOwn      // own
	KwIf       // if
	KwElse     // else
	KwWhile    // while
	KwFor      // for
	KwIn       // in
	KwBreak    // break
	KwContinue // continue
	KwReturn   // return
	KwImport   // import
	KwAs       // as
	KwType     // type
	KwContract // contract
	KwTag      // tag
	KwExtern   // extern
	KwPub      // pub
	KwAsync    // async
	KwAwait    // await
	KwCompare  // compare
	KwFinally  // finally
	KwChannel  // channel
	KwSpawn    // spawn
	KwTrue     // true
	KwFalse    // false
	KwSignal   // signal
	KwParallel // parallel
	KwMap      // map
	KwReduce   // reduce
	KwWith     // with
	KwMacro    // macro
	KwPragma   // pragma
	KwTo       // to
	KwHeir     // heir
	KwIs       // is
	KwField    // field

	// литералы
	NothingLit
	IntLit
	UintLit
	FloatLit
	BoolLit
	StringLit

	// операторы и пунктуация
	Plus             // +
	Minus            // -
	Star             // *
	Slash            // /
	Percent          // %
	Assign           // =
	PlusAssign       // +=
	MinusAssign      // -=
	StarAssign       // *=
	SlashAssign      // /=
	PercentAssign    // %=
	AmpAssign        // &=
	PipeAssign       // |=
	CaretAssign      // ^=
	ShlAssign        // <<=
	ShrAssign        // >>=
	EqEq             // ==
	Bang             // !
	BangEq           // !=
	Lt               // <
	LtEq             // <=
	Gt               // >
	GtEq             // >=
	Shl              // <<
	Shr              // >>
	Amp              // &
	Pipe             // |
	Caret            // ^
	AndAnd           // &&
	OrOr             // ||
	Question         // ?
	QuestionQuestion // ??
	Colon            // :
	ColonColon       // ::
	ColonAssign      // :=
	Semicolon        // ;
	Comma            // ,
	Dot              // .
	DotDot           // ..
	Arrow            // ->
	FatArrow         // =>
	LParen           // (
	RParen           // )
	LBrace           // {
	RBrace           // }
	LBracket         // [
	RBracket         // ]
	At               // @
	Underscore       // _
	DotDotEq         // ..=
	DotDotDot        // ... (vararg)
)
